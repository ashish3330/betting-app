package matching

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

// -----------------------------------------------------------------------------
// Redis key layout (updated from the original JSON-blob-in-ZSet design)
// -----------------------------------------------------------------------------
//
// Per market, per side, two keys:
//
//   ob:{marketID}:back:z    ZSet    member = betID, score = composite price/time
//   ob:{marketID}:back:h    Hash    field  = betID, value = pipe-separated order
//   ob:{marketID}:lay:z     ZSet    member = betID, score = composite price/time
//   ob:{marketID}:lay:h     Hash    field  = betID, value = pipe-separated order
//
//   market:suspended:{marketID}   string flag, presence = suspended
//
// The pipe-separated order value is:
//
//     "userID|price|stake|remaining|timestamp"
//
// where every field is an integer:
//
//   userID     - int64
//   price      - int64 hundredths (1.50 -> 150)
//   stake      - int64 paise/cents
//   remaining  - int64 paise/cents
//   timestamp  - int64 unix seconds
//
// This format is cheap to parse in Lua via string.match and avoids cjson on
// every candidate in the matching hot loop.
//
// Stable member ID: the ZSet member is just the betID, so partial fills only
// rewrite the Hash value; the sorted set entry is never touched until the
// order is fully filled or cancelled, preserving price-time priority without
// the ZREM+ZADD churn of the old design.
//
// Composite score (unchanged):
//   Back side: score = price*scoreMultiplier + (epochOffset - timestamp)
//   Lay side:  score = price*scoreMultiplier + timestamp
// -----------------------------------------------------------------------------

// Score multiplier and epoch offset for composite score computation.
const (
	scoreMultiplier int64 = 10000000000 // 1e10
	epochOffset     int64 = 9999999999  // safely above current unix seconds
	// maxCandidates bounds the ZRANGEBYSCORE scan per match call so a deep
	// order book cannot blow up a single Lua invocation.
	maxCandidates = 100
)

// Redis Lua script for atomic order matching with integer arithmetic.
// Contract:
//   KEYS[1] = sameSideZ      (resting zset for incoming order's own side)
//   KEYS[2] = sameSideH      (resting hash for incoming order's own side)
//   KEYS[3] = oppositeZ      (zset we're crossing against)
//   KEYS[4] = oppositeH      (hash we're crossing against)
//   KEYS[5] = suspendedKey
//   ARGV[1] = side              ("back" or "lay")
//   ARGV[2] = orderID           (string / UUID)
//   ARGV[3] = orderUserID       (int64 as string)
//   ARGV[4] = orderPrice        (int64 hundredths)
//   ARGV[5] = orderStake        (int64 paise/cents)
//   ARGV[6] = orderTimestamp    (int64 unix seconds)
//
// Returns a flat Lua array (maps to []interface{} in go-redis):
//   [1] matched   (int, paise/cents)
//   [2] unmatched (int, paise/cents)
//   [3] fills     (array of strings "counterBetID:size:price")
const luaMatchScript = `
local sameZ       = KEYS[1]
local sameH       = KEYS[2]
local oppZ        = KEYS[3]
local oppH        = KEYS[4]
local suspended   = KEYS[5]

local side        = ARGV[1]
local orderID     = ARGV[2]
local orderUserID = ARGV[3]
local orderPrice  = tonumber(ARGV[4])
local orderStake  = tonumber(ARGV[5])
local orderTs     = tonumber(ARGV[6])

if redis.call('EXISTS', suspended) == 1 then
    return redis.error_reply("ERR market is suspended")
end

local SCORE_MULT = 10000000000
local EPOCH_OFF  = 9999999999

local function parseOrder(s)
    -- "userID|price|stake|remaining|timestamp"
    local uid, pr, st, rem, ts = string.match(s, "([^|]+)|([^|]+)|([^|]+)|([^|]+)|([^|]+)")
    return uid, tonumber(pr), tonumber(st), tonumber(rem), tonumber(ts)
end

local function formatOrder(uid, pr, st, rem, ts)
    return uid .. "|" .. pr .. "|" .. st .. "|" .. rem .. "|" .. ts
end

local remaining = orderStake
local fills     = {}
local matched   = 0

local candidates
if side == "back" then
    -- Back crosses lay book at lay.price <= orderPrice; ascending score = lowest price first.
    -- Upper bound: (price+1)*SCORE_MULT is strictly greater than any lay entry priced at orderPrice.
    local maxScore = (orderPrice + 1) * SCORE_MULT
    candidates = redis.call('ZRANGEBYSCORE', oppZ, '-inf', '(' .. maxScore, 'LIMIT', 0, 100)
else
    -- Lay crosses back book at back.price >= orderPrice; descending score = highest price first.
    local minScore = orderPrice * SCORE_MULT
    candidates = redis.call('ZREVRANGEBYSCORE', oppZ, '+inf', minScore, 'LIMIT', 0, 100)
end

for i = 1, #candidates do
    if remaining <= 0 then break end
    local betID = candidates[i]
    local raw = redis.call('HGET', oppH, betID)
    if raw then
        local uid, pr, st, rem, ts = parseOrder(raw)
        -- Price guard: ensure the candidate actually crosses.
        local crosses
        if side == "back" then
            crosses = (pr <= orderPrice)
        else
            crosses = (pr >= orderPrice)
        end
        if not crosses then
            break
        end
        -- Self-trade prevention.
        if uid ~= orderUserID then
            local fillSize = remaining
            if rem < fillSize then fillSize = rem end

            remaining = remaining - fillSize
            rem       = rem - fillSize
            matched   = matched + fillSize

            fills[#fills + 1] = betID .. ":" .. fillSize .. ":" .. pr

            if rem <= 0 then
                redis.call('ZREM', oppZ, betID)
                redis.call('HDEL', oppH, betID)
            else
                -- Partial fill: update hash only. ZSet score is unchanged,
                -- preserving price-time priority without member churn.
                redis.call('HSET', oppH, betID, formatOrder(uid, pr, st, rem, ts))
            end
        end
    end
end

-- Rest the leftover on the book.
if remaining > 0 then
    local score
    if side == "back" then
        score = orderPrice * SCORE_MULT + (EPOCH_OFF - orderTs)
    else
        score = orderPrice * SCORE_MULT + orderTs
    end
    redis.call('ZADD', sameZ, score, orderID)
    redis.call('HSET', sameH, orderID, formatOrder(orderUserID, orderPrice, orderStake, remaining, orderTs))
end

return {matched, remaining, fills}
`

