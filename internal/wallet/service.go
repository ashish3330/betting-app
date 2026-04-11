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
	// Reject non-positive amounts up front. A zero/negative hold would silently
	// reduce the user's exposure (since we add `amount` to it below) and could
	// be abused to free funds without a matching release.
	if amount <= 0 {
		return fmt.Errorf("hold funds: amount must be positive, got %.2f", amount)
	}

	// READ COMMITTED is sufficient: the SELECT ... FOR UPDATE below locks the
	// user row, which gives us the serialization we need for the balance/exposure
	// invariant without the 40001 retry storms SERIALIZABLE causes at scale.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("hold funds: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

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

	// Best-effort Redis update AFTER successful commit. Guard against a nil
	// redis client so the service stays usable in unit tests and in degraded
	// modes where Redis is temporarily unconfigured.
	if s.redis != nil {
		exposureKey := fmt.Sprintf("exposure:user:%d", userID)
		if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", amount).Err(); redisErr != nil {
			s.logger.WarnContext(ctx, "hold funds: redis exposure update failed (best-effort)",
				"user_id", userID, "error", redisErr)
		}
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
	defer func() { _ = tx.Rollback() }()

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

	// Best-effort Redis update AFTER successful commit. Guard against a nil
	// redis client so the service stays usable in unit tests and in degraded
	// modes where Redis is temporarily unconfigured.
	if s.redis != nil {
		exposureKey := fmt.Sprintf("exposure:user:%d", userID)
		if redisErr := s.redis.HIncrByFloat(ctx, exposureKey, "total", -amount).Err(); redisErr != nil {
			s.logger.WarnContext(ctx, "release funds: redis exposure update failed (best-effort)",
				"user_id", userID, "error", redisErr)
		}
	}
	s.invalidateBalance(ctx, userID)

	s.logger.InfoContext(ctx, "funds released", "user_id", userID, "amount", amount, "bet_id", betID)
	return nil
}

// ---------------------------------------------------------------------------
// SettleBet
// ---------------------------------------------------------------------------

// settlementOutcome is the JSON payload we persist alongside the
// settlement_idempotency row so a replay can return the same answer.
type settlementOutcome struct {
	UserID     int64   `json:"user_id"`
	BetID      string  `json:"bet_id"`
	PnL        float64 `json:"pnl"`
	Commission float64 `json:"commission"`
	AppliedAt  string  `json:"applied_at"`
}

// SettleBet credits/debits the user's balance for a settled bet.
//
// Idempotency: betID is used as the idempotency key. Bet IDs are random hex
// strings created at bet placement (see internal/matching) and are globally
// unique, so reusing them here gives us at-most-once semantics without an
// extra parameter at the call site. The first SettleBet for a given betID
// claims a row in betting.settlement_idempotency INSIDE the same transaction
// that mutates the user balance and writes ledger entries; any subsequent
// call short-circuits, returns ErrDuplicateOperation, and reads the original
// outcome back from the idempotency table for diagnostics.
//
// Callers that previously inspected the ledger UNIQUE(reference) constraint
// for duplicate detection will now see ErrDuplicateOperation here first,
// which is the same sentinel they already handle (see
// internal/settlement/service.go).
func (s *Service) SettleBet(ctx context.Context, userID int64, betID string, pnl float64, commission float64) error {
	if betID == "" {
		return fmt.Errorf("settle bet: betID is required for idempotency")
	}

	// READ COMMITTED + row lock via FOR UPDATE below.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("settle bet: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Claim the idempotency key first. If a row already exists for this
	// betID we treat the operation as already applied and return the
	// recorded outcome via ErrDuplicateOperation.
	outcome := settlementOutcome{
		UserID:     userID,
		BetID:      betID,
		PnL:        pnl,
		Commission: commission,
		AppliedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	outcomeJSON, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("settle bet: marshal outcome: %w", err)
	}
	idemRes, err := tx.ExecContext(ctx,
		`INSERT INTO betting.settlement_idempotency (key, applied_at, result_json)
		 VALUES ($1, NOW(), $2::jsonb)
		 ON CONFLICT (key) DO NOTHING`,
		betID, string(outcomeJSON),
	)
	if err != nil {
		return fmt.Errorf("settle bet: claim idempotency key: %w", err)
	}
	claimed, err := idemRes.RowsAffected()
	if err != nil {
		return fmt.Errorf("settle bet: idempotency rows affected: %w", err)
	}
	if claimed == 0 {
		// Row already exists. Roll back our (no-op) tx and read the prior
		// outcome from a fresh statement so the caller can log it.
		_ = tx.Rollback()
		var priorJSON []byte
		if err := s.db.QueryRowContext(ctx,
			`SELECT result_json FROM betting.settlement_idempotency WHERE key = $1`,
			betID,
		).Scan(&priorJSON); err == nil {
			s.logger.InfoContext(ctx, "settle bet: duplicate, returning prior outcome",
				"bet_id", betID, "user_id", userID, "prior", string(priorJSON))
		} else {
			s.logger.InfoContext(ctx, "settle bet: duplicate, prior outcome unavailable",
				"bet_id", betID, "user_id", userID, "error", err)
		}
		return ErrDuplicateOperation
	}

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

	// Settlement ledger entry. The ON CONFLICT here is now belt-and-braces
	// since the idempotency table above already gates re-entry, but we keep
	// it so a manually-inserted ledger row doesn't blow up.
	ref := fmt.Sprintf("settlement:%s", betID)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
		 VALUES ($1, $2, 'settlement', $3, $4, NOW())
		 ON CONFLICT (reference) DO NOTHING`,
		userID, pnl, ref, betID,
	); err != nil {
		return fmt.Errorf("settle bet: insert settlement ledger: %w", err)
	}

	// Commission entry if applicable
	if commission > 0 {
		commRef := fmt.Sprintf("commission:%s", betID)
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO ledger (user_id, amount, type, reference, bet_id, created_at)
			 VALUES ($1, $2, 'commission', $3, $4, NOW())
			 ON CONFLICT (reference) DO NOTHING`,
			userID, -commission, commRef, betID,
		); err != nil {
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

	s.invalidateBalance(ctx, userID)

	s.logger.InfoContext(ctx, "bet settled",
		"user_id", userID, "bet_id", betID, "pnl", pnl, "commission", commission)
	return nil
}

