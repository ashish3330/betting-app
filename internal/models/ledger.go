package models

import "time"

type LedgerType string

const (
	LedgerHold       LedgerType = "hold"
	LedgerRelease    LedgerType = "release"
	LedgerSettlement LedgerType = "settlement"
	LedgerCommission LedgerType = "commission"
	LedgerDeposit    LedgerType = "deposit"
	LedgerWithdrawal LedgerType = "withdrawal"
	LedgerTransfer   LedgerType = "transfer"
)

type LedgerEntry struct {
	ID        int64      `json:"id" db:"id"`
	UserID    int64      `json:"user_id" db:"user_id"`
	Amount    float64    `json:"amount" db:"amount"`
	Type      LedgerType `json:"type" db:"type"`
	Reference string     `json:"reference" db:"reference"`
	BetID     *string    `json:"bet_id,omitempty" db:"bet_id"`
	MarketID  *string    `json:"market_id,omitempty" db:"market_id"`
	Balance   float64    `json:"balance" db:"balance"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
}

type WalletSummary struct {
	UserID         int64   `json:"user_id"`
	Balance        float64 `json:"balance"`
	Exposure       float64 `json:"exposure"`
	AvailableBalance float64 `json:"available_balance"`
	TotalDeposits  float64 `json:"total_deposits"`
	TotalWithdrawals float64 `json:"total_withdrawals"`
	ProfitLoss     float64 `json:"profit_loss"`
}
