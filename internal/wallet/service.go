package wallet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

// ErrDuplicateOperation is returned when an idempotency key (reference) has
// already been consumed. Callers should treat this as a no-op rather than a
// transient failure.
var ErrDuplicateOperation = errors.New("duplicate operation")

// balanceCacheKey is the Redis key prefix for cached wallet summaries.
// A very short TTL keeps latency low for hot reads without letting stale
// values linger long enough to matter for the user experience; mutating
// operations also explicitly invalidate the key after their DB commit.
const (
	balanceCacheKey = "wallet:balance:"
	balanceCacheTTL = 3 * time.Second
)

func balanceKey(userID int64) string {
	return balanceCacheKey + strconv.FormatInt(userID, 10)
}

type Service struct {
	db     *sql.DB
	redis  *redis.Client
	logger *slog.Logger
}

func NewService(db *sql.DB, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{db: db, redis: rdb, logger: logger}
}

// invalidateBalance best-effort deletes the cached balance for a user.
// Errors are swallowed because the DB is the source of truth and the cache
// will expire on its own within balanceCacheTTL.
func (s *Service) invalidateBalance(ctx context.Context, userID int64) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Del(ctx, balanceKey(userID)).Err(); err != nil {
		s.logger.WarnContext(ctx, "wallet: balance cache invalidation failed",
			"user_id", userID, "error", err)
	}
}

// ---------------------------------------------------------------------------
// GetBalance
// ---------------------------------------------------------------------------

func (s *Service) GetBalance(ctx context.Context, userID int64) (*models.WalletSummary, error) {
	// Short-TTL Redis cache in front of the balance read. The balance query
	// plus ledger aggregation is one of the hottest endpoints in the API, so
	// a 3-second cache collapses thundering-herd polling from the dashboard
	// into a single DB hit per user per window.
	key := balanceKey(userID)
	if s.redis != nil {
		if cached, err := s.redis.Get(ctx, key).Bytes(); err == nil {
			var summary models.WalletSummary
			if json.Unmarshal(cached, &summary) == nil {
				return &summary, nil
			}
		}
	}

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

	// Best-effort populate the cache. Failure just means the next request
	// re-reads from the DB.
	if s.redis != nil {
		if data, mErr := json.Marshal(summary); mErr == nil {
			if setErr := s.redis.Set(ctx, key, data, balanceCacheTTL).Err(); setErr != nil {
				s.logger.WarnContext(ctx, "wallet: balance cache set failed",
					"user_id", userID, "error", setErr)
			}
		}
	}

	return &summary, nil
}

// ---------------------------------------------------------------------------
// HoldFunds
// ---------------------------------------------------------------------------

func (s *Service) HoldFunds(ctx context.Context, userID int64, amount float64, betID string) error {
	// READ COMMITTED is sufficient: the SELECT ... FOR UPDATE below locks the
	// user row, which gives us the serialization we need for the balance/exposure
	// invariant without the 40001 retry storms SERIALIZABLE causes at scale.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
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
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'hold', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, -amount, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("hold funds: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hold funds: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("hold funds: commit: %w", err)
	}

	// Best-effort Redis update AFTER successful commit.
	exposureKey := fmt.Sprintf("exposure:user:%d", userID)
	if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", amount).Err(); redisErr != nil {
		s.logger.WarnContext(ctx, "hold funds: redis exposure update failed (best-effort)",
			"user_id", userID, "error", redisErr)
	}
	s.invalidateBalance(ctx, userID)

	s.logger.InfoContext(ctx, "funds held", "user_id", userID, "amount", amount, "bet_id", betID)
	return nil
}

// ---------------------------------------------------------------------------
// ReleaseFunds
// ---------------------------------------------------------------------------

func (s *Service) ReleaseFunds(ctx context.Context, userID int64, amount float64, betID string) error {
	// READ COMMITTED + row lock via FOR UPDATE below.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
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
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'release', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, amount, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("release funds: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("release funds: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("release funds: commit: %w", err)
	}

	// Best-effort Redis update AFTER successful commit.
	exposureKey := fmt.Sprintf("exposure:user:%d", userID)
	if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", -amount).Err(); redisErr != nil {
		s.logger.WarnContext(ctx, "release funds: redis exposure update failed (best-effort)",
			"user_id", userID, "error", redisErr)
	}
	s.invalidateBalance(ctx, userID)

	s.logger.InfoContext(ctx, "funds released", "user_id", userID, "amount", amount, "bet_id", betID)
	return nil
}

// ---------------------------------------------------------------------------
// SettleBet
// ---------------------------------------------------------------------------

