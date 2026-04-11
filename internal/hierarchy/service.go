package hierarchy

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
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

func (s *Service) GetChildren(ctx context.Context, userID int64) ([]*models.User, error) {
	var userPath string
	err := s.db.QueryRowContext(ctx, "SELECT path FROM users WHERE id = $1", userID).Scan(&userPath)
	if err != nil {
		return nil, fmt.Errorf("get user path: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users
		 WHERE path <@ $1 AND id != $2
		 ORDER BY path`,
		userPath, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query children: %w", err)
	}
	defer rows.Close()

	var children []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
			&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
			&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		children = append(children, u)
	}
	return children, rows.Err()
}

func (s *Service) GetDirectChildren(ctx context.Context, userID int64) ([]*models.User, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users
		 WHERE parent_id = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query direct children: %w", err)
	}
	defer rows.Close()

	var children []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
			&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
			&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		children = append(children, u)
	}
	return children, rows.Err()
}

func (s *Service) TransferCredit(ctx context.Context, req *models.CreditTransferRequest) error {
	// READ COMMITTED — both user rows are pinned with SELECT ... FOR UPDATE
	// below, which serializes concurrent transfers touching the same rows
	// without the overhead and retry storms of SERIALIZABLE.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Verify parent-child relationship
	var fromRole models.Role
	var fromBalance float64
	err = tx.QueryRowContext(ctx,
		"SELECT role, balance FROM users WHERE id = $1 FOR UPDATE",
		req.FromUserID,
	).Scan(&fromRole, &fromBalance)
	if err != nil {
		return fmt.Errorf("get sender: %w", err)
	}

	var toRole models.Role
	var toParentID *int64
	err = tx.QueryRowContext(ctx,
		"SELECT role, parent_id FROM users WHERE id = $1 FOR UPDATE",
		req.ToUserID,
	).Scan(&toRole, &toParentID)
	if err != nil {
		return fmt.Errorf("get receiver: %w", err)
	}

	if !fromRole.CanManage(toRole) {
		return fmt.Errorf("insufficient hierarchy permissions")
	}

	if toParentID == nil || *toParentID != req.FromUserID {
		return fmt.Errorf("receiver is not a direct child of sender")
	}

	if fromBalance < req.Amount {
		return fmt.Errorf("insufficient balance: %.2f available, %.2f required", fromBalance, req.Amount)
	}

	// Debit sender
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance - $1, updated_at = NOW() WHERE id = $2",
		req.Amount, req.FromUserID,
	)
	if err != nil {
		return fmt.Errorf("debit sender: %w", err)
	}

	// Credit receiver
	_, err = tx.ExecContext(ctx,
		"UPDATE users SET balance = balance + $1, updated_at = NOW() WHERE id = $2",
		req.Amount, req.ToUserID,
	)
	if err != nil {
		return fmt.Errorf("credit receiver: %w", err)
	}

	// Ledger entries
	ref := fmt.Sprintf("transfer:%d:%d:%s", req.FromUserID, req.ToUserID, uuid.New().String())
	_, err = tx.ExecContext(ctx,
		`INSERT INTO ledger (user_id, amount, type, reference, created_at)
		 VALUES ($1, $2, 'transfer', $3, NOW()), ($4, $5, 'transfer', $6, NOW())`,
		req.FromUserID, -req.Amount, ref+":debit",
		req.ToUserID, req.Amount, ref+":credit",
	)
	if err != nil {
		return fmt.Errorf("insert ledger: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	s.logger.InfoContext(ctx, "credit transferred",
		"from", req.FromUserID, "to", req.ToUserID, "amount", req.Amount)
	return nil
}

func (s *Service) GetUser(ctx context.Context, userID int64) (*models.User, error) {
	u := &models.User{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, username, email, role, path, parent_id, balance, exposure,
		        credit_limit, commission_rate, status, created_at, updated_at
		 FROM users WHERE id = $1`,
		userID,
	).Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Path,
		&u.ParentID, &u.Balance, &u.Exposure, &u.CreditLimit,
		&u.CommissionRate, &u.Status, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get user: %w", err)
	}
	return u, nil
}

