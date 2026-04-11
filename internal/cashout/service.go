package cashout

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
	"github.com/redis/go-redis/v9"
)

type CashoutStatus string

const (
	CashoutOffered  CashoutStatus = "offered"
	CashoutAccepted CashoutStatus = "accepted"
	CashoutExpired  CashoutStatus = "expired"
	CashoutRejected CashoutStatus = "rejected"
)

type CashoutOffer struct {
	ID            string        `json:"id"`
	BetID         string        `json:"bet_id"`
	UserID        int64         `json:"user_id"`
	OriginalStake float64       `json:"original_stake"`
	OriginalPrice float64       `json:"original_price"`
	CashoutAmount float64       `json:"cashout_amount"`
	Status        CashoutStatus `json:"status"`
	OfferedAt     time.Time     `json:"offered_at"`
	AcceptedAt    *time.Time    `json:"accepted_at,omitempty"`
	ExpiresAt     time.Time     `json:"expires_at"`
}

type Service struct {
	db     *sql.DB
	redis  *redis.Client
	wallet *wallet.Service
	logger *slog.Logger
}

func NewService(db *sql.DB, rdb *redis.Client, walletSvc *wallet.Service, logger *slog.Logger) *Service {
	return &Service{db: db, redis: rdb, wallet: walletSvc, logger: logger}
}

// CalculateCashoutAmount determines the cashout value based on current odds
func (s *Service) CalculateCashoutAmount(originalStake, originalPrice, currentPrice float64, side string) float64 {
	if side == "back" {
		// Back bet cashout: user can lock in partial profit or minimize loss
		// cashout = (originalPrice / currentPrice) * originalStake
		cashout := (originalPrice / currentPrice) * originalStake
		// Apply 5% margin for the house
		cashout = cashout * 0.95
		return math.Round(cashout*100) / 100
	}
	// Lay bet cashout
	originalLiability := originalStake * (originalPrice - 1)
	currentLiability := originalStake * (currentPrice - 1)
	cashout := originalStake + (originalLiability - currentLiability)
	cashout = cashout * 0.95
	return math.Round(cashout*100) / 100
}

// GenerateOffer creates a cashout offer for an active bet
func (s *Service) GenerateOffer(ctx context.Context, betID string, userID int64) (*CashoutOffer, error) {
	// Get bet details
	var marketID, side string
	var stake, price, matchedStake float64
	var status string

	err := s.db.QueryRowContext(ctx,
		`SELECT market_id, side, stake, price, matched_stake, status
		 FROM bets WHERE id = $1 AND user_id = $2`,
		betID, userID,
	).Scan(&marketID, &side, &stake, &price, &matchedStake, &status)
	if err != nil {
		return nil, fmt.Errorf("bet not found: %w", err)
	}

	if status != "matched" && status != "partial" {
		return nil, fmt.Errorf("bet not eligible for cashout (status: %s)", status)
	}

	if matchedStake <= 0 {
		return nil, fmt.Errorf("no matched portion to cash out")
	}

	// Get current market price from Redis
	currentPrice, err := s.getCurrentPrice(ctx, marketID, side)
	if err != nil {
		// Fallback: use original price with small adjustment
		currentPrice = price * 1.02
	}

	cashoutAmount := s.CalculateCashoutAmount(matchedStake, price, currentPrice, side)
	if cashoutAmount <= 0 {
		return nil, fmt.Errorf("cashout not available at current odds")
	}

	offer := &CashoutOffer{
		ID:            uuid.New().String(),
		BetID:         betID,
		UserID:        userID,
		OriginalStake: matchedStake,
		OriginalPrice: price,
		CashoutAmount: cashoutAmount,
		Status:        CashoutOffered,
		OfferedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(10 * time.Second), // 10 second window
	}

	// Store offer
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO cashout_offers (id, bet_id, user_id, original_stake, original_price, cashout_amount, status, offered_at, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		offer.ID, offer.BetID, offer.UserID, offer.OriginalStake, offer.OriginalPrice,
		offer.CashoutAmount, offer.Status, offer.OfferedAt, offer.ExpiresAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create offer: %w", err)
	}

	s.logger.InfoContext(ctx, "cashout offer generated",
		"offer_id", offer.ID, "bet_id", betID, "amount", cashoutAmount)

	return offer, nil
}

