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
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

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
