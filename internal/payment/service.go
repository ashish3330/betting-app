package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
)

type PaymentMethod string

const (
	PaymentUPI    PaymentMethod = "upi"
	PaymentCrypto PaymentMethod = "crypto"
	PaymentBank   PaymentMethod = "bank"
)

type PaymentStatus string

const (
	PaymentPending   PaymentStatus = "pending"
	PaymentCompleted PaymentStatus = "completed"
	PaymentFailed    PaymentStatus = "failed"
	PaymentRefunded  PaymentStatus = "refunded"
)

type PaymentDirection string

const (
	PaymentDeposit    PaymentDirection = "deposit"
	PaymentWithdrawal PaymentDirection = "withdrawal"
)

type Transaction struct {
	ID            string           `json:"id" db:"id"`
	UserID        int64            `json:"user_id" db:"user_id"`
	Direction     PaymentDirection `json:"direction" db:"direction"`
	Method        PaymentMethod    `json:"method" db:"method"`
	Amount        float64          `json:"amount" db:"amount"`
	Currency      string           `json:"currency" db:"currency"`
	Status        PaymentStatus    `json:"status" db:"status"`
	ProviderRef   string           `json:"provider_ref" db:"provider_ref"`
	UPIId         string           `json:"upi_id,omitempty" db:"upi_id"`
	WalletAddress string           `json:"wallet_address,omitempty" db:"wallet_address"`
	TxHash        string           `json:"tx_hash,omitempty" db:"tx_hash"`
	Metadata      string           `json:"metadata,omitempty" db:"metadata"`
	CreatedAt     time.Time        `json:"created_at" db:"created_at"`
	CompletedAt   *time.Time       `json:"completed_at,omitempty" db:"completed_at"`
}

type UPIDepositRequest struct {
	Amount float64 `json:"amount" validate:"required,gt=0"`
	UPIID  string  `json:"upi_id" validate:"required"`
}

type CryptoDepositRequest struct {
	Amount        float64 `json:"amount" validate:"required,gt=0"`
	Currency      string  `json:"currency" validate:"required"`
	WalletAddress string  `json:"wallet_address" validate:"required"`
}

type WithdrawalRequest struct {
	Amount        float64       `json:"amount" validate:"required,gt=0"`
	Method        PaymentMethod `json:"method" validate:"required"`
	UPIID         string        `json:"upi_id,omitempty"`
	WalletAddress string        `json:"wallet_address,omitempty"`
}

// Webhook payloads from payment providers
type RazorpayWebhook struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID        string  `json:"id"`
				Amount    float64 `json:"amount"` // in paise
				Currency  string  `json:"currency"`
				Status    string  `json:"status"`
				OrderID   string  `json:"order_id"`
				Method    string  `json:"method"`
				VPA       string  `json:"vpa"`
				Notes     map[string]string `json:"notes"`
			} `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

type CryptoWebhook struct {
	TxHash        string  `json:"tx_hash"`
	WalletAddress string  `json:"wallet_address"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	Confirmations int     `json:"confirmations"`
	Status        string  `json:"status"`
	Reference     string  `json:"reference"`
}

type Service struct {
	db               *sql.DB
	wallet           *wallet.Service
	logger           *slog.Logger
	razorpaySecret   string
	cryptoWebhookKey string
	minDeposit       float64
	maxDeposit       float64
	minWithdrawal    float64
	maxWithdrawal    float64
}

func NewService(db *sql.DB, walletSvc *wallet.Service, logger *slog.Logger, razorpaySecret, cryptoKey string) *Service {
	return &Service{
		db:               db,
		wallet:           walletSvc,
		logger:           logger,
		razorpaySecret:   razorpaySecret,
		cryptoWebhookKey: cryptoKey,
		minDeposit:       100,
		maxDeposit:       1000000,
		minWithdrawal:    500,
		maxWithdrawal:    500000,
	}
}

