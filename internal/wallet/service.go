package wallet

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

type Service struct {
	db     *sql.DB
	redis  *redis.Client
	logger *slog.Logger
}

func NewService(db *sql.DB, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{db: db, redis: rdb, logger: logger}
}

// ---------------------------------------------------------------------------
// GetBalance
// ---------------------------------------------------------------------------

func (s *Service) GetBalance(ctx context.Context, userID int64) (*models.WalletSummary, error) {
	var summary models.WalletSummary
	summary.UserID = userID

	err := s.db.QueryRowContext(ctx,
		"SELECT balance, exposure FROM users WHERE id = $1", userID,
	).Scan(&summary.Balance, &summary.Exposure)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}

	summary.AvailableBalance = summary.Balance - summary.Exposure

	// Aggregate totals from ledger -- error is now propagated instead of silently ignored.
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(CASE WHEN type = 'deposit' THEN amount ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN type = 'withdrawal' THEN ABS(amount) ELSE 0 END), 0),
		        COALESCE(SUM(CASE WHEN type = 'settlement' THEN amount ELSE 0 END), 0)
		 FROM ledger WHERE user_id = $1`,
		userID,
	).Scan(&summary.TotalDeposits, &summary.TotalWithdrawals, &summary.ProfitLoss)
	if err != nil {
		return nil, fmt.Errorf("aggregate ledger totals: %w", err)
	}

	return &summary, nil
}

// ---------------------------------------------------------------------------
// HoldFunds
// ---------------------------------------------------------------------------

func (s *Service) HoldFunds(ctx context.Context, userID int64, amount float64, betID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("hold funds: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Check balance with row lock
	var balance, exposure float64
	err = tx.QueryRowContext(ctx,
		"SELECT balance, exposure FROM users WHERE id = $1 FOR UPDATE",
		userID,
	).Scan(&balance, &exposure)
	if err != nil {
		return fmt.Errorf("hold funds: get balance: %w", err)
	}

	available := balance - exposure
	if available < amount {
		return fmt.Errorf("hold funds: insufficient balance: available %.2f, required %.2f", available, amount)
	}

	// Increase exposure
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET exposure = exposure + $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("hold funds: update exposure: %w", err)
	}

	// Ledger entry with idempotency
	ref := fmt.Sprintf("hold:%s", betID)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'hold', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, -amount, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("hold funds: insert ledger: %w", err)
	}

	// Best-effort Redis update BEFORE commit so the cache is warm even if
	// the commit succeeds and we crash right after. The DB remains the
	// source of truth -- callers must verify against it.
	exposureKey := fmt.Sprintf("exposure:user:%d", userID)
	if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", amount).Err(); redisErr != nil {
		s.logger.WarnContext(ctx, "hold funds: redis exposure update failed (best-effort)",
			"user_id", userID, "error", redisErr)
	}

	if err := tx.Commit(); err != nil {
		// Attempt to roll back the Redis increment on commit failure.
		if rollbackErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", -amount).Err(); rollbackErr != nil {
			s.logger.WarnContext(ctx, "hold funds: redis rollback failed",
				"user_id", userID, "error", rollbackErr)
		}
		return fmt.Errorf("hold funds: commit: %w", err)
	}

	s.logger.InfoContext(ctx, "funds held", "user_id", userID, "amount", amount, "bet_id", betID)
	return nil
}

// ---------------------------------------------------------------------------
// ReleaseFunds
// ---------------------------------------------------------------------------

func (s *Service) ReleaseFunds(ctx context.Context, userID int64, amount float64, betID string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("release funds: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Read current exposure to detect negative-exposure anomalies instead of
	// silently clamping with GREATEST().
	var currentExposure float64
	err = tx.QueryRowContext(ctx,
		"SELECT exposure FROM users WHERE id = $1 FOR UPDATE", userID,
	).Scan(&currentExposure)
	if err != nil {
		return fmt.Errorf("release funds: get exposure: %w", err)
	}

	newExposure := currentExposure - amount
	if newExposure < 0 {
		s.logger.WarnContext(ctx, "release funds: exposure would go negative, clamping to zero",
			"user_id", userID,
			"current_exposure", currentExposure,
			"release_amount", amount,
			"anomaly_delta", newExposure,
			"bet_id", betID,
		)
		newExposure = 0
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET exposure = $1, updated_at = NOW() WHERE id = $2",
		newExposure, userID,
	)
	if err != nil {
		return fmt.Errorf("release funds: update exposure: %w", err)
	}

	ref := fmt.Sprintf("release:%s", betID)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'release', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, amount, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("release funds: insert ledger: %w", err)
	}

	// Best-effort Redis update before commit.
	exposureKey := fmt.Sprintf("exposure:user:%d", userID)
	if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", -amount).Err(); redisErr != nil {
		s.logger.WarnContext(ctx, "release funds: redis exposure update failed (best-effort)",
			"user_id", userID, "error", redisErr)
	}

	if err := tx.Commit(); err != nil {
		// Attempt to roll back the Redis decrement on commit failure.
		if rollbackErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", amount).Err(); rollbackErr != nil {
			s.logger.WarnContext(ctx, "release funds: redis rollback failed",
				"user_id", userID, "error", rollbackErr)
		}
		return fmt.Errorf("release funds: commit: %w", err)
	}

	s.logger.InfoContext(ctx, "funds released", "user_id", userID, "amount", amount, "bet_id", betID)
	return nil
}

// ---------------------------------------------------------------------------
// SettleBet
// ---------------------------------------------------------------------------

func (s *Service) SettleBet(ctx context.Context, userID int64, betID string, pnl float64, commission float64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("settle bet: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Update balance with P&L
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		pnl, userID,
	)
	if err != nil {
		return fmt.Errorf("settle bet: update balance: %w", err)
	}

	// Settlement ledger entry
	ref := fmt.Sprintf("settlement:%s", betID)
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'settlement', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, pnl, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("settle bet: insert settlement ledger: %w", err)
	}

	// Commission entry if applicable
	if commission > 0 {
		commRef := fmt.Sprintf("commission:%s", betID)
		_, err = tx.ExecContext(ctx,
			`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
			 VALUES ($1, $2, 'commission', $3, $4, NOW())
			 ON CONFLICT (reference) DO NOTHING`,
			userID, -commission, commRef, betID,
		)
		if err != nil {
			return fmt.Errorf("settle bet: insert commission ledger: %w", err)
		}

		_, err = tx.ExecContext(ctx,
			"UPDATE users SET balance = balance - $1 WHERE id = $2",
			commission, userID,
		)
		if err != nil {
			return fmt.Errorf("settle bet: deduct commission: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("settle bet: commit: %w", err)
	}

	s.logger.InfoContext(ctx, "bet settled",
		"user_id", userID, "bet_id", betID, "pnl", pnl, "commission", commission)
	return nil
}

// ---------------------------------------------------------------------------
// Deposit
// ---------------------------------------------------------------------------

func (s *Service) Deposit(ctx context.Context, userID int64, amount float64, reference string) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("deposit: begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("deposit: update balance: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'deposit', $3, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, amount, reference,
	)
	if err != nil {
		return fmt.Errorf("deposit: insert ledger: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("deposit: commit: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Withdraw
// ---------------------------------------------------------------------------

func (s *Service) Withdraw(ctx context.Context, userID int64, amount float64, reference string) error {
	if amount <= 0 {
		return fmt.Errorf("withdraw: amount must be positive")
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("withdraw: begin tx: %w", err)
	}
	defer tx.Rollback()

	var balance, exposure float64
	err = tx.QueryRowContext(ctx,
		"SELECT balance, exposure FROM users WHERE id = $1 FOR UPDATE",
		userID,
	).Scan(&balance, &exposure)
	if err != nil {
		return fmt.Errorf("withdraw: get balance: %w", err)
	}

	available := balance - exposure
	if available < amount {
		return fmt.Errorf("withdraw: insufficient balance: available %.2f, requested %.2f", available, amount)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance - $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("withdraw: update balance: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'withdrawal', $3, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, -amount, reference,
	)
	if err != nil {
		return fmt.Errorf("withdraw: insert ledger: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("withdraw: commit: %w", err)
	}

	s.logger.InfoContext(ctx, "withdrawal completed",
		"user_id", userID, "amount", amount, "reference", reference)
	return nil
}

// ---------------------------------------------------------------------------
// GetStatements
// ---------------------------------------------------------------------------

// GetStatements returns ledger entries for a user within a time range,
// ordered by creation time descending, with pagination support.
func (s *Service) GetStatements(ctx context.Context, userID int64, from, to time.Time, limit, offset int) ([]*models.LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, amount, type, reference, bet_id, market_id, created_at
		 FROM ledger
		 WHERE user_id = $1 AND created_at >= $2 AND created_at <= $3
		 ORDER BY created_at DESC
		 LIMIT $4 OFFSET $5`,
		userID, from, to, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get statements: query: %w", err)
	}
	defer rows.Close()

	var entries []*models.LedgerEntry
	for rows.Next() {
		e := &models.LedgerEntry{}
		err := rows.Scan(&e.ID, &e.UserID, &e.Amount, &e.Type, &e.Reference,
			&e.BetID, &e.MarketID, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("get statements: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get statements: rows iteration: %w", err)
	}
	return entries, nil
}

// ---------------------------------------------------------------------------
// GetLedger
// ---------------------------------------------------------------------------

func (s *Service) GetLedger(ctx context.Context, userID int64, limit, offset int) ([]*models.LedgerEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, amount, type, reference, bet_id, market_id, created_at
		 FROM ledger WHERE user_id = $1
		 ORDER BY created_at DESC
		 LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get ledger: query: %w", err)
	}
	defer rows.Close()

	var entries []*models.LedgerEntry
	for rows.Next() {
		e := &models.LedgerEntry{}
		err := rows.Scan(&e.ID, &e.UserID, &e.Amount, &e.Type, &e.Reference,
			&e.BetID, &e.MarketID, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("get ledger: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get ledger: rows iteration: %w", err)
	}
	return entries, nil
}