// Lua script for atomic cancel: looks up the order in the per-side Hash,
// verifies ownership, then removes it from both the Hash and the ZSet.
// Returns the compact value string.
const luaCancelScript = `
local zkey   = KEYS[1]
local hkey   = KEYS[2]
local betID  = ARGV[1]
local userID = ARGV[2]

local raw = redis.call('HGET', hkey, betID)
if not raw then
    return redis.error_reply("ERR order not found: " .. betID)
end
local uid = string.match(raw, "([^|]+)|")
if uid ~= userID then
    return redis.error_reply("ERR order belongs to another user")
end
redis.call('HDEL', hkey, betID)
redis.call('ZREM', zkey, betID)
return raw
`

type Engine struct {
	redis        *redis.Client
	logger       *slog.Logger
	matchScript  *redis.Script
	cancelScript *redis.Script

	// marketKeysCache avoids rebuilding per-market Redis key strings on every
	// bet. Keys are stable for a given marketID, so a sync.Map works well.
	marketKeysCache sync.Map // marketID -> *marketKeys
}

// marketKeys bundles all per-market Redis key strings together so a single
// sync.Map lookup returns everything we need for a bet.
type marketKeys struct {
	backZ     string
	backH     string
	layZ      string
	layH      string
	suspended string
}

func (e *Engine) keysFor(marketID string) *marketKeys {
	if v, ok := e.marketKeysCache.Load(marketID); ok {
		return v.(*marketKeys)
	}
	mk := &marketKeys{
		backZ:     "ob:" + marketID + ":back:z",
		backH:     "ob:" + marketID + ":back:h",
		layZ:      "ob:" + marketID + ":lay:z",
		layH:      "ob:" + marketID + ":lay:h",
		suspended: "market:suspended:" + marketID,
	}
	actual, _ := e.marketKeysCache.LoadOrStore(marketID, mk)
	return actual.(*marketKeys)
}

// luaMatchResult is the typed decoding target for the Lua match script's flat
// array return. Populated directly from the []interface{} that go-redis hands
// us, with no intermediate map[string]interface{} or json.Unmarshal.
type luaMatchResult struct {
	Matched   int64
	Unmatched int64
	// Fills are raw "betID:size:price" strings from the Lua script.
	Fills []string
}

