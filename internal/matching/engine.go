package matching

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

// Redis Lua script for atomic order matching with integer arithmetic.
// Amounts are in paise/cents (multiplied by 100 at the Go level).
// Composite score enforces price-time priority (FIFO at same price level):
//   Back side:  score = price * 1000000 + (999999999 - timestamp_seconds)  -> highest price, earliest time first
//   Lay side:   score = price * 1000000 + timestamp_seconds                -> lowest price, earliest time first
const luaMatchScript = `
local backKey = KEYS[1]
local layKey  = KEYS[2]
local orderJSON = ARGV[1]
local side      = ARGV[2]

local ok, order = pcall(cjson.decode, orderJSON)
if not ok then
    return redis.error_reply("ERR invalid order JSON: " .. tostring(order))
end

local fills = {}
local remainingStake = tonumber(order.stake)

if side == "back" then
    -- Back order matches against lay orders at same or lower price (lowest first)
    local layOrders = redis.call('ZRANGEBYSCORE', layKey, '-inf', tostring(order.price * 1000000 + 999999999))
    for _, layJSON in ipairs(layOrders) do
        if remainingStake <= 0 then break end
        local lok, lay = pcall(cjson.decode, layJSON)
        if not lok then
            return redis.error_reply("ERR corrupt lay order JSON")
        end
        -- Check price is actually matchable (lay price <= back price)
        if tonumber(lay.price) > tonumber(order.price) then
            break
        end
        local layRemaining = tonumber(lay.remaining)
        local fillSize = remainingStake
        if layRemaining < fillSize then
            fillSize = layRemaining
        end
        remainingStake = remainingStake - fillSize
        lay.remaining = layRemaining - fillSize

        fills[#fills + 1] = cjson.encode({
            counter_bet_id = lay.id,
            price = lay.price,
            size = fillSize
        })

        redis.call('ZREM', layKey, layJSON)
        if lay.remaining > 0 then
            local newScore = tonumber(lay.price) * 1000000 + tonumber(lay.timestamp)
            redis.call('ZADD', layKey, newScore, cjson.encode(lay))
        end
    end
else
    -- Lay order matches against back orders at same or higher price (highest first)
    local backOrders = redis.call('ZREVRANGEBYSCORE', backKey, '+inf', tostring(order.price * 1000000))
    for _, backJSON in ipairs(backOrders) do
        if remainingStake <= 0 then break end
        local bok, back = pcall(cjson.decode, backJSON)
        if not bok then
            return redis.error_reply("ERR corrupt back order JSON")
        end
        -- Check price is actually matchable (back price >= lay price)
        if tonumber(back.price) < tonumber(order.price) then
            break
        end
        local backRemaining = tonumber(back.remaining)
        local fillSize = remainingStake
        if backRemaining < fillSize then
            fillSize = backRemaining
        end
        remainingStake = remainingStake - fillSize
        back.remaining = backRemaining - fillSize

        fills[#fills + 1] = cjson.encode({
            counter_bet_id = back.id,
            price = back.price,
            size = fillSize
        })

        redis.call('ZREM', backKey, backJSON)
        if back.remaining > 0 then
            local newScore = tonumber(back.price) * 1000000 + (999999999 - tonumber(back.timestamp))
            redis.call('ZADD', backKey, newScore, cjson.encode(back))
        end
    end
end

-- Place remaining as resting order
if remainingStake > 0 then
    order.remaining = remainingStake
    if side == "back" then
        local score = tonumber(order.price) * 1000000 + (999999999 - tonumber(order.timestamp))
        redis.call('ZADD', backKey, score, cjson.encode(order))
    else
        local score = tonumber(order.price) * 1000000 + tonumber(order.timestamp)
        redis.call('ZADD', layKey, score, cjson.encode(order))
    end
end

return cjson.encode({
    matched = tonumber(order.stake) - remainingStake,
    unmatched = remainingStake,
    fills = fills
})
`

type Engine struct {
	redis       *redis.Client
	logger      *slog.Logger
	orderPool   sync.Pool
	matchScript *redis.Script
}

// redisOrder uses integer amounts (paise/cents) internally to avoid float precision issues.
type redisOrder struct {
	ID        string `json:"id"`
	MarketID  string `json:"market_id"`
	UserID    int64  `json:"user_id"`
	Side      string `json:"side"`
	Price     int64  `json:"price"`     // price in hundredths (e.g. 1.50 -> 150)
	Stake     int64  `json:"stake"`     // stake in paise/cents
	Remaining int64  `json:"remaining"` // remaining in paise/cents
	Timestamp int64  `json:"timestamp"` // unix seconds
}

type luaResult struct {
	Matched   int64    `json:"matched"`
	Unmatched int64    `json:"unmatched"`
	Fills     []string `json:"fills"`
}

