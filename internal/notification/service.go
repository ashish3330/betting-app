package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
)

// stripHTMLTags removes HTML tags from a string.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// stripControlChars removes ASCII control characters (except space).
var controlCharRe = regexp.MustCompile(`[\x00-\x1f\x7f]`)

// sanitizeText strips HTML tags and control characters from user-facing text.
func sanitizeText(s string) string {
	s = htmlTagRe.ReplaceAllString(s, "")
	s = controlCharRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

type NotificationType string

const (
	NotifBetMatched         NotificationType = "bet_matched"
	NotifBetSettled          NotificationType = "bet_settled"
	NotifDepositComplete     NotificationType = "deposit_complete"
	NotifWithdrawalComplete  NotificationType = "withdrawal_complete"
	NotifCashoutAvailable    NotificationType = "cashout_available"
	NotifKYCUpdate           NotificationType = "kyc_update"
	NotifPromotion           NotificationType = "promotion"
	NotifSystem              NotificationType = "system"
	NotifResponsibleGambling NotificationType = "responsible_gambling"
)

type Notification struct {
	ID        int64            `json:"id"`
	UserID    int64            `json:"user_id"`
	Type      NotificationType `json:"type"`
	Title     string           `json:"title"`
	Message   string           `json:"message"`
	Data      json.RawMessage  `json:"data,omitempty"`
	Read      bool             `json:"read"`
	CreatedAt time.Time        `json:"created_at"`
}

type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewService(db *sql.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

// Send delivers a notification. If idempotencyKey is non-empty, duplicate sends
// with the same key are silently ignored (requires a unique index on
// notifications.idempotency_key).
func (s *Service) Send(ctx context.Context, userID int64, notifType NotificationType, title, message string, data interface{}, idempotencyKey string) error {
	// Sanitize user-facing text to prevent content injection.
	title = sanitizeText(title)
	message = sanitizeText(message)

	var dataJSON []byte
	if data != nil {
		var err error
		dataJSON, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal data: %w", err)
		}
	}

	if idempotencyKey != "" {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO notifications (user_id, type, title, message, data, idempotency_key, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, NOW())
			 ON CONFLICT (idempotency_key) DO NOTHING`,
			userID, notifType, title, message, dataJSON, idempotencyKey,
		)
		if err != nil {
			return fmt.Errorf("send notification: %w", err)
		}
	} else {
		_, err := s.db.ExecContext(ctx,
			`INSERT INTO notifications (user_id, type, title, message, data, created_at)
			 VALUES ($1, $2, $3, $4, $5, NOW())`,
			userID, notifType, title, message, dataJSON,
		)
		if err != nil {
			return fmt.Errorf("send notification: %w", err)
		}
	}

	s.logger.DebugContext(ctx, "notification sent",
		"user_id", userID, "type", notifType, "title", title)
	return nil
}

func (s *Service) GetUserNotifications(ctx context.Context, userID int64, unreadOnly bool, limit, offset int) ([]*Notification, error) {
	query := `SELECT id, user_id, type, title, message, data, read, created_at
	          FROM notifications WHERE user_id = $1`
	args := []interface{}{userID}

	if unreadOnly {
		query += " AND read = false"
	}
	query += " ORDER BY created_at DESC LIMIT $2 OFFSET $3"
	args = append(args, limit, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	notifs := []*Notification{}
	for rows.Next() {
		n := &Notification{}
		// Use []byte for the JSONB data column so NULL rows scan cleanly.
		var dataBytes []byte
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Message,
			&dataBytes, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
		}
		if len(dataBytes) > 0 {
			n.Data = json.RawMessage(dataBytes)
		}
		notifs = append(notifs, n)
	}
	return notifs, rows.Err()
}

func (s *Service) MarkAsRead(ctx context.Context, userID int64, notifID int64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE notifications SET read = true WHERE id = $1 AND user_id = $2",
		notifID, userID,
	)
	return err
}

func (s *Service) MarkAllRead(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE notifications SET read = true WHERE user_id = $1 AND read = false",
		userID,
	)
	return err
}

func (s *Service) GetUnreadCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read = false",
		userID,
	).Scan(&count)
	return count, err
}

// Helper methods for common notifications
func (s *Service) NotifyBetMatched(ctx context.Context, userID int64, betID string, stake, price float64) {
	s.Send(ctx, userID, NotifBetMatched, "Bet Matched",
		fmt.Sprintf("Your bet has been matched - Stake: %.2f @ %.2f", stake, price),
		map[string]interface{}{"bet_id": betID, "stake": stake, "price": price},
		fmt.Sprintf("bet_matched:%s", betID))
}

func (s *Service) NotifyBetSettled(ctx context.Context, userID int64, betID string, pnl float64) {
	title := "Bet Won"
	if pnl < 0 {
		title = "Bet Lost"
	}
	s.Send(ctx, userID, NotifBetSettled, title,
		fmt.Sprintf("P&L: %.2f", pnl),
		map[string]interface{}{"bet_id": betID, "pnl": pnl},
		fmt.Sprintf("bet_settled:%s", betID))
}

func (s *Service) NotifyDeposit(ctx context.Context, userID int64, amount float64, txID string) {
	s.Send(ctx, userID, NotifDepositComplete, "Deposit Successful",
		fmt.Sprintf("%.2f has been credited to your wallet", amount),
		map[string]interface{}{"amount": amount, "tx_id": txID},
		fmt.Sprintf("deposit:%s", txID))
}

func (s *Service) NotifyWithdrawal(ctx context.Context, userID int64, amount float64, txID string) {
	s.Send(ctx, userID, NotifWithdrawalComplete, "Withdrawal Processed",
		fmt.Sprintf("%.2f withdrawal has been processed", amount),
		map[string]interface{}{"amount": amount, "tx_id": txID},
		fmt.Sprintf("withdrawal:%s", txID))
}