func NewEngine(rdb *redis.Client, logger *slog.Logger) *Engine {
	return &Engine{
		redis:        rdb,
		logger:       logger,
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

// formatCompactOrder assembles the pipe-separated order value without any
// fmt.Sprintf allocations. Layout: "userID|price|stake|remaining|timestamp".
func formatCompactOrder(userID, price, stake, remaining, timestamp int64) string {
	// 5 int64 fields + 4 separators. int64 max is 19 chars; allocate once.
	buf := make([]byte, 0, 5*20+4)
	buf = strconv.AppendInt(buf, userID, 10)
	buf = append(buf, '|')
	buf = strconv.AppendInt(buf, price, 10)
	buf = append(buf, '|')
	buf = strconv.AppendInt(buf, stake, 10)
	buf = append(buf, '|')
	buf = strconv.AppendInt(buf, remaining, 10)
	buf = append(buf, '|')
	buf = strconv.AppendInt(buf, timestamp, 10)
	return string(buf)
}

// parseCompactOrder reverses formatCompactOrder.
func parseCompactOrder(s string) (userID, price, stake, remaining, timestamp int64, err error) {
	parts := strings.Split(s, "|")
	if len(parts) != 5 {
		err = fmt.Errorf("invalid compact order: %d fields", len(parts))
		return
	}
	if userID, err = strconv.ParseInt(parts[0], 10, 64); err != nil {
		return
	}
	if price, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return
	}
	if stake, err = strconv.ParseInt(parts[2], 10, 64); err != nil {
		return
	}
	if remaining, err = strconv.ParseInt(parts[3], 10, 64); err != nil {
		return
	}
	if timestamp, err = strconv.ParseInt(parts[4], 10, 64); err != nil {
		return
	}
	return
}

// IsMarketSuspended checks whether a market is currently suspended via Redis flag.
func (e *Engine) IsMarketSuspended(ctx context.Context, marketID string) (bool, error) {
	mk := e.keysFor(marketID)
	val, err := e.redis.Exists(ctx, mk.suspended).Result()
	if err != nil {
		return false, fmt.Errorf("check market suspended: %w", err)
	}
	return val > 0, nil
}

// SuspendMarket sets the suspension flag for a market in Redis.
func (e *Engine) SuspendMarket(ctx context.Context, marketID string) error {
	mk := e.keysFor(marketID)
	if err := e.redis.Set(ctx, mk.suspended, "1", 0).Err(); err != nil {
		return fmt.Errorf("suspend market: %w", err)
	}
	e.logger.InfoContext(ctx, "market suspended", "market_id", marketID)
	return nil
}

// ResumeMarket removes the suspension flag for a market in Redis.
func (e *Engine) ResumeMarket(ctx context.Context, marketID string) error {
	mk := e.keysFor(marketID)
	if err := e.redis.Del(ctx, mk.suspended).Err(); err != nil {
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

	price := toIntPrice(req.Price)
	stake := toIntAmount(req.Stake)
	ts := time.Now().Unix()

	mk := e.keysFor(req.MarketID)

	var sameZ, sameH, oppZ, oppH string
	if req.Side == models.BetSideBack {
		sameZ, sameH = mk.backZ, mk.backH
		oppZ, oppH = mk.layZ, mk.layH
	} else {
		sameZ, sameH = mk.layZ, mk.layH
		oppZ, oppH = mk.backZ, mk.backH
	}

	raw, err := e.matchScript.Run(ctx, e.redis,
		[]string{sameZ, sameH, oppZ, oppH, mk.suspended},
		string(req.Side),
		betID,
		strconv.FormatInt(userID, 10),
		strconv.FormatInt(price, 10),
		strconv.FormatInt(stake, 10),
		strconv.FormatInt(ts, 10),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("execute match script: %w", err)
	}

	res, err := decodeLuaMatchResult(raw)
	if err != nil {
		return nil, fmt.Errorf("decode match result: %w", err)
	}

	fills := make([]models.Fill, 0, len(res.Fills))
	for i, f := range res.Fills {
		counterID, fillSize, fillPrice, perr := parseLuaFill(f)
		if perr != nil {
			return nil, fmt.Errorf("parse fill[%d] %q: %w", i, f, perr)
		}
		fills = append(fills, models.Fill{
			CounterBetID: counterID,
			Price:        toFloatPrice(fillPrice),
			Size:         toFloatAmount(fillSize),
			Timestamp:    time.Now(),
		})
	}

	matchedFloat := toFloatAmount(res.Matched)
	unmatchedFloat := toFloatAmount(res.Unmatched)

	status := models.BetStatusUnmatched
	if res.Matched > 0 && res.Unmatched == 0 {
		status = models.BetStatusMatched
	} else if res.Matched > 0 {
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

// decodeLuaMatchResult converts the raw value returned by go-redis into a typed
// luaMatchResult. The match script returns a Lua array, which go-redis decodes
// as []interface{} with int64 integers and string fills. No JSON involved.
func decodeLuaMatchResult(raw interface{}) (*luaMatchResult, error) {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("unexpected match result type: %T", raw)
	}
	if len(arr) != 3 {
		return nil, fmt.Errorf("match result has %d elements, want 3", len(arr))
	}

	matched, err := asInt64(arr[0])
	if err != nil {
		return nil, fmt.Errorf("matched: %w", err)
	}
	unmatched, err := asInt64(arr[1])
	if err != nil {
		return nil, fmt.Errorf("unmatched: %w", err)
	}

	fillsArr, ok := arr[2].([]interface{})
	if !ok {
		// Lua empty table may be decoded as empty slice; accept nil too.
		if arr[2] == nil {
			return &luaMatchResult{Matched: matched, Unmatched: unmatched}, nil
		}
		return nil, fmt.Errorf("fills is not an array: %T", arr[2])
	}
	fills := make([]string, 0, len(fillsArr))
	for i, f := range fillsArr {
		s, ok := f.(string)
		if !ok {
			return nil, fmt.Errorf("fill[%d] is not a string: %T", i, f)
		}
		fills = append(fills, s)
	}
	return &luaMatchResult{Matched: matched, Unmatched: unmatched, Fills: fills}, nil
}

// parseLuaFill splits "betID:size:price". The betID is a UUID which contains no
// colons, so simple index-based splits are safe.
func parseLuaFill(s string) (counterBetID string, size int64, price int64, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		err = fmt.Errorf("expected 3 fields, got %d", len(parts))
		return
	}
	counterBetID = parts[0]
	if size, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return
	}
	if price, err = strconv.ParseInt(parts[2], 10, 64); err != nil {
		return
	}
	return
}

func asInt64(v interface{}) (int64, error) {
	switch n := v.(type) {
	case int64:
		return n, nil
	case int:
		return int64(n), nil
	case float64:
		return int64(n), nil
	case string:
		return strconv.ParseInt(n, 10, 64)
	default:
		return 0, fmt.Errorf("not numeric: %T", v)
	}
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
	mk := e.keysFor(marketID)
	var zkey, hkey string
	if side == models.BetSideBack {
		zkey, hkey = mk.backZ, mk.backH
	} else {
		zkey, hkey = mk.layZ, mk.layH
	}

	result, err := e.cancelScript.Run(ctx, e.redis,
		[]string{zkey, hkey},
		betID, strconv.FormatInt(userID, 10),
	).Result()
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected cancel result type: %T", result)
	}

	uid, price, _, remaining, _, err := parseCompactOrder(resultStr)
	if err != nil {
		return nil, fmt.Errorf("parse cancelled order: %w", err)
	}

	e.logger.InfoContext(ctx, "order cancelled", "bet_id", betID, "market", marketID)
	return &CancelledOrder{
		UserID:    uid,
		Price:     toFloatPrice(price),
		Remaining: toFloatAmount(remaining),
		Side:      side,
	}, nil
}

