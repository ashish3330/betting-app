package settlement

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
)

const settlementBatchSize = 500

// SettlementEvent represents a pending wallet operation stored in the outbox table.
type SettlementEvent struct {
	ID         int64   `json:"id"`
	MarketID   string  `json:"market_id"`
	BetID      string  `json:"bet_id"`
	UserID     int64   `json:"user_id"`
	EventType  string  `json:"event_type"` // "settle", "void_release"
	Amount     float64 `json:"amount"`     // P&L or refund amount
	Commission float64 `json:"commission"`
	HeldStake  float64 `json:"held_stake"` // amount to release from exposure
	Status     string  `json:"status"`     // "pending", "processed", "failed"
	CreatedAt  time.Time `json:"created_at"`
}

type Service struct {
	db     *sql.DB
	wallet *wallet.Service
	logger *slog.Logger
}

func NewService(db *sql.DB, walletSvc *wallet.Service, logger *slog.Logger) *Service {
	return &Service{db: db, wallet: walletSvc, logger: logger}
}

type SettlementResult struct {
	MarketID     string  `json:"market_id"`
	WinnerID     int64   `json:"winner_selection_id"`
	BetsSettled  int     `json:"bets_settled"`
	TotalPaidOut float64 `json:"total_paid_out"`
}

type betRow struct {
	ID           string
	UserID       int64
	SelectionID  int64
	Side         models.BetSide
	Price        float64
	MatchedStake float64
	Status       models.BetStatus
}

// betSettlement holds the computed P&L and commission for a single bet.
type betSettlement struct {
	Bet        betRow
	PnL        float64
	Commission float64
	Won        bool
}