func (s *Service) UpdateUserStatus(ctx context.Context, userID int64, status string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE users SET status = $1, updated_at = NOW() WHERE id = $2",
		status, userID,
	)
	return err
}

// IsUserSelfExcluded checks whether the given user has an active self-exclusion
// period that has not yet expired.
func (s *Service) IsUserSelfExcluded(ctx context.Context, userID int64) (bool, time.Time, error) {
	var until time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT self_excluded_until FROM responsible_gambling
		 WHERE user_id = $1 AND self_excluded_until > NOW()`,
		userID,
	).Scan(&until)
	if err == sql.ErrNoRows {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, fmt.Errorf("check self-exclusion: %w", err)
	}
	return true, until, nil
}

// GetReferralCode returns the referral_code for a given user, or an empty
// string if the column is blank.
func (s *Service) GetReferralCode(ctx context.Context, userID int64) (string, error) {
	var code sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(referral_code, '') FROM users WHERE id = $1`,
		userID,
	).Scan(&code)
	if err != nil {
		return "", fmt.Errorf("get referral code: %w", err)
	}
	return code.String, nil
}

// ReferralStats is the payload shape consumed by the frontend referral
// dashboard.
type ReferralStats struct {
	ReferralCode   string         `json:"referral_code"`
	ReferralLink   string         `json:"referral_link"`
	TotalReferrals int            `json:"total_referrals"`
	TotalEarnings  float64        `json:"total_earnings"`
	ReferredUsers  []ReferredUser `json:"referred_users"`
}

// ReferredUser is a single row in ReferralStats.ReferredUsers.
type ReferredUser struct {
	Username string  `json:"username"`
	JoinedAt string  `json:"joined_at"`
	Status   string  `json:"status"`
	Earnings float64 `json:"earnings"`
}

// GetReferralStats returns the referral code plus aggregated stats for a user.
//
// The auth.users table in the microservices migration set does not always
// declare a `referred_by` column, so the count-and-aggregate query is
// guarded by a pg_catalog lookup. If the column is not present we simply
// return zero totals rather than erroring out — the caller still gets a
// usable, well-formed response.
func (s *Service) GetReferralStats(ctx context.Context, userID int64) (*ReferralStats, error) {
	code, err := s.GetReferralCode(ctx, userID)
	if err != nil {
		return nil, err
	}

	stats := &ReferralStats{
		ReferralCode:  code,
		ReferralLink:  "https://lotusexchange.com/register?ref=" + code,
		ReferredUsers: []ReferredUser{},
	}

	// Detect column presence once per call — cheap and avoids the need for a
	// global schema cache in what is a read-only listing endpoint.
	var hasReferredBy bool
	if err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM information_schema.columns
		   WHERE table_schema = 'auth'
		     AND table_name = 'users'
		     AND column_name = 'referred_by'
		 )`,
	).Scan(&hasReferredBy); err != nil {
		return nil, fmt.Errorf("check referred_by column: %w", err)
	}

	if !hasReferredBy {
		return stats, nil
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT username, COALESCE(to_char(created_at, 'YYYY-MM-DD"T"HH24:MI:SSOF'), ''), status
		 FROM users
		 WHERE referred_by = $1
		 ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list referred users: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var ru ReferredUser
		if err := rows.Scan(&ru.Username, &ru.JoinedAt, &ru.Status); err != nil {
			return nil, fmt.Errorf("scan referred user: %w", err)
		}
		// Mock earnings formula: 1% of a notional 1000 first-deposit. Real
		// earnings accounting lives in the reporting pipeline; this endpoint
		// is just for the referral dashboard card.
		ru.Earnings = 10.0
		stats.TotalEarnings += ru.Earnings
		stats.ReferredUsers = append(stats.ReferredUsers, ru)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate referred users: %w", err)
	}
	stats.TotalReferrals = len(stats.ReferredUsers)
	return stats, nil
}

func (s *Service) IsAncestor(ctx context.Context, ancestorID, descendantID int64) (bool, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM users u1, users u2
		 WHERE u1.id = $1 AND u2.id = $2 AND u2.path <@ u1.path`,
		ancestorID, descendantID,
	).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
