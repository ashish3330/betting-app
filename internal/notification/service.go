package notification

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

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

func (s *Service) Send(ctx context.Context, userID int64, notifType NotificationType, title, message string, data interface{}) error {
	var dataJSON []byte
	if data != nil {
		var err error
		dataJSON, err = json.Marshal(data)
		if err != nil {
			return fmt.Errorf("marshal data: %w", err)
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO notifications (user_id, type, title, message, data, created_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		userID, notifType, title, message, dataJSON,
	)
	if err != nil {
		return fmt.Errorf("send notification: %w", err)
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

	var notifs []*Notification
	for rows.Next() {
		n := &Notification{}
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Message,
			&n.Data, &n.Read, &n.CreatedAt); err != nil {
			return nil, err
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
		map[string]interface{}{"bet_id": betID, "stake": stake, "price": price})
}

func (s *Service) NotifyBetSettled(ctx context.Context, userID int64, betID string, pnl float64) {
	title := "Bet Won"
	if pnl < 0 {
		title = "Bet Lost"
	}
	s.Send(ctx, userID, NotifBetSettled, title,
		fmt.Sprintf("P&L: %.2f", pnl),
		map[string]interface{}{"bet_id": betID, "pnl": pnl})
}

func (s *Service) NotifyDeposit(ctx context.Context, userID int64, amount float64, txID string) {
	s.Send(ctx, userID, NotifDepositComplete, "Deposit Successful",
		fmt.Sprintf("%.2f has been credited to your wallet", amount),
		map[string]interface{}{"amount": amount, "tx_id": txID})
}

func (s *Service) NotifyWithdrawal(ctx context.Context, userID int64, amount float64, txID string) {
	s.Send(ctx, userID, NotifWithdrawalComplete, "Withdrawal Processed",
		fmt.Sprintf("%.2f withdrawal has been processed", amount),
		map[string]interface{}{"amount": amount, "tx_id": txID})
}