type luaFill struct {
	CounterBetID string `json:"counter_bet_id"`
	Price        int64  `json:"price"`
	Size         int64  `json:"size"`
}

func NewEngine(rdb *redis.Client, logger *slog.Logger) *Engine {
	return &Engine{
		redis:  rdb,
		logger: logger,
		orderPool: sync.Pool{
			New: func() interface{} { return &redisOrder{} },
		},
		matchScript: redis.NewScript(luaMatchScript),
	}
}

// toIntPrice converts a float64 price to integer hundredths (e.g. 1.50 -> 150).
func toIntPrice(price float64) int64 {
	return int64(math.Round(price * 100))
}

// toIntAmount converts a float64 amount to integer paise/cents.
func toIntAmount(amount float64) int64 {
	return int64(math.Round(amount * 100))
}

// toFloatPrice converts integer hundredths back to float64.
func toFloatPrice(price int64) float64 {
	return float64(price) / 100.0
}

// toFloatAmount converts integer paise/cents back to float64.
func toFloatAmount(amount int64) float64 {
	return float64(amount) / 100.0
}

// IsMarketSuspended checks whether a market is currently suspended via Redis flag.
func (e *Engine) IsMarketSuspended(ctx context.Context, marketID string) (bool, error) {
	key := fmt.Sprintf("market:suspended:%s", marketID)
	val, err := e.redis.Exists(ctx, key).Result()
	if err != nil {
		return false, fmt.Errorf("check market suspended: %w", err)
	}
	return val > 0, nil
}

// SuspendMarket sets the suspension flag for a market in Redis.
func (e *Engine) SuspendMarket(ctx context.Context, marketID string) error {
	key := fmt.Sprintf("market:suspended:%s", marketID)
	if err := e.redis.Set(ctx, key, "1", 0).Err(); err != nil {
		return fmt.Errorf("suspend market: %w", err)
	}
	e.logger.InfoContext(ctx, "market suspended", "market_id", marketID)
	return nil
}

// ResumeMarket removes the suspension flag for a market in Redis.
func (e *Engine) ResumeMarket(ctx context.Context, marketID string) error {
	key := fmt.Sprintf("market:suspended:%s", marketID)
	if err := e.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("resume market: %w", err)
	}
	e.logger.InfoContext(ctx, "market resumed", "market_id", marketID)
	return nil
}

// PlaceAndMatch places an order and atomically matches it against the opposing side.
// Returns an error if the market is suspended.
func (e *Engine) PlaceAndMatch(ctx context.Context, req *models.PlaceBetRequest, userID int64) (*models.MatchResult, error) {
	// Circuit breaker: check if market is suspended
	suspended, err := e.IsMarketSuspended(ctx, req.MarketID)
	if err != nil {
		return nil, fmt.Errorf("check suspension: %w", err)
	}
	if suspended {
		return nil, fmt.Errorf("market %s is suspended", req.MarketID)
	}

	betID := uuid.New().String()

	order := e.orderPool.Get().(*redisOrder)
	defer e.orderPool.Put(order)

	order.ID = betID
	order.MarketID = req.MarketID
	order.UserID = userID
	order.Side = string(req.Side)
	order.Price = toIntPrice(req.Price)
	order.Stake = toIntAmount(req.Stake)
	order.Remaining = toIntAmount(req.Stake)
	order.Timestamp = time.Now().Unix()

	orderJSON, err := json.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("marshal order: %w", err)
	}

	backKey := fmt.Sprintf("orderbook:%s:back", req.MarketID)
	layKey := fmt.Sprintf("orderbook:%s:lay", req.MarketID)

	result, err := e.matchScript.Run(ctx, e.redis,
		[]string{backKey, layKey},
		string(orderJSON), string(req.Side),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("execute match script: %w", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected match result type: %T", result)
	}

	// Parse into a flexible structure to handle cjson empty table as {} vs []
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(resultStr), &raw); err != nil {
		return nil, fmt.Errorf("parse match result: %w", err)
	}

	matchedRaw := toRawInt64(raw["matched"])
	unmatchedRaw := toRawInt64(raw["unmatched"])

	// Parse fills with proper error handling (no silent continues)
	var fills []models.Fill
	if fillsRaw, exists := raw["fills"]; exists {
		fillsArr, isArr := fillsRaw.([]interface{})
		if isArr {
			for i, f := range fillsArr {
				fStr, isStr := f.(string)
				if !isStr {
					return nil, fmt.Errorf("fill[%d] is not a string: %T", i, f)
				}
				var lf luaFill
				if err := json.Unmarshal([]byte(fStr), &lf); err != nil {
					return nil, fmt.Errorf("parse fill[%d]: %w", i, err)
				}
				fills = append(fills, models.Fill{
					CounterBetID: lf.CounterBetID,
					Price:        toFloatPrice(lf.Price),
					Size:         toFloatAmount(lf.Size),
					Timestamp:    time.Now(),
				})
			}
		}
		// else: empty object {} from Lua means no fills, which is fine
	}

	// Convert integer results back to float for the response
	matchedFloat := toFloatAmount(matchedRaw)
	unmatchedFloat := toFloatAmount(unmatchedRaw)

	status := models.BetStatusUnmatched
	if matchedRaw > 0 && unmatchedRaw == 0 {
		status = models.BetStatusMatched
	} else if matchedRaw > 0 {
		status = models.BetStatusPartial
	}

	matchResult := &models.MatchResult{
		BetID:          betID,
		MatchedStake:   matchedFloat,
		UnmatchedStake: unmatchedFloat,
		Status:         status,
		Fills:          fills,
	}

	e.logger.InfoContext(ctx, "order matched",
		"bet_id", betID, "matched", matchedFloat, "unmatched", unmatchedFloat,
		"fills", len(fills), "market", req.MarketID)

	return matchResult, nil
}