// AcceptOffer processes a cashout.
// Wallet operations are written to the settlement_events outbox table within the
// same transaction that marks the bet settled and the offer accepted. This avoids
// the previous bug where the transaction could commit but subsequent wallet calls
// could fail, leaving the wallet in an inconsistent state.
func (s *Service) AcceptOffer(ctx context.Context, offerID string, userID int64) (*CashoutOffer, error) {
	// --- Step 1: Check for expiry outside the serializable transaction. ---
	// If the offer is expired, mark it with a simple UPDATE and return early.
	// This avoids the previous confusing pattern of committing the serializable
	// tx just to persist an expiry status.
	var currentStatus string
	var expiresAt time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT status, expires_at FROM cashout_offers WHERE id = $1 AND user_id = $2`,
		offerID, userID,
	).Scan(&currentStatus, &expiresAt)
	if err != nil {
		return nil, fmt.Errorf("offer not found: %w", err)
	}
	if currentStatus != string(CashoutOffered) {
		return nil, fmt.Errorf("offer already %s", currentStatus)
	}
	if time.Now().After(expiresAt) {
		// Mark expired outside the main transaction — this is a simple status update.
		_, _ = s.db.ExecContext(ctx,
			"UPDATE cashout_offers SET status = 'expired' WHERE id = $1 AND status = 'offered'",
			offerID,
		)
		return nil, fmt.Errorf("offer has expired, request a new one")
	}

	// --- Step 2: ReadCommitted transaction for acceptance + outbox write. ---
	// The cashout_offers row is locked with SELECT ... FOR UPDATE below, which
	// serializes concurrent accept attempts on the same offer without the
	// overhead of SERIALIZABLE predicate locks.
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	// Re-check and lock offer inside the serializable tx
	var offer CashoutOffer
	err = tx.QueryRowContext(ctx,
		`SELECT id, bet_id, user_id, original_stake, original_price, cashout_amount, status, offered_at, expires_at
		 FROM cashout_offers WHERE id = $1 AND user_id = $2 FOR UPDATE`,
		offerID, userID,
	).Scan(&offer.ID, &offer.BetID, &offer.UserID, &offer.OriginalStake, &offer.OriginalPrice,
		&offer.CashoutAmount, &offer.Status, &offer.OfferedAt, &offer.ExpiresAt)
	if err != nil {
		return nil, fmt.Errorf("offer not found: %w", err)
	}

	if offer.Status != CashoutOffered {
		return nil, fmt.Errorf("offer already %s", offer.Status)
	}

	if time.Now().After(offer.ExpiresAt) {
		return nil, fmt.Errorf("offer has expired, request a new one")
	}

	// Mark offer as accepted
	now := time.Now()
	_, err = tx.ExecContext(ctx,
		"UPDATE cashout_offers SET status = 'accepted', accepted_at = $1 WHERE id = $2",
		now, offerID,
	)
	if err != nil {
		return nil, fmt.Errorf("accept offer: %w", err)
	}

	// Settle the bet as cashed out
	pnl := offer.CashoutAmount - offer.OriginalStake
	_, err = tx.ExecContext(ctx,
		`UPDATE bets SET status = 'settled', profit = $1, settled_at = $2 WHERE id = $3`,
		pnl, now, offer.BetID,
	)
	if err != nil {
		return nil, fmt.Errorf("settle bet: %w", err)
	}

	// Write cashout settlement event to the outbox within the same transaction.
	// The settlement outbox processor will handle ReleaseFunds + SettleBet atomically.
	_, err = tx.ExecContext(ctx,
		`INSERT INTO settlement_events
		     (market_id, bet_id, user_id, event_type, amount, commission, held_stake, status, created_at)
		 VALUES ('cashout', $1, $2, 'settle', $3, 0, $4, 'pending', NOW())`,
		offer.BetID, userID, pnl, offer.OriginalStake,
	)
	if err != nil {
		return nil, fmt.Errorf("write cashout outbox event: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	offer.Status = CashoutAccepted
	offer.AcceptedAt = &now

	s.logger.InfoContext(ctx, "cashout accepted",
		"offer_id", offerID, "bet_id", offer.BetID, "amount", offer.CashoutAmount)

	return &offer, nil
}

// GetUserOffers returns active cashout offers for a user
func (s *Service) GetUserOffers(ctx context.Context, userID int64) ([]*CashoutOffer, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, bet_id, user_id, original_stake, original_price, cashout_amount, status, offered_at, accepted_at, expires_at
		 FROM cashout_offers WHERE user_id = $1 AND status = 'offered' AND expires_at > NOW()
		 ORDER BY offered_at DESC`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var offers []*CashoutOffer
	for rows.Next() {
		o := &CashoutOffer{}
		if err := rows.Scan(&o.ID, &o.BetID, &o.UserID, &o.OriginalStake, &o.OriginalPrice,
			&o.CashoutAmount, &o.Status, &o.OfferedAt, &o.AcceptedAt, &o.ExpiresAt); err != nil {
			return nil, err
		}
		offers = append(offers, o)
	}
	return offers, rows.Err()
}

func (s *Service) getCurrentPrice(ctx context.Context, marketID, side string) (float64, error) {
	key := fmt.Sprintf("odds:latest:%s", marketID)
	priceStr, err := s.redis.HGet(ctx, key, side+"_price").Result()
	if err != nil {
		return 0, err
	}
	var price float64
	fmt.Sscanf(priceStr, "%f", &price)
	if price <= 1.0 {
		return 0, fmt.Errorf("invalid price")
	}
	return price, nil
}