func (s *Service) InitiateUPIDeposit(ctx context.Context, userID int64, req *UPIDepositRequest) (*Transaction, error) {
	if req.Amount < s.minDeposit || req.Amount > s.maxDeposit {
		return nil, fmt.Errorf("amount must be between %.0f and %.0f", s.minDeposit, s.maxDeposit)
	}

	txID := generateTxID()
	tx := &Transaction{
		ID:        txID,
		UserID:    userID,
		Direction: PaymentDeposit,
		Method:    PaymentUPI,
		Amount:    req.Amount,
		Currency:  "INR",
		Status:    PaymentPending,
		UPIId:     req.UPIID,
		CreatedAt: time.Now(),
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO payment_transactions (id, user_id, direction, method, amount, currency, status, upi_id, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tx.ID, tx.UserID, tx.Direction, tx.Method, tx.Amount, tx.Currency, tx.Status, tx.UPIId, tx.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	s.logger.InfoContext(ctx, "UPI deposit initiated",
		"tx_id", txID, "user_id", userID, "amount", req.Amount)

	return tx, nil
}

func (s *Service) InitiateCryptoDeposit(ctx context.Context, userID int64, req *CryptoDepositRequest) (*Transaction, error) {
	if req.Currency != "USDT" && req.Currency != "BTC" && req.Currency != "ETH" {
		return nil, fmt.Errorf("unsupported currency: %s", req.Currency)
	}

	txID := generateTxID()
	tx := &Transaction{
		ID:            txID,
		UserID:        userID,
		Direction:     PaymentDeposit,
		Method:        PaymentCrypto,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Status:        PaymentPending,
		WalletAddress: req.WalletAddress,
		CreatedAt:     time.Now(),
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO payment_transactions (id, user_id, direction, method, amount, currency, status, wallet_address, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		tx.ID, tx.UserID, tx.Direction, tx.Method, tx.Amount, tx.Currency, tx.Status, tx.WalletAddress, tx.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create transaction: %w", err)
	}

	s.logger.InfoContext(ctx, "crypto deposit initiated",
		"tx_id", txID, "user_id", userID, "currency", req.Currency, "amount", req.Amount)

	return tx, nil
}

func (s *Service) InitiateWithdrawal(ctx context.Context, userID int64, req *WithdrawalRequest) (*Transaction, error) {
	if req.Amount < s.minWithdrawal || req.Amount > s.maxWithdrawal {
		return nil, fmt.Errorf("withdrawal must be between %.0f and %.0f", s.minWithdrawal, s.maxWithdrawal)
	}

	// Check available balance
	summary, err := s.wallet.GetBalance(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	if summary.AvailableBalance < req.Amount {
		return nil, fmt.Errorf("insufficient balance: available %.2f", summary.AvailableBalance)
	}

	txID := generateTxID()
	tx := &Transaction{
		ID:            txID,
		UserID:        userID,
		Direction:     PaymentWithdrawal,
		Method:        req.Method,
		Amount:        req.Amount,
		Currency:      "INR",
		Status:        PaymentPending,
		UPIId:         req.UPIID,
		WalletAddress: req.WalletAddress,
		CreatedAt:     time.Now(),
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO payment_transactions (id, user_id, direction, method, amount, currency, status, upi_id, wallet_address, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		tx.ID, tx.UserID, tx.Direction, tx.Method, tx.Amount, tx.Currency, tx.Status,
		tx.UPIId, tx.WalletAddress, tx.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create withdrawal: %w", err)
	}

	// Hold withdrawal amount
	ref := fmt.Sprintf("withdrawal:%s", txID)
	if err := s.wallet.HoldFunds(ctx, userID, req.Amount, ref); err != nil {
		return nil, fmt.Errorf("hold funds: %w", err)
	}

	s.logger.InfoContext(ctx, "withdrawal initiated",
		"tx_id", txID, "user_id", userID, "method", req.Method, "amount", req.Amount)

	return tx, nil
}

func (s *Service) HandleRazorpayWebhook(ctx context.Context, payload []byte, signature string) error {
	// Verify webhook signature
	if !s.verifyRazorpaySignature(payload, signature) {
		return fmt.Errorf("invalid webhook signature")
	}

	var webhook RazorpayWebhook
	// Parse payload (simplified - in production use json.Unmarshal)
	_ = webhook

	// Process based on event type
	// payment.captured -> complete deposit
	// payment.failed -> mark as failed

	return nil
}

func (s *Service) HandleCryptoWebhook(ctx context.Context, webhook *CryptoWebhook) error {
	if webhook.Confirmations < 3 {
		return nil // Wait for more confirmations
	}

	// Find pending transaction by reference
	var tx Transaction
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, amount, status FROM payment_transactions
		 WHERE id = $1 AND status = 'pending'`,
		webhook.Reference,
	).Scan(&tx.ID, &tx.UserID, &tx.Amount, &tx.Status)
	if err != nil {
		return fmt.Errorf("transaction not found: %w", err)
	}

	// Complete the deposit
	return s.completeDeposit(ctx, tx.ID, tx.UserID, tx.Amount, webhook.TxHash)
}

func (s *Service) completeDeposit(ctx context.Context, txID string, userID int64, amount float64, providerRef string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE payment_transactions SET status = 'completed', provider_ref = $1, completed_at = $2
		 WHERE id = $3 AND status = 'pending'`,
		providerRef, now, txID,
	)
	if err != nil {
		return fmt.Errorf("update transaction: %w", err)
	}

	ref := fmt.Sprintf("deposit:%s", txID)
	if err := s.wallet.Deposit(ctx, userID, amount, ref); err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}

	s.logger.InfoContext(ctx, "deposit completed", "tx_id", txID, "user_id", userID, "amount", amount)
	return nil
}

func (s *Service) GetTransaction(ctx context.Context, txID string) (*Transaction, error) {
	tx := &Transaction{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, direction, method, amount, currency, status, provider_ref,
		        upi_id, wallet_address, tx_hash, created_at, completed_at
		 FROM payment_transactions WHERE id = $1`,
		txID,
	).Scan(&tx.ID, &tx.UserID, &tx.Direction, &tx.Method, &tx.Amount, &tx.Currency,
		&tx.Status, &tx.ProviderRef, &tx.UPIId, &tx.WalletAddress, &tx.TxHash,
		&tx.CreatedAt, &tx.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("get transaction: %w", err)
	}
	return tx, nil
}

func (s *Service) GetUserTransactions(ctx context.Context, userID int64, limit, offset int) ([]*Transaction, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, direction, method, amount, currency, status, provider_ref,
		        upi_id, wallet_address, tx_hash, created_at, completed_at
		 FROM payment_transactions WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var txns []*Transaction
	for rows.Next() {
		tx := &Transaction{}
		if err := rows.Scan(&tx.ID, &tx.UserID, &tx.Direction, &tx.Method, &tx.Amount,
			&tx.Currency, &tx.Status, &tx.ProviderRef, &tx.UPIId, &tx.WalletAddress,
			&tx.TxHash, &tx.CreatedAt, &tx.CompletedAt); err != nil {
			return nil, err
		}
		txns = append(txns, tx)
	}
	return txns, rows.Err()
}

func (s *Service) verifyRazorpaySignature(payload []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(s.razorpaySecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func generateTxID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("tx_%s", hex.EncodeToString(b))
}