func (s *Service) SettleMarket(ctx context.Context, marketID string, winnerSelectionID int64) (*SettlementResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock the market row to prevent double settlement
	var currentStatus string
	err = tx.QueryRowContext(ctx,
		"SELECT status FROM markets WHERE id = $1 FOR UPDATE",
		marketID,
	).Scan(&currentStatus)
	if err != nil {
		return nil, fmt.Errorf("lock market: %w", err)
	}
	if currentStatus == "settled" {
		return nil, fmt.Errorf("market %s is already settled", marketID)
	}
	if currentStatus == "void" {
		return nil, fmt.Errorf("market %s is voided and cannot be settled", marketID)
	}

	// Mark market as settled
	_, err = tx.ExecContext(ctx,
		"UPDATE markets SET status = 'settled', updated_at = NOW() WHERE id = $1",
		marketID,
	)
	if err != nil {
		return nil, fmt.Errorf("update market status: %w", err)
	}

	result := &SettlementResult{
		MarketID: marketID,
		WinnerID: winnerSelectionID,
	}

	// Process bets in batches
	offset := 0
	for {
		settlements, count, err := s.settleBatch(ctx, tx, marketID, winnerSelectionID, offset)
		if err != nil {
			return nil, err
		}

		// Write outbox events and update bet statuses inside the transaction
		for _, stl := range settlements {
			// Update bet status and profit
			_, err = tx.ExecContext(ctx,
				"UPDATE bets SET status = 'settled', profit = $1, settled_at = NOW() WHERE id = $2",
				stl.PnL, stl.Bet.ID,
			)
			if err != nil {
				return nil, fmt.Errorf("settle bet %s: %w", stl.Bet.ID, err)
			}

			// Write settlement event to outbox (wallet ops happen later from outbox)
			_, err = tx.ExecContext(ctx,
				`INSERT INTO settlement_events
				     (market_id, bet_id, user_id, event_type, amount, commission, held_stake, status, created_at)
				 VALUES ($1, $2, $3, 'settle', $4, $5, $6, 'pending', NOW())`,
				marketID, stl.Bet.ID, stl.Bet.UserID,
				stl.PnL, stl.Commission, stl.Bet.MatchedStake,
			)
			if err != nil {
				return nil, fmt.Errorf("write outbox for bet %s: %w", stl.Bet.ID, err)
			}

			result.BetsSettled++
			if stl.PnL > 0 {
				result.TotalPaidOut += stl.PnL - stl.Commission
			}
		}

		if count < settlementBatchSize {
			break
		}
		offset += settlementBatchSize
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	// Process outbox events (best-effort; ProcessOutbox can be retried independently)
	if err := s.ProcessOutbox(ctx); err != nil {
		s.logger.WarnContext(ctx, "outbox processing failed after settlement, will retry",
			"market_id", marketID, "error", err)
	}

	s.logger.InfoContext(ctx, "market settled",
		"market_id", marketID, "winner", winnerSelectionID,
		"bets", result.BetsSettled, "payout", result.TotalPaidOut)

	return result, nil
}

// settleBatch fetches and computes P&L for a batch of bets. Returns the settlements,
// the number of bets fetched (to know if there are more), and any error.
func (s *Service) settleBatch(
	ctx context.Context,
	tx *sql.Tx,
	marketID string,
	winnerSelectionID int64,
	offset int,
) ([]betSettlement, int, error) {

	rows, err := tx.QueryContext(ctx,
		`SELECT id, user_id, selection_id, side, price, matched_stake, status
		 FROM bets
		 WHERE market_id = $1 AND status IN ('matched', 'partial')
		 ORDER BY id
		 LIMIT $2 OFFSET $3
		 FOR UPDATE`,
		marketID, settlementBatchSize, offset,
	)
	if err != nil {
		return nil, 0, fmt.Errorf("get bets batch: %w", err)
	}
	defer rows.Close()

	var bets []betRow
	for rows.Next() {
		var b betRow
		if err := rows.Scan(&b.ID, &b.UserID, &b.SelectionID, &b.Side, &b.Price, &b.MatchedStake, &b.Status); err != nil {
			return nil, 0, fmt.Errorf("scan bet: %w", err)
		}
		bets = append(bets, b)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate bets: %w", err)
	}

	// Single P&L + commission calculation loop
	settlements := make([]betSettlement, 0, len(bets))
	for _, bet := range bets {
		won := (bet.SelectionID == winnerSelectionID && bet.Side == models.BetSideBack) ||
			(bet.SelectionID != winnerSelectionID && bet.Side == models.BetSideLay)

		var pnl float64
		if won {
			if bet.Side == models.BetSideBack {
				pnl = bet.MatchedStake * (bet.Price - 1)
			} else {
				pnl = bet.MatchedStake
			}
		} else {
			if bet.Side == models.BetSideBack {
				pnl = -bet.MatchedStake
			} else {
				pnl = -bet.MatchedStake * (bet.Price - 1)
			}
		}

		// Commission from database, not hardcoded
		var commission float64
		if pnl > 0 {
			var commRate float64
			err := tx.QueryRowContext(ctx,
				"SELECT COALESCE(commission_rate, 2.0) FROM users WHERE id = $1",
				bet.UserID,
			).Scan(&commRate)
			if err != nil {
				return nil, 0, fmt.Errorf("get commission rate for user %d: %w", bet.UserID, err)
			}
			commission = pnl * commRate / 100
		}

		settlements = append(settlements, betSettlement{
			Bet:        bet,
			PnL:        pnl,
			Commission: commission,
			Won:        won,
		})
	}

	return settlements, len(bets), nil
}

func (s *Service) VoidMarket(ctx context.Context, marketID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock market row to prevent double void / concurrent settlement
	var currentStatus string
	err = tx.QueryRowContext(ctx,
		"SELECT status FROM markets WHERE id = $1 FOR UPDATE",
		marketID,
	).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("lock market for void: %w", err)
	}
	if currentStatus == "void" {
		return fmt.Errorf("market %s is already voided", marketID)
	}
	if currentStatus == "settled" {
		return fmt.Errorf("market %s is already settled and cannot be voided", marketID)
	}

	// Void all bets and collect info for outbox
	rows, err := tx.QueryContext(ctx,
		`UPDATE bets SET status = 'void', settled_at = NOW()
		 WHERE market_id = $1 AND status IN ('matched', 'partial', 'unmatched', 'pending')
		 RETURNING id, user_id, stake`,
		marketID,
	)
	if err != nil {
		return fmt.Errorf("void bets: %w", err)
	}
	defer rows.Close()

	type voidedBet struct {
		ID     string
		UserID int64
		Stake  float64
	}

	var voided []voidedBet
	for rows.Next() {
		var v voidedBet
		if err := rows.Scan(&v.ID, &v.UserID, &v.Stake); err != nil {
			return fmt.Errorf("scan voided bet: %w", err)
		}
		voided = append(voided, v)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate voided bets: %w", err)
	}

	// Write void release events to outbox inside the transaction
	for _, v := range voided {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO settlement_events
			     (market_id, bet_id, user_id, event_type, amount, commission, held_stake, status, created_at)
			 VALUES ($1, $2, $3, 'void_release', 0, 0, $4, 'pending', NOW())`,
			marketID, v.ID, v.UserID, v.Stake,
		)
		if err != nil {
			return fmt.Errorf("write void outbox for bet %s: %w", v.ID, err)
		}
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE markets SET status = 'void', updated_at = NOW() WHERE id = $1", marketID)
	if err != nil {
		return fmt.Errorf("update market to void: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit void: %w", err)
	}

	// Process outbox events (best-effort)
	if err := s.ProcessOutbox(ctx); err != nil {
		s.logger.WarnContext(ctx, "outbox processing failed after void, will retry",
			"market_id", marketID, "error", err)
	}

	s.logger.InfoContext(ctx, "market voided", "market_id", marketID, "bets_voided", len(voided))
	return nil
}

// ProcessOutbox reads pending settlement events and executes the corresponding
// wallet operations. This is idempotent and safe to retry.
func (s *Service) ProcessOutbox(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, market_id, bet_id, user_id, event_type, amount, commission, held_stake
		 FROM settlement_events
		 WHERE status = 'pending'
		 ORDER BY id ASC
		 LIMIT 1000`,
	)
	if err != nil {
		return fmt.Errorf("query outbox: %w", err)
	}
	defer rows.Close()

	var events []SettlementEvent
	for rows.Next() {
		var ev SettlementEvent
		if err := rows.Scan(&ev.ID, &ev.MarketID, &ev.BetID, &ev.UserID,
			&ev.EventType, &ev.Amount, &ev.Commission, &ev.HeldStake); err != nil {
			return fmt.Errorf("scan outbox event: %w", err)
		}
		events = append(events, ev)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate outbox: %w", err)
	}

	for _, ev := range events {
		var processErr error

		switch ev.EventType {
		case "settle":
			// Release held funds then settle P&L
			if err := s.wallet.ReleaseFunds(ctx, ev.UserID, ev.HeldStake, ev.BetID); err != nil {
				processErr = fmt.Errorf("release funds for bet %s: %w", ev.BetID, err)
			}
			if processErr == nil {
				if err := s.wallet.SettleBet(ctx, ev.UserID, ev.BetID, ev.Amount, ev.Commission); err != nil {
					processErr = fmt.Errorf("settle bet %s: %w", ev.BetID, err)
				}
			}

		case "void_release":
			// Just release the held stake
			if err := s.wallet.ReleaseFunds(ctx, ev.UserID, ev.HeldStake, ev.BetID); err != nil {
				processErr = fmt.Errorf("void release for bet %s: %w", ev.BetID, err)
			}

		default:
			processErr = fmt.Errorf("unknown event type: %s", ev.EventType)
		}

		newStatus := "processed"
		if processErr != nil {
			newStatus = "failed"
			s.logger.ErrorContext(ctx, "outbox event failed",
				"event_id", ev.ID, "bet_id", ev.BetID, "error", processErr)
		}

		_, err := s.db.ExecContext(ctx,
			"UPDATE settlement_events SET status = $1, processed_at = NOW() WHERE id = $2",
			newStatus, ev.ID,
		)
		if err != nil {
			s.logger.ErrorContext(ctx, "failed to update outbox status",
				"event_id", ev.ID, "error", err)
		}
	}

	return nil
}