// EnsureSettlementIdempotencySchema creates the betting.settlement_idempotency
// table if it does not already exist. Call once at service startup before
// SettleBet is invoked. The table is intentionally narrow: a primary key on
// the idempotency key, an applied_at timestamp for forensics/cleanup, and a
// JSONB blob holding the recorded outcome so that duplicate calls can return
// the original answer.
func EnsureSettlementIdempotencySchema(db *sql.DB) error {
	if db == nil {
		return errors.New("ensure settlement idempotency schema: db is nil")
	}
	const stmt = `
CREATE TABLE IF NOT EXISTS betting.settlement_idempotency (
    key         TEXT PRIMARY KEY,
    applied_at  TIMESTAMPTZ DEFAULT NOW(),
    result_json JSONB
)`
	if _, err := db.Exec(stmt); err != nil {
		return fmt.Errorf("create settlement_idempotency table: %w", err)
	}
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
	defer func() { _ = tx.Rollback() }()

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
	defer func() { _ = tx.Rollback() }()

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
// GetPaymentHistory
// ---------------------------------------------------------------------------

// PaymentHistoryEntry is a lightweight view of a payment_transactions row
// returned by the wallet service's deposit/withdrawal listing aliases. It
// intentionally lives in the wallet package to avoid an import cycle with
// the payment package.
type PaymentHistoryEntry struct {
	ID            string     `json:"id"`
	UserID        int64      `json:"user_id"`
	Direction     string     `json:"direction"`
	Method        string     `json:"method"`
	Amount        float64    `json:"amount"`
	Currency      string     `json:"currency"`
	Status        string     `json:"status"`
	ProviderRef   string     `json:"provider_ref,omitempty"`
	UPIID         string     `json:"upi_id,omitempty"`
	WalletAddress string     `json:"wallet_address,omitempty"`
	TxHash        string     `json:"tx_hash,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
}

// GetPaymentHistory returns the payment_transactions rows for a user filtered
// by direction ("deposit" or "withdrawal"), newest first. NULL text columns
// are COALESCED to empty strings so the scan is safe into plain `string`.
func (s *Service) GetPaymentHistory(ctx context.Context, userID int64, direction string, limit, offset int) ([]*PaymentHistoryEntry, error) {
	if direction != "deposit" && direction != "withdrawal" {
		return nil, fmt.Errorf("get payment history: invalid direction %q", direction)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, direction, method, amount, currency, status,
		        COALESCE(provider_ref, ''), COALESCE(upi_id, ''),
		        COALESCE(wallet_address, ''), COALESCE(tx_hash, ''),
		        created_at, completed_at
		 FROM payment_transactions
		 WHERE user_id = $1 AND direction = $2
		 ORDER BY created_at DESC
		 LIMIT $3 OFFSET $4`,
		userID, direction, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get payment history: query: %w", err)
	}
	defer rows.Close()

	entries := make([]*PaymentHistoryEntry, 0)
	for rows.Next() {
		e := &PaymentHistoryEntry{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.Direction, &e.Method, &e.Amount,
			&e.Currency, &e.Status, &e.ProviderRef, &e.UPIID, &e.WalletAddress,
			&e.TxHash, &e.CreatedAt, &e.CompletedAt); err != nil {
			return nil, fmt.Errorf("get payment history: scan: %w", err)
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get payment history: rows iteration: %w", err)
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
