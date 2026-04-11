package matching

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// EnrichedBet mirrors the monolith's enriched bet payload so the existing
// frontend (navbar exposure breakdown, history page, etc.) keeps working
// unchanged when it hits the matching-engine instead of cmd/server.
//
// Field order and JSON tags intentionally match handleUserBets in
// cmd/server/main.go.
type EnrichedBet struct {
	ID             string    `json:"id"`
	MarketID       string    `json:"market_id"`
	SelectionID    int64     `json:"selection_id"`
	UserID         int64     `json:"user_id"`
	Side           string    `json:"side"`
	Price          float64   `json:"price"`
	Stake          float64   `json:"stake"`
	MatchedStake   float64   `json:"matched_stake"`
	UnmatchedStake float64   `json:"unmatched_stake"`
	Profit         float64   `json:"profit"`
	Status         string    `json:"status"`
	ClientRef      string    `json:"client_ref"`
	CreatedAt      time.Time `json:"created_at"`

	// Enrichment fields (joined from betting.markets / betting.runners).
	MarketName    string  `json:"market_name"`
	SelectionName string  `json:"selection_name"`
	MarketType    string  `json:"market_type"`
	DisplaySide   string  `json:"display_side"`
	ProfitLoss    float64 `json:"profit_loss"`
}

// BetsHistorySummary mirrors the monolith's handleBetsHistory summary payload.
type BetsHistorySummary struct {
	TotalBets  int     `json:"total_bets"`
	TotalStake float64 `json:"total_stake"`
	TotalPnL   float64 `json:"total_pnl"`
	Won        int     `json:"won"`
	Lost       int     `json:"lost"`
	Pending    int     `json:"pending"`
}

// BetsListResult is the envelope returned by ListUserBets.
type BetsListResult struct {
	Bets  []EnrichedBet `json:"bets"`
	Total int           `json:"total"`
	Page  int           `json:"page"`
	Limit int           `json:"limit"`
}

// BetsHistoryResult is the envelope returned by BetsHistory.
type BetsHistoryResult struct {
	Bets    []EnrichedBet      `json:"bets"`
	Summary BetsHistorySummary `json:"summary"`
}

// Position is one runner's net matched exposure in a market.
type Position struct {
	SelectionID   int64   `json:"selection_id"`
	SelectionName string  `json:"selection_name"`
	BackStake     float64 `json:"back_stake"`
	LayStake      float64 `json:"lay_stake"`
	NetStake      float64 `json:"net_stake"`
	Exposure      float64 `json:"exposure"`
}

// PositionsResult is the envelope returned by GetUserPositionsForMarket.
type PositionsResult struct {
	MarketID   string     `json:"market_id"`
	MarketName string     `json:"market_name"`
	MarketType string     `json:"market_type"`
	Positions  []Position `json:"positions"`
}

// status validation mirrors the monolith: we never return pending / partial /
// unmatched bets to the user — those represent in-flight engine state rather
// than a realised exposure.
var validUserBetStatuses = map[string]bool{
	"matched":   true,
	"settled":   true,
	"cancelled": true,
	"void":      true,
}

// ListUserBets returns the bets for a user, optionally filtered by status and
// market, paginated. This is the porting of cmd/server/main.go:handleUserBets
// into the matching-engine, except enrichment is done via SQL joins instead
// of an in-memory store lookup.
//
// The status filter accepts the "open" alias (matched but not yet settled) in
// addition to the raw statuses.
func (h *Handler) ListUserBets(ctx context.Context, userID int64, statusFilter, marketFilter string, page, limit int) (*BetsListResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if page < 1 {
		page = 1
	}

	rows, err := queryEnrichedBets(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}

	filtered := make([]EnrichedBet, 0, len(rows))
	for _, b := range rows {
		if !validUserBetStatuses[b.Status] {
			continue
		}
		if statusFilter == "open" {
			if b.Status != "matched" {
				continue
			}
		} else if statusFilter != "" && b.Status != statusFilter {
			continue
		}
		if marketFilter != "" && b.MarketID != marketFilter {
			continue
		}
		filtered = append(filtered, b)
	}

	total := len(filtered)
	start := (page - 1) * limit
	if start > total {
		start = total
	}
	end := start + limit
	if end > total {
		end = total
	}

	return &BetsListResult{
		Bets:  filtered[start:end],
		Total: total,
		Page:  page,
		Limit: limit,
	}, nil
}

