package models

import (
	"fmt"
	"time"
)

type MarketStatus string

const (
	MarketOpen      MarketStatus = "open"
	MarketSuspended MarketStatus = "suspended"
	MarketClosed    MarketStatus = "closed"
	MarketSettled   MarketStatus = "settled"
	MarketInPlay    MarketStatus = "in_play"
)

type MarketType string

const (
	MarketTypeMatchOdds         MarketType = "match_odds"
	MarketTypeFancy             MarketType = "fancy"
	MarketTypeBookmaker         MarketType = "bookmaker"
	MarketTypeSession           MarketType = "session"
	MarketTypeToss              MarketType = "toss"
	MarketTypeOverUnder         MarketType = "over_under"
	MarketTypeCorrectScore      MarketType = "correct_score"
	MarketTypeHalfTime          MarketType = "half_time"
	MarketTypeBothTeamsToScore  MarketType = "both_teams_to_score"
	MarketTypeTotalGoals        MarketType = "total_goals"
	MarketTypeHandicap          MarketType = "handicap"
	MarketTypeOutright          MarketType = "outright"
	MarketTypeManOfMatch        MarketType = "man_of_match"
	MarketTypeTopBatsman        MarketType = "top_batsman"
	MarketTypeTopBowler         MarketType = "top_bowler"
	MarketTypePlayerProp        MarketType = "player_prop"
)

type Sport string

const (
	SportCricket     Sport = "cricket"
	SportFootball    Sport = "football"
	SportTennis      Sport = "tennis"
	SportHorseRacing Sport = "horse_racing"
	SportKabaddi     Sport = "kabaddi"
	SportBasketball  Sport = "basketball"
	SportTableTennis Sport = "table_tennis"
	SportEsports     Sport = "esports"
	SportVolleyball  Sport = "volleyball"
	SportIceHockey   Sport = "ice_hockey"
	SportBoxing      Sport = "boxing"
	SportMMA         Sport = "mma"
	SportBadminton   Sport = "badminton"
	SportGolf        Sport = "golf"
)

// AllSports returns the full list of supported sports.
func AllSports() []Sport {
	return []Sport{
		SportCricket, SportFootball, SportTennis, SportHorseRacing,
		SportKabaddi, SportBasketball, SportTableTennis, SportEsports,
		SportVolleyball, SportIceHockey, SportBoxing, SportMMA,
		SportBadminton, SportGolf,
	}
}

// ExchangeMarketTypes are market types that support back/lay exchange trading.
var ExchangeMarketTypes = map[MarketType]bool{
	MarketTypeMatchOdds: true,
	MarketTypeBookmaker: true,
}

type PriceSize struct {
	Price float64 `json:"price"`
	Size  float64 `json:"size"`
}

type Runner struct {
	SelectionID int64       `json:"selection_id"`
	Name        string      `json:"name"`
	Status      string      `json:"status"`
	BackPrices  []PriceSize `json:"back_prices"`
	LayPrices   []PriceSize `json:"lay_prices"`
	LastPrice   float64     `json:"last_price"`
}

type ScoreContext struct {
	MatchID     string  `json:"match_id"`
	Score       string  `json:"score"`
	Overs       string  `json:"overs"`
	Wickets     int     `json:"wickets"`
	RunRate     float64 `json:"run_rate"`
	LastBall    string  `json:"last_ball"`
	Innings     int     `json:"innings"`
	BattingTeam string  `json:"batting_team"`
	// Football / generic fields
	HomeScore int    `json:"home_score,omitempty"`
	AwayScore int    `json:"away_score,omitempty"`
	Period    string `json:"period,omitempty"`
	Clock     string `json:"clock,omitempty"`
	// Tennis fields
	Sets   string `json:"sets,omitempty"`
	Server string `json:"server,omitempty"`
}

type Competition struct {
	ID         string    `json:"id" db:"id"`
	Sport      Sport     `json:"sport" db:"sport"`
	Name       string    `json:"name" db:"name"`
	Region     string    `json:"region" db:"region"`
	StartDate  time.Time `json:"start_date" db:"start_date"`
	EndDate    time.Time `json:"end_date" db:"end_date"`
	Status     string    `json:"status" db:"status"` // active, upcoming, completed
	MatchCount int       `json:"match_count" db:"match_count"`
}

type Event struct {
	ID            string    `json:"id" db:"id"`
	CompetitionID string    `json:"competition_id" db:"competition_id"`
	Sport         Sport     `json:"sport" db:"sport"`
	Name          string    `json:"name" db:"name"`
	HomeTeam      string    `json:"home_team" db:"home_team"`
	AwayTeam      string    `json:"away_team" db:"away_team"`
	StartTime     time.Time `json:"start_time" db:"start_time"`
	Status        string    `json:"status" db:"status"` // upcoming, in_play, completed, cancelled
	InPlay        bool      `json:"in_play" db:"in_play"`
	Score         string    `json:"score" db:"score"`
}

type Market struct {
	ID           string        `json:"id" db:"id"`
	EventID      string        `json:"event_id" db:"event_id"`
	Sport        Sport         `json:"sport" db:"sport"`
	Name         string        `json:"name" db:"name"`
	MarketType   MarketType    `json:"market_type" db:"market_type"`
	Status       MarketStatus  `json:"status" db:"status"`
	Runners      []Runner      `json:"runners"`
	Score        *ScoreContext `json:"score,omitempty"`
	InPlay       bool          `json:"in_play" db:"in_play"`
	StartTime    time.Time     `json:"start_time" db:"start_time"`
	TotalMatched float64       `json:"total_matched" db:"total_matched"`
	CreatedAt    time.Time     `json:"created_at" db:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at" db:"updated_at"`
}

func (m *Market) CanAcceptBets() bool {
	return m.Status == MarketOpen || m.Status == MarketInPlay
}

func (m *Market) Suspend() error {
	if m.Status != MarketOpen && m.Status != MarketInPlay {
		return fmt.Errorf("cannot suspend market from state %q (allowed: open, in_play)", m.Status)
	}
	m.Status = MarketSuspended
	return nil
}

func (m *Market) Resume() error {
	if m.Status != MarketSuspended {
		return fmt.Errorf("cannot resume market from state %q (allowed: suspended)", m.Status)
	}
	m.Status = MarketOpen
	return nil
}

func (m *Market) Close() error {
	if m.Status != MarketOpen && m.Status != MarketSuspended && m.Status != MarketInPlay {
		return fmt.Errorf("cannot close market from state %q (allowed: open, suspended, in_play)", m.Status)
	}
	m.Status = MarketClosed
	return nil
}

// IsExchangeMarket returns true if this market supports back/lay exchange
// trading (match_odds, bookmaker). Other types (fancy, session, etc.) are
// fixed-odds / fancy markets handled by the platform book.
func (m *Market) IsExchangeMarket() bool {
	return ExchangeMarketTypes[m.MarketType]
}