// RollbackSettlement reverts a settled market back to its pre-settlement state.
// It re-opens the market, resets bet statuses, and writes compensating outbox events.
func (s *Service) RollbackSettlement(ctx context.Context, marketID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin rollback tx: %w", err)
	}
	defer tx.Rollback()

	// Lock and verify market is settled
	var currentStatus string
	err = tx.QueryRowContext(ctx,
		"SELECT status FROM markets WHERE id = $1 FOR UPDATE",
		marketID,
	).Scan(&currentStatus)
	if err != nil {
		return fmt.Errorf("lock market for rollback: %w", err)
	}
	if currentStatus != "settled" {
		return fmt.Errorf("market %s is not settled (status: %s), cannot rollback", marketID, currentStatus)
	}

	// Get all settled bets for this market to generate compensating events
	rows, err := tx.QueryContext(ctx,
		`SELECT id, user_id, matched_stake, profit
		 FROM bets
		 WHERE market_id = $1 AND status = 'settled'
		 FOR UPDATE`,
		marketID,
	)
	if err != nil {
		return fmt.Errorf("get settled bets for rollback: %w", err)
	}
	defer rows.Close()

	type settledBet struct {
		ID           string
		UserID       int64
		MatchedStake float64
		Profit       float64
	}

	var bets []settledBet
	for rows.Next() {
		var b settledBet
		if err := rows.Scan(&b.ID, &b.UserID, &b.MatchedStake, &b.Profit); err != nil {
			return fmt.Errorf("scan settled bet: %w", err)
		}
		bets = append(bets, b)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate settled bets: %w", err)
	}

	// Revert bets back to their pre-settlement status
	_, err = tx.ExecContext(ctx,
		`UPDATE bets SET status = 'matched', profit = 0, settled_at = NULL
		 WHERE market_id = $1 AND status = 'settled'`,
		marketID,
	)
	if err != nil {
		return fmt.Errorf("revert bet statuses: %w", err)
	}

	// Write compensating settlement events (reverse the P&L)
	for _, bet := range bets {
		_, err = tx.ExecContext(ctx,
			`INSERT INTO settlement_events
			     (market_id, bet_id, user_id, event_type, amount, commission, held_stake, status, created_at)
			 VALUES ($1, $2, $3, 'settle', $4, 0, 0, 'pending', NOW())`,
			marketID, bet.ID, bet.UserID,
			-bet.Profit, // reverse the original P&L
		)
		if err != nil {
			return fmt.Errorf("write rollback outbox for bet %s: %w", bet.ID, err)
		}
	}

	// Re-open the market
	_, err = tx.ExecContext(ctx,
		"UPDATE markets SET status = 'open', updated_at = NOW() WHERE id = $1",
		marketID,
	)
	if err != nil {
		return fmt.Errorf("reopen market: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit rollback: %w", err)
	}

	// Process outbox to apply compensating wallet operations
	if err := s.ProcessOutbox(ctx); err != nil {
		s.logger.WarnContext(ctx, "outbox processing failed after rollback, will retry",
			"market_id", marketID, "error", err)
	}

	s.logger.InfoContext(ctx, "settlement rolled back",
		"market_id", marketID, "bets_reverted", len(bets))

	return nil
}
