package matching

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

// Score multiplier and epoch offset for composite score computation.
// Composite score encodes price-time priority into a single float64 for Redis sorted sets.
//   Back side:  score = price * scoreMultiplier + (epochOffset - timestamp)  -> highest price, earliest time first
//   Lay side:   score = price * scoreMultiplier + timestamp                  -> lowest price, earliest time first
// epochOffset must exceed any plausible Unix timestamp to keep scores positive.
const (
	scoreMultiplier int64 = 10000000000 // 1e10 — enough room for timestamps up to ~9.9 billion
	epochOffset     int64 = 9999999999  // ~year 2286, safely above current Unix timestamps (~1.7e9)
)

// Redis Lua script for atomic order matching with integer arithmetic.
// Amounts are in paise/cents (multiplied by 100 at the Go level).
// The suspension check is performed atomically inside the script so that no
// race exists between the Go-level check and the actual matching.
const luaMatchScript = `
local backKey      = KEYS[1]
local layKey       = KEYS[2]
local suspendedKey = KEYS[3]
local orderJSON    = ARGV[1]
local side         = ARGV[2]

-- Atomic suspension check: reject if market is suspended
if redis.call('EXISTS', suspendedKey) == 1 then
    return redis.error_reply("ERR market is suspended")
end

local ok, order = pcall(cjson.decode, orderJSON)
if not ok then
    return redis.error_reply("ERR invalid order JSON: " .. tostring(order))
end

local SCORE_MULT  = 10000000000
local EPOCH_OFF   = 9999999999

local fills = {}
local remainingStake = tonumber(order.stake)
local orderUserID = tostring(order.user_id)

if side == "back" then
    -- Back order matches against lay orders at same or lower price (lowest first)
    local maxLayScore = tostring(order.price * SCORE_MULT + EPOCH_OFF)
    local layOrders = redis.call('ZRANGEBYSCORE', layKey, '-inf', maxLayScore)
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
        -- Self-trade prevention: skip orders from the same user
        if tostring(lay.user_id) ~= orderUserID then
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
                local newScore = tonumber(lay.price) * SCORE_MULT + tonumber(lay.timestamp)
                redis.call('ZADD', layKey, newScore, cjson.encode(lay))
            end
        end
    end
else
    -- Lay order matches against back orders at same or higher price (highest first)
    local minBackScore = tostring(order.price * SCORE_MULT)
    local backOrders = redis.call('ZREVRANGEBYSCORE', backKey, '+inf', minBackScore)
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
        -- Self-trade prevention: skip orders from the same user
        if tostring(back.user_id) ~= orderUserID then
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
                local newScore = tonumber(back.price) * SCORE_MULT + (EPOCH_OFF - tonumber(back.timestamp))
                redis.call('ZADD', backKey, newScore, cjson.encode(back))
            end
        end
    end
end

-- Place remaining as resting order
if remainingStake > 0 then
    order.remaining = remainingStake
    if side == "back" then
        local score = tonumber(order.price) * SCORE_MULT + (EPOCH_OFF - tonumber(order.timestamp))
        redis.call('ZADD', backKey, score, cjson.encode(order))
    else
        local score = tonumber(order.price) * SCORE_MULT + tonumber(order.timestamp)
        redis.call('ZADD', layKey, score, cjson.encode(order))
    end
end

return cjson.encode({
    matched = tonumber(order.stake) - remainingStake,
    unmatched = remainingStake,
    fills = fills
})
`

// Lua script for atomic cancel: scans the sorted set for the order by betID,
// verifies ownership, removes it, and returns the order JSON. This replaces the
// non-atomic Go-level ZSCAN+ZREM pattern.
const luaCancelScript = `
local key    = KEYS[1]
local betID  = ARGV[1]
local userID = ARGV[2]

local members = redis.call('ZRANGE', key, 0, -1)
for _, memberJSON in ipairs(members) do
    local ok, order = pcall(cjson.decode, memberJSON)
    if ok and order.id == betID then
        if tostring(order.user_id) ~= userID then
            return redis.error_reply("ERR order belongs to another user")
        end
        redis.call('ZREM', key, memberJSON)
        return memberJSON
    end
end
return redis.error_reply("ERR order not found: " .. betID)
`