// GetOrderBook returns the best backs and lays aggregated by price level.
func (e *Engine) GetOrderBook(ctx context.Context, marketID string, depth int) (backs []models.PriceSize, lays []models.PriceSize, err error) {
	mk := e.keysFor(marketID)

	// Best backs: highest composite score first (highest price, earliest time).
	backMembers, err := e.redis.ZRevRange(ctx, mk.backZ, 0, int64(depth-1)).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("get backs: %w", err)
	}
	// Best lays: lowest composite score first.
	layMembers, err := e.redis.ZRange(ctx, mk.layZ, 0, int64(depth-1)).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("get lays: %w", err)
	}

	backAgg, err := aggregateSide(ctx, e.redis, mk.backH, backMembers)
	if err != nil {
		return nil, nil, err
	}
	layAgg, err := aggregateSide(ctx, e.redis, mk.layH, layMembers)
	if err != nil {
		return nil, nil, err
	}

	for price, size := range backAgg {
		backs = append(backs, models.PriceSize{Price: price, Size: size})
	}
	for price, size := range layAgg {
		lays = append(lays, models.PriceSize{Price: price, Size: size})
	}
	return backs, lays, nil
}

func aggregateSide(ctx context.Context, rdb *redis.Client, hashKey string, betIDs []string) (map[float64]float64, error) {
	agg := map[float64]float64{}
	if len(betIDs) == 0 {
		return agg, nil
	}
	raws, err := rdb.HMGet(ctx, hashKey, betIDs...).Result()
	if err != nil {
		return nil, fmt.Errorf("hmget order book: %w", err)
	}
	for i, r := range raws {
		if r == nil {
			continue
		}
		s, ok := r.(string)
		if !ok {
			return nil, fmt.Errorf("order[%d] not a string: %T", i, r)
		}
		_, price, _, remaining, _, perr := parseCompactOrder(s)
		if perr != nil {
			return nil, fmt.Errorf("parse order[%d]: %w", i, perr)
		}
		agg[toFloatPrice(price)] += toFloatAmount(remaining)
	}
	return agg, nil
}

