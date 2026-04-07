package models

import (
	"time"

	"github.com/lotus-exchange/lotus-exchange/pkg/validator"
)

type BetSide string

const (
	BetSideBack BetSide = "back"
	BetSideLay  BetSide = "lay"
)

type BetStatus string

const (
	BetStatusPending   BetStatus = "pending"
	BetStatusMatched   BetStatus = "matched"
	BetStatusPartial   BetStatus = "partial"
	BetStatusUnmatched BetStatus = "unmatched"
	BetStatusCancelled BetStatus = "cancelled"
	BetStatusSettled   BetStatus = "settled"
	BetStatusVoid      BetStatus = "void"
)

type Bet struct {
	ID            string    `json:"id" db:"id"`
	MarketID      string    `json:"market_id" db:"market_id"`
	SelectionID   int64     `json:"selection_id" db:"selection_id"`
	UserID        int64     `json:"user_id" db:"user_id"`
	Side          BetSide   `json:"side" db:"side"`
	Price         float64   `json:"price" db:"price"`
	Stake         float64   `json:"stake" db:"stake"`
	MatchedStake  float64   `json:"matched_stake" db:"matched_stake"`
	UnmatchedStake float64  `json:"unmatched_stake" db:"unmatched_stake"`
	Profit        float64   `json:"profit" db:"profit"`
	Status        BetStatus `json:"status" db:"status"`
	ClientRef     string    `json:"client_ref" db:"client_ref"`
	CreatedAt     time.Time `json:"created_at" db:"created_at"`
	MatchedAt     *time.Time `json:"matched_at,omitempty" db:"matched_at"`
	SettledAt     *time.Time `json:"settled_at,omitempty" db:"settled_at"`
}

func (b *Bet) Liability() float64 {
	if b.Side == BetSideBack {
		return b.Stake
	}
	// Lay liability = stake * (price - 1)
	return b.Stake * (b.Price - 1)
}

func (b *Bet) PotentialProfit() float64 {
	if b.Side == BetSideBack {
		return b.Stake * (b.Price - 1)
	}
	return b.Stake
}

type PlaceBetRequest struct {
	MarketID    string  `json:"market_id" validate:"required,min=1,max=100,safe_string"`
	SelectionID int64   `json:"selection_id" validate:"required,gt=0"`
	Side        BetSide `json:"side" validate:"required,betting_side"`
	Price       float64 `json:"price" validate:"required,betting_price"`
	Stake       float64 `json:"stake" validate:"required,gt=0,lte=500000"`
	ClientRef   string  `json:"client_ref" validate:"required,min=1,max=100,safe_string"`
}

func (r *PlaceBetRequest) Validate() error {
	return validator.Validate(r)
}

type MatchResult struct {
	BetID         string  `json:"bet_id"`
	MatchedStake  float64 `json:"matched_stake"`
	UnmatchedStake float64 `json:"unmatched_stake"`
	Status        BetStatus `json:"status"`
	Fills         []Fill  `json:"fills"`
}

type Fill struct {
	CounterBetID string  `json:"counter_bet_id"`
	Price        float64 `json:"price"`
	Size         float64 `json:"size"`
	Timestamp    time.Time `json:"timestamp"`
}

type OddsUpdate struct {
	MarketID  string       `json:"market_id"`
	Runners   []Runner     `json:"runners"`
	Score     *ScoreContext `json:"score,omitempty"`
	Timestamp time.Time    `json:"timestamp"`
}