type Engine struct {
	redis        *redis.Client
	logger       *slog.Logger
	orderPool    sync.Pool
	matchScript  *redis.Script
	cancelScript *redis.Script
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
		matchScript:  redis.NewScript(luaMatchScript),
		cancelScript: redis.NewScript(luaCancelScript),
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
// The suspension check is performed atomically inside the Lua script to prevent
// race conditions between checking and matching.
func (e *Engine) PlaceAndMatch(ctx context.Context, req *models.PlaceBetRequest, userID int64) (*models.MatchResult, error) {
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
	suspendedKey := fmt.Sprintf("market:suspended:%s", req.MarketID)

	result, err := e.matchScript.Run(ctx, e.redis,
		[]string{backKey, layKey, suspendedKey},
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

// CancelledOrder holds the details of a cancelled order for downstream processing.
type CancelledOrder struct {
	UserID    int64
	Price     float64
	Remaining float64
	Side      models.BetSide
}

// CancelOrder atomically finds and removes an order from the Redis order book
// using a Lua script that verifies ownership. Returns the cancelled order details.
func (e *Engine) CancelOrder(ctx context.Context, marketID, betID string, side models.BetSide, userID int64) (*CancelledOrder, error) {
	key := fmt.Sprintf("orderbook:%s:%s", marketID, side)

	result, err := e.cancelScript.Run(ctx, e.redis,
		[]string{key},
		betID, fmt.Sprintf("%d", userID),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected cancel result type: %T", result)
	}

	var order redisOrder
	if err := json.Unmarshal([]byte(resultStr), &order); err != nil {
		return nil, fmt.Errorf("parse cancelled order: %w", err)
	}

	e.logger.InfoContext(ctx, "order cancelled", "bet_id", betID, "market", marketID)
	return &CancelledOrder{
		UserID:    order.UserID,
		Price:     toFloatPrice(order.Price),
		Remaining: toFloatAmount(order.Remaining),
		Side:      side,
	}, nil
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

// PersistOrder writes the order and its match result to the PostgreSQL bets table.
//
// Hot-path optimizations:
//   - No-fill fast path: if the order did not match anything, we skip the
//     transaction machinery and perform a single plain INSERT.
//   - Batched fill inserts: all fills are written in one multi-row INSERT
//     instead of N round-trips.
//   - Batched counter-bet update: all counter-party bets are updated in a
//     single UPDATE ... FROM (VALUES ...) statement instead of N round-trips.
func (e *Engine) PersistOrder(ctx context.Context, db *sql.DB, req *models.PlaceBetRequest, userID int64, matchResult *models.MatchResult) error {
	now := time.Now()

	const insertBetSQL = `INSERT INTO bets (id, market_id, selection_id, user_id, side, price, stake,
		                    matched_stake, unmatched_stake, status, client_ref, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		 ON CONFLICT (id) DO UPDATE SET
		     matched_stake = EXCLUDED.matched_stake,
		     unmatched_stake = EXCLUDED.unmatched_stake,
		     status = EXCLUDED.status`

	// No-fill fast path: skip the transaction entirely.
	if len(matchResult.Fills) == 0 {
		_, err := db.ExecContext(ctx, insertBetSQL,
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
			return fmt.Errorf("persist order (no-fill fast path): %w", err)
		}
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin persist tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback is a no-op after commit

	_, err = tx.ExecContext(ctx, insertBetSQL,
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

	// --- Batched multi-row INSERT for fills ---
	{
		var sb strings.Builder
		sb.WriteString("INSERT INTO bet_fills (bet_id, counter_bet_id, price, size, created_at) VALUES ")
		args := make([]interface{}, 0, len(matchResult.Fills)*5)
		argIdx := 1
		for i, f := range matchResult.Fills {
			if i > 0 {
				sb.WriteString(",")
			}
			fmt.Fprintf(&sb, "($%d,$%d,$%d,$%d,$%d)", argIdx, argIdx+1, argIdx+2, argIdx+3, argIdx+4)
			args = append(args, matchResult.BetID, f.CounterBetID, f.Price, f.Size, f.Timestamp)
			argIdx += 5
		}
		sb.WriteString(" ON CONFLICT DO NOTHING")

		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("persist fills batch: %w", err)
		}
	}

	// --- Batched counter-bet UPDATE using UPDATE ... FROM (VALUES ...) ---
	{
		var sb strings.Builder
		sb.WriteString("UPDATE bets SET ")
		sb.WriteString("matched_stake = bets.matched_stake + v.fill_size, ")
		sb.WriteString("unmatched_stake = GREATEST(bets.unmatched_stake - v.fill_size, 0), ")
		sb.WriteString("status = CASE WHEN bets.unmatched_stake - v.fill_size <= 0 THEN 'matched' ELSE 'partial' END ")
		sb.WriteString("FROM (VALUES ")
		args := make([]interface{}, 0, len(matchResult.Fills)*2)
		argIdx := 1
		for i, f := range matchResult.Fills {
			if i > 0 {
				sb.WriteString(",")
			}
			// Cast fill_size to numeric so Postgres infers the types correctly.
			fmt.Fprintf(&sb, "($%d,$%d::numeric)", argIdx, argIdx+1)
			args = append(args, f.CounterBetID, f.Size)
			argIdx += 2
		}
		sb.WriteString(") AS v(counter_bet_id, fill_size) WHERE bets.id = v.counter_bet_id")

		if _, err := tx.ExecContext(ctx, sb.String(), args...); err != nil {
			return fmt.Errorf("update counter bets batch: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit persist tx: %w", err)
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
			score = float64(order.Price)*float64(scoreMultiplier) + float64(epochOffset-order.Timestamp)
		} else {
			score = float64(order.Price)*float64(scoreMultiplier) + float64(order.Timestamp)
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
