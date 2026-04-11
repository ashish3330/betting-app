package payment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/wallet"
)

// withSerializableRetry executes fn inside a serializable transaction,
// retrying up to maxRetries times on PostgreSQL serialization failures
// (SQLSTATE 40001).
func withSerializableRetry(ctx context.Context, db *sql.DB, maxRetries int, fn func(tx *sql.Tx) error) error {
	for attempt := 0; attempt <= maxRetries; attempt++ {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		err = fn(tx)
		if err != nil {
			tx.Rollback()
			if isSerializationFailure(err) && attempt < maxRetries {
				continue
			}
			return err
		}

		err = tx.Commit()
		if err != nil {
			if isSerializationFailure(err) && attempt < maxRetries {
				continue
			}
			return fmt.Errorf("commit: %w", err)
		}
		return nil
	}
	return fmt.Errorf("serializable transaction failed after %d retries", maxRetries)
}

// isSerializationFailure checks whether the error is a PostgreSQL
// serialization failure (SQLSTATE 40001).
func isSerializationFailure(err error) bool {
	if err == nil {
		return false
	}
	// Check the error message for the PostgreSQL serialization failure code.
	// The lib/pq driver includes the code in the error string.
	msg := err.Error()
	return strings.Contains(msg, "40001") || strings.Contains(msg, "could not serialize access")
}

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

	txID := generateTxID()

	// Insert the payment record and hold funds inside a single serializable
	// transaction with retry logic for serialization failures.
	payTx := &Transaction{
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

	err := withSerializableRetry(ctx, s.db, 3, func(dbTx *sql.Tx) error {
		_, err := dbTx.ExecContext(ctx,
			`INSERT INTO payment_transactions (id, user_id, direction, method, amount, currency, status, upi_id, wallet_address, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			payTx.ID, payTx.UserID, payTx.Direction, payTx.Method, payTx.Amount, payTx.Currency, payTx.Status,
			payTx.UPIId, payTx.WalletAddress, payTx.CreatedAt,
		)
		if err != nil {
			return fmt.Errorf("create withdrawal: %w", err)
		}

		// Hold funds atomically within the same transaction (includes balance check).
		ref := fmt.Sprintf("withdrawal:%s", txID)
		if err := s.wallet.HoldFundsTx(ctx, dbTx, userID, req.Amount, ref); err != nil {
			return fmt.Errorf("hold funds: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("initiate withdrawal: %w", err)
	}

	s.logger.InfoContext(ctx, "withdrawal initiated",
		"tx_id", txID, "user_id", userID, "method", req.Method, "amount", req.Amount)

	return payTx, nil
}

func (s *Service) HandleRazorpayWebhook(ctx context.Context, payload []byte, signature string) error {
	// NOTE: Signature verification is performed at the handler level before
	// this method is called.

	var webhook RazorpayWebhook
	if err := json.Unmarshal(payload, &webhook); err != nil {
		return fmt.Errorf("parse webhook payload: %w", err)
	}

	entity := webhook.Payload.Payment.Entity

	// Extract our internal transaction reference from the notes attached
	// when the payment was created.
	txRef, ok := entity.Notes["tx_id"]
	if !ok || txRef == "" {
		return fmt.Errorf("webhook missing tx_id in notes")
	}

	switch webhook.Event {
	case "payment.captured":
		var tx Transaction
		err := s.db.QueryRowContext(ctx,
			`SELECT id, user_id, amount, status FROM payment_transactions
			 WHERE id = $1 AND status = 'pending'`,
			txRef,
		).Scan(&tx.ID, &tx.UserID, &tx.Amount, &tx.Status)
		if err != nil {
			return fmt.Errorf("transaction not found: %w", err)
		}

		// Razorpay sends amount in paise; convert to rupees.
		return s.completeDeposit(ctx, tx.ID, tx.UserID, tx.Amount, entity.ID)

	case "payment.failed":
		_, err := s.db.ExecContext(ctx,
			`UPDATE payment_transactions SET status = 'failed', updated_at = NOW()
			 WHERE id = $1 AND status = 'pending'`,
			txRef,
		)
		if err != nil {
			return fmt.Errorf("mark transaction failed: %w", err)
		}

		s.logger.InfoContext(ctx, "razorpay payment failed", "tx_id", txRef, "razorpay_id", entity.ID)
		return nil

	default:
		s.logger.InfoContext(ctx, "unhandled razorpay event", "event", webhook.Event)
		return nil
	}
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
	// Wrap payment status update and wallet credit in a single serializable
	// transaction with retry logic for serialization failures.
	err := withSerializableRetry(ctx, s.db, 3, func(dbTx *sql.Tx) error {
		now := time.Now()
		res, err := dbTx.ExecContext(ctx,
			`UPDATE payment_transactions SET status = 'completed', provider_ref = $1, completed_at = $2
			 WHERE id = $3 AND status = 'pending'`,
			providerRef, now, txID,
		)
		if err != nil {
			return fmt.Errorf("update transaction: %w", err)
		}
		rows, err := res.RowsAffected()
		if err != nil {
			return fmt.Errorf("complete deposit: rows affected: %w", err)
		}
		if rows == 0 {
			// Transaction was already completed or no longer pending.
			return fmt.Errorf("transaction %s is not in pending state", txID)
		}

		ref := fmt.Sprintf("deposit:%s", txID)
		if err := s.wallet.DepositTx(ctx, dbTx, userID, amount, ref); err != nil {
			return fmt.Errorf("credit wallet: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("complete deposit: %w", err)
	}

	s.logger.InfoContext(ctx, "deposit completed", "tx_id", txID, "user_id", userID, "amount", amount)
	return nil
}

func (s *Service) GetTransaction(ctx context.Context, txID string) (*Transaction, error) {
	tx := &Transaction{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, direction, method, amount, currency, status,
		        COALESCE(provider_ref, ''), COALESCE(upi_id, ''),
		        COALESCE(wallet_address, ''), COALESCE(tx_hash, ''),
		        created_at, completed_at
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
	// The text columns provider_ref, upi_id, wallet_address and tx_hash are
	// all nullable in the schema, but the Transaction struct scans them into
	// plain `string`. Without COALESCE any row created with NULLs (e.g. a UPI
	// deposit whose wallet_address is unused) would fail the scan with
	// "converting NULL to string is unsupported" — this was the bug that made
	// the list query look "broken".
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, direction, method, amount, currency, status,
		        COALESCE(provider_ref, ''), COALESCE(upi_id, ''),
		        COALESCE(wallet_address, ''), COALESCE(tx_hash, ''),
		        created_at, completed_at
		 FROM payment_transactions WHERE user_id = $1
		 ORDER BY created_at DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get user transactions: query: %w", err)
	}
	defer rows.Close()

	txns := make([]*Transaction, 0)
	for rows.Next() {
		tx := &Transaction{}
		if err := rows.Scan(&tx.ID, &tx.UserID, &tx.Direction, &tx.Method, &tx.Amount,
			&tx.Currency, &tx.Status, &tx.ProviderRef, &tx.UPIId, &tx.WalletAddress,
			&tx.TxHash, &tx.CreatedAt, &tx.CompletedAt); err != nil {
			return nil, fmt.Errorf("get user transactions: scan: %w", err)
		}
		txns = append(txns, tx)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get user transactions: rows iteration: %w", err)
	}
	return txns, nil
}

func (s *Service) verifyRazorpaySignature(payload []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(s.razorpaySecret))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (s *Service) VerifyCryptoSignature(payload []byte, signature string) bool {
	if s.cryptoWebhookKey == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(s.cryptoWebhookKey))
	mac.Write(payload)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func generateTxID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
	}
	return fmt.Sprintf("tx_%s", hex.EncodeToString(b))
}