// BetsHistory returns all valid-status bets for the user plus aggregated
// summary stats. Ported from cmd/server/main.go:handleBetsHistory.
//
// Unlike ListUserBets this always returns the full list so the frontend
// history page can show lifetime totals. Callers that need pagination should
// use ListUserBets with an explicit status filter.
func (h *Handler) BetsHistory(ctx context.Context, userID int64) (*BetsHistoryResult, error) {
	rows, err := queryEnrichedBets(ctx, h.db, userID)
	if err != nil {
		return nil, err
	}

	bets := make([]EnrichedBet, 0, len(rows))
	var totalStake, totalPnL float64
	var won, lost, pending int

	for _, b := range rows {
		if !validUserBetStatuses[b.Status] {
			continue
		}
		totalStake += b.Stake
		switch b.Status {
		case "settled":
			totalPnL += b.Profit
			if b.Profit > 0 {
				won++
			} else if b.Profit < 0 {
				lost++
			}
		case "matched":
			pending++
		}
		bets = append(bets, b)
	}

	return &BetsHistoryResult{
		Bets: bets,
		Summary: BetsHistorySummary{
			TotalBets:  len(bets),
			TotalStake: roundMoney(totalStake),
			TotalPnL:   roundMoney(totalPnL),
			Won:        won,
			Lost:       lost,
			Pending:    pending,
		},
	}, nil
}

// GetUserPositionsForMarket computes the user's net matched position for a
// single market, grouped by runner.
//
// The query is intentionally scoped to status='matched' because settled/void
// bets no longer represent an open exposure. For each selection we sum matched
// stake separately for back and lay sides, then derive a simple net exposure
// (back - lay).
func (h *Handler) GetUserPositionsForMarket(ctx context.Context, userID int64, marketID string) (*PositionsResult, error) {
	if marketID == "" {
		return nil, fmt.Errorf("market_id required")
	}

	// Market metadata for the envelope.
	var marketName, marketType sql.NullString
	if err := h.db.QueryRowContext(ctx,
		`SELECT name, market_type FROM betting.markets WHERE id = $1`,
		marketID,
	).Scan(&marketName, &marketType); err != nil && err != sql.ErrNoRows {
		return nil, fmt.Errorf("fetch market: %w", err)
	}

	// Per-runner net matched position. LEFT JOIN runners so that selections
	// without a runner row still return something — bets are the source of
	// truth for a user's exposure, runner names are just cosmetic.
	//
	// We filter on status='matched' only: settled bets have already paid out
	// through the ledger so they no longer contribute to open exposure.
	const q = `
		SELECT b.selection_id,
		       COALESCE(r.name, '') AS selection_name,
		       COALESCE(SUM(CASE WHEN b.side = 'back' THEN b.matched_stake ELSE 0 END), 0) AS back_stake,
		       COALESCE(SUM(CASE WHEN b.side = 'lay'  THEN b.matched_stake ELSE 0 END), 0) AS lay_stake,
		       COALESCE(SUM(CASE WHEN b.side = 'back' THEN b.matched_stake * (b.price - 1)
		                         WHEN b.side = 'lay'  THEN -b.matched_stake * (b.price - 1)
		                         ELSE 0 END), 0) AS exposure
		FROM betting.bets b
		LEFT JOIN betting.runners r
		  ON r.market_id = b.market_id AND r.selection_id = b.selection_id
		WHERE b.user_id = $1
		  AND b.market_id = $2
		  AND b.status = 'matched'
		GROUP BY b.selection_id, r.name
		ORDER BY b.selection_id`

	rows, err := h.db.QueryContext(ctx, q, userID, marketID)
	if err != nil {
		return nil, fmt.Errorf("query positions: %w", err)
	}
	defer rows.Close()

	positions := make([]Position, 0, 4)
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.SelectionID, &p.SelectionName, &p.BackStake, &p.LayStake, &p.Exposure); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		p.NetStake = roundMoney(p.BackStake - p.LayStake)
		p.BackStake = roundMoney(p.BackStake)
		p.LayStake = roundMoney(p.LayStake)
		p.Exposure = roundMoney(p.Exposure)
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate positions: %w", err)
	}

	return &PositionsResult{
		MarketID:   marketID,
		MarketName: marketName.String,
		MarketType: marketType.String,
		Positions:  positions,
	}, nil
}