func (s *Service) SettleBet(ctx context.Context, userID int64, betID string, pnl float64, commission float64) error {
	// READ COMMITTED + row lock via FOR UPDATE below.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("settle bet: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock the row before updating to prevent serialization failures.
	var balance float64
	err = tx.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1 FOR UPDATE", userID,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("settle bet: lock user row: %w", err)
	}

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
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'settlement', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, pnl, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("settle bet: insert settlement ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("settle bet: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	// Commission entry if applicable
	if commission > 0 {
		commRef := fmt.Sprintf("commission:%s", betID)
		commRes, err := tx.ExecContext(ctx,
			`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
			 VALUES ($1, $2, 'commission', $3, $4, NOW())
			 ON CONFLICT (reference) DO NOTHING`,
			userID, -commission, commRef, betID,
		)
		if err != nil {
			return fmt.Errorf("settle bet: insert commission ledger: %w", err)
		}
		commRows, err := commRes.RowsAffected()
		if err != nil {
			return fmt.Errorf("settle bet: commission rows affected: %w", err)
		}
		if commRows == 0 {
			return ErrDuplicateOperation
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

	s.invalidateBalance(ctx, userID)

	s.logger.InfoContext(ctx, "bet settled",
		"user_id", userID, "bet_id", betID, "pnl", pnl, "commission", commission)
	return nil
}

// ---------------------------------------------------------------------------
// Deposit
// ---------------------------------------------------------------------------

func (s *Service) Deposit(ctx context.Context, userID int64, amount float64, reference string) error {
	// READ COMMITTED + row lock via FOR UPDATE below.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("deposit: begin tx: %w", err)
	}
	defer tx.Rollback()

	// Lock the row before updating to prevent serialization failures.
	var balance float64
	err = tx.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1 FOR UPDATE", userID,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("deposit: lock user row: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("deposit: update balance: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'deposit', $3, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, amount, reference,
	)
	if err != nil {
		return fmt.Errorf("deposit: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deposit: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("deposit: commit: %w", err)
	}

	s.invalidateBalance(ctx, userID)
	return nil
}

// SettleBetTx performs a bet settlement within an existing database transaction.
// This allows callers to atomically combine a casino bet insert with the wallet
// settlement.
func (s *Service) SettleBetTx(ctx context.Context, tx *sql.Tx, userID int64, betID string, pnl float64, commission float64) error {
	// Lock the row before updating to prevent serialization failures.
	var balance float64
	err := tx.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1 FOR UPDATE", userID,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("settle bet tx: lock user row: %w", err)
	}

	// Update balance with P&L
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		pnl, userID,
	)
	if err != nil {
		return fmt.Errorf("settle bet tx: update balance: %w", err)
	}

	// Settlement ledger entry
	ref := fmt.Sprintf("settlement:%s", betID)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'settlement', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, pnl, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("settle bet tx: insert settlement ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("settle bet tx: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	// Commission entry if applicable
	if commission > 0 {
		commRef := fmt.Sprintf("commission:%s", betID)
		commRes, err := tx.ExecContext(ctx,
			`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
			 VALUES ($1, $2, 'commission', $3, $4, NOW())
			 ON CONFLICT (reference) DO NOTHING`,
			userID, -commission, commRef, betID,
		)
		if err != nil {
			return fmt.Errorf("settle bet tx: insert commission ledger: %w", err)
		}
		commRows, err := commRes.RowsAffected()
		if err != nil {
			return fmt.Errorf("settle bet tx: commission rows affected: %w", err)
		}
		if commRows == 0 {
			return ErrDuplicateOperation
		}

		_, err = tx.ExecContext(ctx,
			"UPDATE users SET balance = balance - $1 WHERE id = $2",
			commission, userID,
		)
		if err != nil {
			return fmt.Errorf("settle bet tx: deduct commission: %w", err)
		}
	}

	return nil
}

// DepositTx performs a deposit within an existing database transaction. This
// allows callers to atomically combine a payment status update with the wallet
// credit.
func (s *Service) DepositTx(ctx context.Context, tx *sql.Tx, userID int64, amount float64, reference string) error {
	// Lock the row before updating to prevent serialization failures.
	var balance float64
	err := tx.QueryRowContext(ctx,
		"SELECT balance FROM users WHERE id = $1 FOR UPDATE", userID,
	).Scan(&balance)
	if err != nil {
		return fmt.Errorf("deposit tx: lock user row: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("deposit tx: update balance: %w", err)
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'deposit', $3, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, amount, reference,
	)
	if err != nil {
		return fmt.Errorf("deposit tx: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("deposit tx: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}
	return nil
}

// HoldFundsTx performs a hold within an existing database transaction. This
// allows callers to atomically combine payment record creation with the funds
// hold.
func (s *Service) HoldFundsTx(ctx context.Context, tx *sql.Tx, userID int64, amount float64, betID string) error {
	var balance, exposure float64
	err := tx.QueryRowContext(ctx,
		"SELECT balance, exposure FROM users WHERE id = $1 FOR UPDATE",
		userID,
	).Scan(&balance, &exposure)
	if err != nil {
		return fmt.Errorf("hold funds tx: get balance: %w", err)
	}

	available := balance - exposure
	if available < amount {
		return fmt.Errorf("hold funds tx: insufficient balance: available %.2f, required %.2f", available, amount)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE users SET exposure = exposure + $1, updated_at = NOW() WHERE id = $2",
		amount, userID,
	)
	if err != nil {
		return fmt.Errorf("hold funds tx: update exposure: %w", err)
	}

	ref := fmt.Sprintf("hold:%s", betID)
	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'hold', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, -amount, ref, betID,
	)
	if err != nil {
		return fmt.Errorf("hold funds tx: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("hold funds tx: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
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

	// READ COMMITTED + row lock via FOR UPDATE below.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
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

	res, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'withdrawal', $3, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, -amount, reference,
	)
	if err != nil {
		return fmt.Errorf("withdraw: insert ledger: %w", err)
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("withdraw: rows affected: %w", err)
	}
	if rows == 0 {
		return ErrDuplicateOperation
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("withdraw: commit: %w", err)
	}

	s.invalidateBalance(ctx, userID)

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