// CancelOrder removes an order from the Redis order book.
func (e *Engine) CancelOrder(ctx context.Context, marketID, betID string, side models.BetSide) error {
	key := fmt.Sprintf("orderbook:%s:%s", marketID, side)

	// Scan for the order and remove it
	var cursor uint64
	for {
		keys, nextCursor, err := e.redis.ZScan(ctx, key, cursor, fmt.Sprintf("*%s*", betID), 100).Result()
		if err != nil {
			return fmt.Errorf("scan orderbook: %w", err)
		}

		for i := 0; i < len(keys); i += 2 {
			member := keys[i]
			var order redisOrder
			if err := json.Unmarshal([]byte(member), &order); err != nil {
				return fmt.Errorf("parse order during cancel scan: %w", err)
			}
			if order.ID == betID {
				if err := e.redis.ZRem(ctx, key, member).Err(); err != nil {
					return fmt.Errorf("remove order: %w", err)
				}
				e.logger.InfoContext(ctx, "order cancelled", "bet_id", betID, "market", marketID)
				return nil
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return fmt.Errorf("order not found: %s", betID)
}

// GetOrderBook returns the best backs and lays aggregated by price level.
func (e *Engine) GetOrderBook(ctx context.Context, marketID string, depth int) (backs []models.PriceSize, lays []models.PriceSize, err error) {
	backKey := fmt.Sprintf("orderbook:%s:back", marketID)
	layKey := fmt.Sprintf("orderbook:%s:lay", marketID)

	// Best backs: highest composite score first (highest price, earliest time)
	backMembers, err := e.redis.ZRevRangeWithScores(ctx, backKey, 0, int64(depth-1)).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("get backs: %w", err)
	}

	// Best lays: lowest composite score first (lowest price, earliest time)
	layMembers, err := e.redis.ZRangeWithScores(ctx, layKey, 0, int64(depth-1)).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("get lays: %w", err)
	}

	// Aggregate by price level
	backAgg := make(map[float64]float64)
	for _, m := range backMembers {
		var order redisOrder
		if err := json.Unmarshal([]byte(m.Member.(string)), &order); err != nil {
			return nil, nil, fmt.Errorf("parse back order: %w", err)
		}
		backAgg[toFloatPrice(order.Price)] += toFloatAmount(order.Remaining)
	}

	layAgg := make(map[float64]float64)
	for _, m := range layMembers {
		var order redisOrder
		if err := json.Unmarshal([]byte(m.Member.(string)), &order); err != nil {
			return nil, nil, fmt.Errorf("parse lay order: %w", err)
		}
		layAgg[toFloatPrice(order.Price)] += toFloatAmount(order.Remaining)
	}

	for price, size := range backAgg {
		backs = append(backs, models.PriceSize{Price: price, Size: size})
	}
	for price, size := range layAgg {
		lays = append(lays, models.PriceSize{Price: price, Size: size})
	}

	return backs, lays, nil
}

// ExpireOrders removes all resting orders for a market that were placed before the given time.
func (e *Engine) ExpireOrders(ctx context.Context, marketID string, before time.Time) (int, error) {
	beforeSec := before.Unix()
	removed := 0

	for _, side := range []string{"back", "lay"} {
		key := fmt.Sprintf("orderbook:%s:%s", marketID, side)

		// Retrieve all members and check timestamps
		var cursor uint64
		for {
			members, nextCursor, err := e.redis.ZScan(ctx, key, cursor, "*", 200).Result()
			if err != nil {
				return removed, fmt.Errorf("scan for expiry: %w", err)
			}

			for i := 0; i < len(members); i += 2 {
				memberJSON := members[i]
				var order redisOrder
				if err := json.Unmarshal([]byte(memberJSON), &order); err != nil {
					return removed, fmt.Errorf("parse order for expiry: %w", err)
				}
				if order.Timestamp < beforeSec {
					if err := e.redis.ZRem(ctx, key, memberJSON).Err(); err != nil {
						return removed, fmt.Errorf("remove expired order: %w", err)
					}
					removed++
				}
			}

			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}

	if removed > 0 {
		e.logger.InfoContext(ctx, "expired orders removed",
			"market_id", marketID, "removed", removed, "before", before)
	}
	return removed, nil
}

// PersistOrder writes the order and its match result to the PostgreSQL bets table
// so that data survives a Redis crash.
func (e *Engine) PersistOrder(ctx context.Context, db *sql.DB, req *models.PlaceBetRequest, userID int64, matchResult *models.MatchResult) error {
	now := time.Now()

	_, err := db.ExecContext(ctx,
		`INSERT INTO bets (id, market_id, selection_id, user_id, side, price, stake,
		                    matched_stake, unmatched_stake, status, client_ref, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (id) DO UPDATE SET
		     matched_stake = EXCLUDED.matched_stake,
		     unmatched_stake = EXCLUDED.unmatched_stake,
		     status = EXCLUDED.status`,
		matchResult.BetID,
		req.MarketID,
		req.SelectionID,
		userID,
		string(req.Side),
		req.Price,
		req.Stake,
		matchResult.MatchedStake,
		matchResult.UnmatchedStake,
		string(matchResult.Status),
		req.ClientRef,
		now,
	)
	if err != nil {
		return fmt.Errorf("persist order: %w", err)
	}

	// Persist individual fills
	for _, fill := range matchResult.Fills {
		_, err := db.ExecContext(ctx,
			`INSERT INTO bet_fills (bet_id, counter_bet_id, price, size, created_at)
			 VALUES ($1, $2, $3, $4, $5)
			 ON CONFLICT DO NOTHING`,
			matchResult.BetID, fill.CounterBetID, fill.Price, fill.Size, fill.Timestamp,
		)
		if err != nil {
			return fmt.Errorf("persist fill: %w", err)
		}

		// Update the counter-order's matched/unmatched stakes in DB
		_, err = db.ExecContext(ctx,
			`UPDATE bets SET
			     matched_stake = matched_stake + $1,
			     unmatched_stake = GREATEST(unmatched_stake - $1, 0),
			     status = CASE
			         WHEN unmatched_stake - $1 <= 0 THEN 'matched'
			         ELSE 'partial'
			     END
			 WHERE id = $2`,
			fill.Size, fill.CounterBetID,
		)
		if err != nil {
			return fmt.Errorf("update counter bet: %w", err)
		}
	}

	return nil
}

// RecoverOrders loads all unmatched/partial orders from PostgreSQL back into the
// Redis order book. Call this on startup to recover from a Redis crash.
func (e *Engine) RecoverOrders(ctx context.Context, db *sql.DB) (int, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, market_id, user_id, side, price, unmatched_stake,
		        EXTRACT(EPOCH FROM created_at)::bigint AS ts
		 FROM bets
		 WHERE status IN ('unmatched', 'partial') AND unmatched_stake > 0
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return 0, fmt.Errorf("query unmatched orders: %w", err)
	}
	defer rows.Close()

	loaded := 0
	for rows.Next() {
		var (
			id       string
			marketID string
			userID   int64
			side     string
			price    float64
			stake    float64
			ts       int64
		)
		if err := rows.Scan(&id, &marketID, &userID, &side, &price, &stake, &ts); err != nil {
			return loaded, fmt.Errorf("scan order for recovery: %w", err)
		}

		order := redisOrder{
			ID:        id,
			MarketID:  marketID,
			UserID:    userID,
			Side:      side,
			Price:     toIntPrice(price),
			Stake:     toIntAmount(stake),
			Remaining: toIntAmount(stake),
			Timestamp: ts,
		}

		orderJSON, err := json.Marshal(order)
		if err != nil {
			return loaded, fmt.Errorf("marshal recovery order: %w", err)
		}

		key := fmt.Sprintf("orderbook:%s:%s", marketID, side)
		var score float64
		if side == "back" {
			score = float64(order.Price)*1000000 + float64(999999999-order.Timestamp)
		} else {
			score = float64(order.Price)*1000000 + float64(order.Timestamp)
		}

		if err := e.redis.ZAdd(ctx, key, redis.Z{
			Score:  score,
			Member: string(orderJSON),
		}).Err(); err != nil {
			return loaded, fmt.Errorf("recover order to redis: %w", err)
		}
		loaded++
	}
	if err := rows.Err(); err != nil {
		return loaded, fmt.Errorf("iterate recovery rows: %w", err)
	}

	e.logger.InfoContext(ctx, "orders recovered from database", "count", loaded)
	return loaded, nil
}

// toRawInt64 converts a JSON-decoded numeric value to int64.
func toRawInt64(v interface{}) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}