// queryEnrichedBets pulls every bet for a user along with the joined
// market_name / selection_name / market_type so the caller can avoid N+1
// lookups. Results are sorted newest-first; callers apply filtering and
// pagination in Go because the valid-status filter differs between
// ListUserBets and BetsHistory.
//
// NOTE: the schema stores market_type and display_side on the bet row itself
// (migrations/009) but those can be NULL for older bets written before the
// migration. We coalesce with the market row so the response stays consistent.
func queryEnrichedBets(ctx context.Context, db *sql.DB, userID int64) ([]EnrichedBet, error) {
	const q = `
		SELECT b.id,
		       b.market_id,
		       b.selection_id,
		       b.user_id,
		       b.side,
		       b.price,
		       b.stake,
		       b.matched_stake,
		       b.unmatched_stake,
		       b.profit,
		       b.status,
		       COALESCE(b.client_ref, ''),
		       b.created_at,
		       COALESCE(m.name, '')         AS market_name,
		       COALESCE(r.name, '')         AS selection_name,
		       COALESCE(b.market_type, m.market_type, 'match_odds') AS market_type,
		       COALESCE(b.display_side, '') AS display_side
		FROM betting.bets b
		LEFT JOIN betting.markets m ON m.id = b.market_id
		LEFT JOIN betting.runners r
		  ON r.market_id = b.market_id AND r.selection_id = b.selection_id
		WHERE b.user_id = $1
		ORDER BY b.created_at DESC`

	rows, err := db.QueryContext(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("query user bets: %w", err)
	}
	defer rows.Close()

	var out []EnrichedBet
	for rows.Next() {
		var b EnrichedBet
		if err := rows.Scan(
			&b.ID,
			&b.MarketID,
			&b.SelectionID,
			&b.UserID,
			&b.Side,
			&b.Price,
			&b.Stake,
			&b.MatchedStake,
			&b.UnmatchedStake,
			&b.Profit,
			&b.Status,
			&b.ClientRef,
			&b.CreatedAt,
			&b.MarketName,
			&b.SelectionName,
			&b.MarketType,
			&b.DisplaySide,
		); err != nil {
			return nil, fmt.Errorf("scan bet: %w", err)
		}
		// Display side mirrors the monolith: fancy/session markets use
		// "yes"/"no" instead of back/lay so the history table reads naturally.
		if b.DisplaySide == "" {
			if strings.EqualFold(b.MarketType, "fancy") || strings.EqualFold(b.MarketType, "session") {
				if b.Side == "back" {
					b.DisplaySide = "yes"
				} else {
					b.DisplaySide = "no"
				}
			} else {
				b.DisplaySide = b.Side
			}
		}
		b.ProfitLoss = b.Profit
		out = append(out, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user bets: %w", err)
	}
	return out, nil
}

// roundMoney rounds a currency amount to 2 decimal places so the JSON payload
// stays consistent with the rest of the API.
func roundMoney(v float64) float64 {
	return float64(int64(v*100+0.5*signOf(v))) / 100
}

func signOf(v float64) float64 {
	if v < 0 {
		return -1
	}
	return 1
}