// ExpireOrders removes all resting orders for a market that were placed before the given time.
func (e *Engine) ExpireOrders(ctx context.Context, marketID string, before time.Time) (int, error) {
	beforeSec := before.Unix()
	removed := 0
	mk := e.keysFor(marketID)

	sides := []struct {
		z string
		h string
	}{
		{mk.backZ, mk.backH},
		{mk.layZ, mk.layH},
	}

	for _, sd := range sides {
		var cursor uint64
		for {
			members, nextCursor, err := e.redis.HScan(ctx, sd.h, cursor, "*", 200).Result()
			if err != nil {
				return removed, fmt.Errorf("scan for expiry: %w", err)
			}
			// HSCAN returns [field, value, field, value, ...].
			for i := 0; i < len(members); i += 2 {
				betID := members[i]
				value := members[i+1]
				_, _, _, _, ts, perr := parseCompactOrder(value)
				if perr != nil {
					return removed, fmt.Errorf("parse order for expiry: %w", perr)
				}
				if ts < beforeSec {
					if err := e.redis.HDel(ctx, sd.h, betID).Err(); err != nil {
						return removed, fmt.Errorf("hdel expired order: %w", err)
					}
					if err := e.redis.ZRem(ctx, sd.z, betID).Err(); err != nil {
						return removed, fmt.Errorf("zrem expired order: %w", err)
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

	// Plain INSERT — no ON CONFLICT clause. The bets table is partitioned
	// with composite PK (id, created_at) in migrations/001_initial.sql, so
	// "ON CONFLICT (id)" would fail at runtime — there's no unique index on
	// id alone in a partitioned table. Bet IDs are randomly generated hex
	// strings so a duplicate is essentially impossible; if it ever happens
	// the unique violation will surface as a real error to the caller.
	const insertBetSQL = `INSERT INTO bets (id, market_id, selection_id, user_id, side, price, stake,
		                    matched_stake, unmatched_stake, status, client_ref, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`

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

// RecoverOrders loads all unmatched/partial orders from PostgreSQL back into
// the Redis order book using the new key layout.
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
			priceF   float64
			stakeF   float64
			ts       int64
		)
		if err := rows.Scan(&id, &marketID, &userID, &side, &priceF, &stakeF, &ts); err != nil {
			return loaded, fmt.Errorf("scan order for recovery: %w", err)
		}

		price := toIntPrice(priceF)
		stake := toIntAmount(stakeF)

		mk := e.keysFor(marketID)
		var zkey, hkey string
		var score float64
		if side == "back" {
			zkey, hkey = mk.backZ, mk.backH
			score = float64(price)*float64(scoreMultiplier) + float64(epochOffset-ts)
		} else {
			zkey, hkey = mk.layZ, mk.layH
			score = float64(price)*float64(scoreMultiplier) + float64(ts)
		}

		value := formatCompactOrder(userID, price, stake, stake, ts)

		if err := e.redis.ZAdd(ctx, zkey, redis.Z{Score: score, Member: id}).Err(); err != nil {
			return loaded, fmt.Errorf("recover order zadd: %w", err)
		}
		if err := e.redis.HSet(ctx, hkey, id, value).Err(); err != nil {
			return loaded, fmt.Errorf("recover order hset: %w", err)
		}
		loaded++
	}
	if err := rows.Err(); err != nil {
		return loaded, fmt.Errorf("iterate recovery rows: %w", err)
	}

	e.logger.InfoContext(ctx, "orders recovered from database", "count", loaded)
	return loaded, nil
}
