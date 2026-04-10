package odds

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

// MockProvider simulates realistic multi-sport odds using
// geometric Brownian motion for price volatility and
// Poisson process for score updates.
type MockProvider struct {
	volatility     float64
	updateInterval time.Duration
	mu             sync.Mutex
	rng            *rand.Rand
}

func NewMockProvider(volatility float64, updateInterval time.Duration) *MockProvider {
	return &MockProvider{
		volatility:     volatility,
		updateInterval: updateInterval,
		rng:            rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (m *MockProvider) Name() string { return "mock" }

// ---------------------------------------------------------------------------
// FetchCompetitions -- returns demo competitions for every sport
// ---------------------------------------------------------------------------

func (m *MockProvider) FetchCompetitions(_ context.Context, sport string) ([]*models.Competition, error) {
	now := time.Now()
	all := map[string][]*models.Competition{
		"cricket": {
			{ID: "comp-ipl-2026", Sport: models.SportCricket, Name: "Indian Premier League 2026", Region: "India", StartDate: now.Add(-15 * 24 * time.Hour), EndDate: now.Add(45 * 24 * time.Hour), Status: "active", MatchCount: 74},
			{ID: "comp-bbl-2026", Sport: models.SportCricket, Name: "Big Bash League 2025-26", Region: "Australia", StartDate: now.Add(-30 * 24 * time.Hour), EndDate: now.Add(10 * 24 * time.Hour), Status: "active", MatchCount: 61},
			{ID: "comp-icc-wc", Sport: models.SportCricket, Name: "ICC World Cup 2026", Region: "International", StartDate: now.Add(60 * 24 * time.Hour), EndDate: now.Add(105 * 24 * time.Hour), Status: "upcoming", MatchCount: 48},
			{ID: "comp-cpl-2026", Sport: models.SportCricket, Name: "Caribbean Premier League 2026", Region: "West Indies", StartDate: now.Add(90 * 24 * time.Hour), EndDate: now.Add(120 * 24 * time.Hour), Status: "upcoming", MatchCount: 34},
		},
		"football": {
			{ID: "comp-epl-2026", Sport: models.SportFootball, Name: "English Premier League 2025-26", Region: "England", StartDate: now.Add(-200 * 24 * time.Hour), EndDate: now.Add(30 * 24 * time.Hour), Status: "active", MatchCount: 380},
			{ID: "comp-laliga-2026", Sport: models.SportFootball, Name: "La Liga 2025-26", Region: "Spain", StartDate: now.Add(-200 * 24 * time.Hour), EndDate: now.Add(30 * 24 * time.Hour), Status: "active", MatchCount: 380},
			{ID: "comp-ucl-2026", Sport: models.SportFootball, Name: "UEFA Champions League 2025-26", Region: "Europe", StartDate: now.Add(-150 * 24 * time.Hour), EndDate: now.Add(60 * 24 * time.Hour), Status: "active", MatchCount: 125},
			{ID: "comp-isl-2026", Sport: models.SportFootball, Name: "Indian Super League 2025-26", Region: "India", StartDate: now.Add(-120 * 24 * time.Hour), EndDate: now.Add(15 * 24 * time.Hour), Status: "active", MatchCount: 115},
		},
		"tennis": {
			{ID: "comp-ao-2026", Sport: models.SportTennis, Name: "Australian Open 2026", Region: "Australia", StartDate: now.Add(-60 * 24 * time.Hour), EndDate: now.Add(-46 * 24 * time.Hour), Status: "completed", MatchCount: 127},
			{ID: "comp-rg-2026", Sport: models.SportTennis, Name: "Roland Garros 2026", Region: "France", StartDate: now.Add(50 * 24 * time.Hour), EndDate: now.Add(64 * 24 * time.Hour), Status: "upcoming", MatchCount: 127},
			{ID: "comp-atp-masters", Sport: models.SportTennis, Name: "ATP Masters 1000 Miami 2026", Region: "USA", StartDate: now.Add(-5 * 24 * time.Hour), EndDate: now.Add(7 * 24 * time.Hour), Status: "active", MatchCount: 96},
		},
		"horse_racing": {
			{ID: "comp-royal-ascot", Sport: models.SportHorseRacing, Name: "Royal Ascot 2026", Region: "UK", StartDate: now.Add(70 * 24 * time.Hour), EndDate: now.Add(75 * 24 * time.Hour), Status: "upcoming", MatchCount: 30},
			{ID: "comp-derby", Sport: models.SportHorseRacing, Name: "Epsom Derby 2026", Region: "UK", StartDate: now.Add(55 * 24 * time.Hour), EndDate: now.Add(56 * 24 * time.Hour), Status: "upcoming", MatchCount: 8},
			{ID: "comp-mumbai-races", Sport: models.SportHorseRacing, Name: "Mumbai Racing Season", Region: "India", StartDate: now.Add(-30 * 24 * time.Hour), EndDate: now.Add(30 * 24 * time.Hour), Status: "active", MatchCount: 48},
		},
		"kabaddi": {
			{ID: "comp-pkl-2026", Sport: models.SportKabaddi, Name: "Pro Kabaddi League Season 11", Region: "India", StartDate: now.Add(-20 * 24 * time.Hour), EndDate: now.Add(40 * 24 * time.Hour), Status: "active", MatchCount: 132},
		},
		"basketball": {
			{ID: "comp-nba-2026", Sport: models.SportBasketball, Name: "NBA 2025-26", Region: "USA", StartDate: now.Add(-180 * 24 * time.Hour), EndDate: now.Add(50 * 24 * time.Hour), Status: "active", MatchCount: 1230},
			{ID: "comp-euroleague", Sport: models.SportBasketball, Name: "EuroLeague 2025-26", Region: "Europe", StartDate: now.Add(-160 * 24 * time.Hour), EndDate: now.Add(40 * 24 * time.Hour), Status: "active", MatchCount: 340},
		},
		"table_tennis": {
			{ID: "comp-wtt-2026", Sport: models.SportTableTennis, Name: "WTT Champions 2026", Region: "International", StartDate: now.Add(-3 * 24 * time.Hour), EndDate: now.Add(4 * 24 * time.Hour), Status: "active", MatchCount: 64},
		},
		"esports": {
			{ID: "comp-dota-ti", Sport: models.SportEsports, Name: "The International 2026", Region: "International", StartDate: now.Add(120 * 24 * time.Hour), EndDate: now.Add(130 * 24 * time.Hour), Status: "upcoming", MatchCount: 96},
			{ID: "comp-csgo-major", Sport: models.SportEsports, Name: "CS2 Major 2026", Region: "International", StartDate: now.Add(-5 * 24 * time.Hour), EndDate: now.Add(10 * 24 * time.Hour), Status: "active", MatchCount: 48},
		},
		"volleyball": {
			{ID: "comp-vnl-2026", Sport: models.SportVolleyball, Name: "Volleyball Nations League 2026", Region: "International", StartDate: now.Add(30 * 24 * time.Hour), EndDate: now.Add(60 * 24 * time.Hour), Status: "upcoming", MatchCount: 120},
		},
		"ice_hockey": {
			{ID: "comp-nhl-2026", Sport: models.SportIceHockey, Name: "NHL 2025-26", Region: "North America", StartDate: now.Add(-180 * 24 * time.Hour), EndDate: now.Add(50 * 24 * time.Hour), Status: "active", MatchCount: 1312},
		},
		"boxing": {
			{ID: "comp-boxing-wbc", Sport: models.SportBoxing, Name: "WBC Championship Fights 2026", Region: "International", StartDate: now.Add(-90 * 24 * time.Hour), EndDate: now.Add(270 * 24 * time.Hour), Status: "active", MatchCount: 24},
		},
		"mma": {
			{ID: "comp-ufc-2026", Sport: models.SportMMA, Name: "UFC 2026 Season", Region: "International", StartDate: now.Add(-90 * 24 * time.Hour), EndDate: now.Add(270 * 24 * time.Hour), Status: "active", MatchCount: 40},
		},
		"badminton": {
			{ID: "comp-bwf-super", Sport: models.SportBadminton, Name: "BWF World Tour Super 1000", Region: "International", StartDate: now.Add(-10 * 24 * time.Hour), EndDate: now.Add(5 * 24 * time.Hour), Status: "active", MatchCount: 64},
		},
		"golf": {
			{ID: "comp-masters-2026", Sport: models.SportGolf, Name: "The Masters 2026", Region: "USA", StartDate: now.Add(-2 * 24 * time.Hour), EndDate: now.Add(2 * 24 * time.Hour), Status: "active", MatchCount: 1},
			{ID: "comp-pga-2026", Sport: models.SportGolf, Name: "PGA Championship 2026", Region: "USA", StartDate: now.Add(40 * 24 * time.Hour), EndDate: now.Add(44 * 24 * time.Hour), Status: "upcoming", MatchCount: 1},
		},
	}

	if sport == "" {
		var result []*models.Competition
		for _, comps := range all {
			result = append(result, comps...)
		}
		return result, nil
	}
	if comps, ok := all[sport]; ok {
		return comps, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// FetchEvents -- returns demo events for a competition
// ---------------------------------------------------------------------------

func (m *MockProvider) FetchEvents(_ context.Context, competitionID string) ([]*models.Event, error) {
	now := time.Now()

	eventsMap := map[string][]*models.Event{
		"comp-ipl-2026": {
			{ID: "evt-ipl-001", CompetitionID: "comp-ipl-2026", Sport: models.SportCricket, Name: "Mumbai Indians v Chennai Super Kings", HomeTeam: "Mumbai Indians", AwayTeam: "Chennai Super Kings", StartTime: now.Add(-30 * time.Minute), Status: "in_play", InPlay: true, Score: "MI 145/3 (16.2)"},
			{ID: "evt-ipl-002", CompetitionID: "comp-ipl-2026", Sport: models.SportCricket, Name: "Royal Challengers v Kolkata Knight Riders", HomeTeam: "Royal Challengers Bengaluru", AwayTeam: "Kolkata Knight Riders", StartTime: now.Add(2 * time.Hour), Status: "upcoming", InPlay: false},
			{ID: "evt-ipl-003", CompetitionID: "comp-ipl-2026", Sport: models.SportCricket, Name: "Delhi Capitals v Rajasthan Royals", HomeTeam: "Delhi Capitals", AwayTeam: "Rajasthan Royals", StartTime: now.Add(26 * time.Hour), Status: "upcoming", InPlay: false},
			{ID: "evt-ipl-004", CompetitionID: "comp-ipl-2026", Sport: models.SportCricket, Name: "Gujarat Titans v Punjab Kings", HomeTeam: "Gujarat Titans", AwayTeam: "Punjab Kings", StartTime: now.Add(50 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-bbl-2026": {
			{ID: "evt-bbl-001", CompetitionID: "comp-bbl-2026", Sport: models.SportCricket, Name: "Sydney Sixers v Melbourne Stars", HomeTeam: "Sydney Sixers", AwayTeam: "Melbourne Stars", StartTime: now.Add(5 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-epl-2026": {
			{ID: "evt-epl-001", CompetitionID: "comp-epl-2026", Sport: models.SportFootball, Name: "Arsenal v Manchester City", HomeTeam: "Arsenal", AwayTeam: "Manchester City", StartTime: now.Add(-45 * time.Minute), Status: "in_play", InPlay: true, Score: "1-1"},
			{ID: "evt-epl-002", CompetitionID: "comp-epl-2026", Sport: models.SportFootball, Name: "Liverpool v Chelsea", HomeTeam: "Liverpool", AwayTeam: "Chelsea", StartTime: now.Add(3 * time.Hour), Status: "upcoming", InPlay: false},
			{ID: "evt-epl-003", CompetitionID: "comp-epl-2026", Sport: models.SportFootball, Name: "Manchester United v Tottenham", HomeTeam: "Manchester United", AwayTeam: "Tottenham Hotspur", StartTime: now.Add(27 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-laliga-2026": {
			{ID: "evt-laliga-001", CompetitionID: "comp-laliga-2026", Sport: models.SportFootball, Name: "Real Madrid v Barcelona", HomeTeam: "Real Madrid", AwayTeam: "Barcelona", StartTime: now.Add(6 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-ucl-2026": {
			{ID: "evt-ucl-001", CompetitionID: "comp-ucl-2026", Sport: models.SportFootball, Name: "Bayern Munich v PSG", HomeTeam: "Bayern Munich", AwayTeam: "Paris Saint-Germain", StartTime: now.Add(28 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-atp-masters": {
			{ID: "evt-ten-001", CompetitionID: "comp-atp-masters", Sport: models.SportTennis, Name: "Djokovic v Alcaraz", HomeTeam: "Novak Djokovic", AwayTeam: "Carlos Alcaraz", StartTime: now.Add(-1 * time.Hour), Status: "in_play", InPlay: true, Score: "6-4 3-5"},
			{ID: "evt-ten-002", CompetitionID: "comp-atp-masters", Sport: models.SportTennis, Name: "Sinner v Medvedev", HomeTeam: "Jannik Sinner", AwayTeam: "Daniil Medvedev", StartTime: now.Add(4 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-mumbai-races": {
			{ID: "evt-hr-001", CompetitionID: "comp-mumbai-races", Sport: models.SportHorseRacing, Name: "Mumbai Race 5 - 1400m Handicap", HomeTeam: "Race 5", AwayTeam: "", StartTime: now.Add(1 * time.Hour), Status: "upcoming", InPlay: false},
			{ID: "evt-hr-002", CompetitionID: "comp-mumbai-races", Sport: models.SportHorseRacing, Name: "Mumbai Race 6 - 1600m Cup", HomeTeam: "Race 6", AwayTeam: "", StartTime: now.Add(2 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-pkl-2026": {
			{ID: "evt-pkl-001", CompetitionID: "comp-pkl-2026", Sport: models.SportKabaddi, Name: "Patna Pirates v Bengal Warriors", HomeTeam: "Patna Pirates", AwayTeam: "Bengal Warriors", StartTime: now.Add(-20 * time.Minute), Status: "in_play", InPlay: true, Score: "28-25"},
			{ID: "evt-pkl-002", CompetitionID: "comp-pkl-2026", Sport: models.SportKabaddi, Name: "Jaipur Pink Panthers v U Mumba", HomeTeam: "Jaipur Pink Panthers", AwayTeam: "U Mumba", StartTime: now.Add(3 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-nba-2026": {
			{ID: "evt-nba-001", CompetitionID: "comp-nba-2026", Sport: models.SportBasketball, Name: "Lakers v Celtics", HomeTeam: "LA Lakers", AwayTeam: "Boston Celtics", StartTime: now.Add(8 * time.Hour), Status: "upcoming", InPlay: false},
			{ID: "evt-nba-002", CompetitionID: "comp-nba-2026", Sport: models.SportBasketball, Name: "Warriors v Bucks", HomeTeam: "Golden State Warriors", AwayTeam: "Milwaukee Bucks", StartTime: now.Add(10 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-wtt-2026": {
			{ID: "evt-tt-001", CompetitionID: "comp-wtt-2026", Sport: models.SportTableTennis, Name: "Ma Long v Fan Zhendong", HomeTeam: "Ma Long", AwayTeam: "Fan Zhendong", StartTime: now.Add(2 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-csgo-major": {
			{ID: "evt-cs-001", CompetitionID: "comp-csgo-major", Sport: models.SportEsports, Name: "Navi v FaZe Clan", HomeTeam: "Natus Vincere", AwayTeam: "FaZe Clan", StartTime: now.Add(-10 * time.Minute), Status: "in_play", InPlay: true, Score: "1-0 (Maps)"},
		},
		"comp-nhl-2026": {
			{ID: "evt-nhl-001", CompetitionID: "comp-nhl-2026", Sport: models.SportIceHockey, Name: "Maple Leafs v Canadiens", HomeTeam: "Toronto Maple Leafs", AwayTeam: "Montreal Canadiens", StartTime: now.Add(9 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-boxing-wbc": {
			{ID: "evt-box-001", CompetitionID: "comp-boxing-wbc", Sport: models.SportBoxing, Name: "Crawford v Spence Jr", HomeTeam: "Terence Crawford", AwayTeam: "Errol Spence Jr", StartTime: now.Add(72 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-ufc-2026": {
			{ID: "evt-ufc-001", CompetitionID: "comp-ufc-2026", Sport: models.SportMMA, Name: "UFC 310: Adesanya v Du Plessis", HomeTeam: "Israel Adesanya", AwayTeam: "Dricus Du Plessis", StartTime: now.Add(48 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-bwf-super": {
			{ID: "evt-bad-001", CompetitionID: "comp-bwf-super", Sport: models.SportBadminton, Name: "Axelsen v Momota", HomeTeam: "Viktor Axelsen", AwayTeam: "Kento Momota", StartTime: now.Add(5 * time.Hour), Status: "upcoming", InPlay: false},
		},
		"comp-masters-2026": {
			{ID: "evt-golf-001", CompetitionID: "comp-masters-2026", Sport: models.SportGolf, Name: "The Masters 2026 - Round 3", HomeTeam: "", AwayTeam: "", StartTime: now.Add(14 * time.Hour), Status: "upcoming", InPlay: false},
		},
	}

	if events, ok := eventsMap[competitionID]; ok {
		return events, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// FetchMarketsByEvent -- returns markets for a specific event
// ---------------------------------------------------------------------------

func (m *MockProvider) FetchMarketsByEvent(_ context.Context, eventID string) ([]*models.Market, error) {
	now := time.Now()

	switch eventID {
	case "evt-ipl-001": // Cricket in-play
		return []*models.Market{
			m.cricketMatchOdds("mkt-ipl-001-mo", eventID, "Mumbai Indians v CSK - Match Odds", now.Add(-30*time.Minute), true),
			m.cricketFancy("mkt-ipl-001-fancy", eventID, "MI Innings Runs - Session", now.Add(-30*time.Minute)),
			m.cricketBookmaker("mkt-ipl-001-bm", eventID, "MI v CSK - Bookmaker", now.Add(-30*time.Minute)),
			{ID: "mkt-ipl-001-toss", EventID: eventID, Sport: models.SportCricket, Name: "Toss Winner", MarketType: models.MarketTypeToss, Status: models.MarketSettled, InPlay: false, StartTime: now.Add(-2 * time.Hour)},
			m.topBatsmanMarket("mkt-ipl-001-tb", eventID, now.Add(-30*time.Minute)),
		}, nil

	case "evt-ipl-002": // Cricket upcoming
		return []*models.Market{
			m.cricketMatchOdds("mkt-ipl-002-mo", eventID, "RCB v KKR - Match Odds", now.Add(2*time.Hour), false),
			{ID: "mkt-ipl-002-toss", EventID: eventID, Sport: models.SportCricket, Name: "Toss Winner", MarketType: models.MarketTypeToss, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(2 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Royal Challengers Bengaluru", Status: "active", BackPrices: []models.PriceSize{{Price: 1.95, Size: 50000}}, LayPrices: []models.PriceSize{{Price: 2.05, Size: 50000}}},
					{SelectionID: 2, Name: "Kolkata Knight Riders", Status: "active", BackPrices: []models.PriceSize{{Price: 1.95, Size: 50000}}, LayPrices: []models.PriceSize{{Price: 2.05, Size: 50000}}},
				}},
		}, nil

	case "evt-epl-001": // Football in-play
		return []*models.Market{
			m.footballMatchOdds("mkt-epl-001-mo", eventID, "Arsenal v Man City - Match Odds", now.Add(-45*time.Minute), true),
			m.overUnderMarket("mkt-epl-001-ou", eventID, "Over/Under 2.5 Goals", now.Add(-45*time.Minute)),
			m.correctScoreMarket("mkt-epl-001-cs", eventID, now.Add(-45*time.Minute)),
			m.bttsMarket("mkt-epl-001-btts", eventID, now.Add(-45*time.Minute)),
			m.handicapMarket("mkt-epl-001-hc", eventID, now.Add(-45*time.Minute)),
		}, nil

	case "evt-epl-002": // Football upcoming
		return []*models.Market{
			m.footballMatchOdds("mkt-epl-002-mo", eventID, "Liverpool v Chelsea - Match Odds", now.Add(3*time.Hour), false),
			m.overUnderMarket("mkt-epl-002-ou", eventID, "Over/Under 2.5 Goals", now.Add(3*time.Hour)),
			m.bttsMarket("mkt-epl-002-btts", eventID, now.Add(3*time.Hour)),
		}, nil

	case "evt-ten-001": // Tennis in-play
		return []*models.Market{
			{ID: "mkt-ten-001-mo", EventID: eventID, Sport: models.SportTennis, Name: "Djokovic v Alcaraz - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-1 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Novak Djokovic", Status: "active", BackPrices: []models.PriceSize{{Price: 1.65, Size: 25000}, {Price: 1.64, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 1.67, Size: 20000}, {Price: 1.68, Size: 12000}}},
					{SelectionID: 2, Name: "Carlos Alcaraz", Status: "active", BackPrices: []models.PriceSize{{Price: 2.30, Size: 20000}, {Price: 2.28, Size: 12000}}, LayPrices: []models.PriceSize{{Price: 2.34, Size: 18000}, {Price: 2.36, Size: 10000}}},
				},
				Score: &models.ScoreContext{MatchID: eventID, Sets: "6-4 3-5", Server: "Alcaraz", Period: "Set 2"},
			},
		}, nil

	case "evt-pkl-001": // Kabaddi in-play
		return []*models.Market{
			{ID: "mkt-pkl-001-mo", EventID: eventID, Sport: models.SportKabaddi, Name: "Patna Pirates v Bengal Warriors - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-20 * time.Minute),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Patna Pirates", Status: "active", BackPrices: []models.PriceSize{{Price: 1.55, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.58, Size: 25000}}},
					{SelectionID: 2, Name: "Bengal Warriors", Status: "active", BackPrices: []models.PriceSize{{Price: 2.50, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 2.56, Size: 20000}}},
				},
				Score: &models.ScoreContext{MatchID: eventID, HomeScore: 28, AwayScore: 25, Period: "2nd Half", Clock: "25:30"},
			},
			{ID: "mkt-pkl-001-bm", EventID: eventID, Sport: models.SportKabaddi, Name: "Patna v Bengal - Bookmaker", MarketType: models.MarketTypeBookmaker, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-20 * time.Minute),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Patna Pirates", Status: "active", BackPrices: []models.PriceSize{{Price: 1.53, Size: 100000}}, LayPrices: []models.PriceSize{{Price: 1.57, Size: 100000}}},
					{SelectionID: 2, Name: "Bengal Warriors", Status: "active", BackPrices: []models.PriceSize{{Price: 2.48, Size: 100000}}, LayPrices: []models.PriceSize{{Price: 2.55, Size: 100000}}},
				},
			},
		}, nil

	case "evt-hr-001": // Horse Racing
		return []*models.Market{
			{ID: "mkt-hr-001-mo", EventID: eventID, Sport: models.SportHorseRacing, Name: "Mumbai Race 5 - Win Market", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(1 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Thunder Bolt", Status: "active", BackPrices: []models.PriceSize{{Price: 3.50, Size: 5000}}, LayPrices: []models.PriceSize{{Price: 3.65, Size: 4000}}},
					{SelectionID: 2, Name: "Silver Arrow", Status: "active", BackPrices: []models.PriceSize{{Price: 4.20, Size: 4000}}, LayPrices: []models.PriceSize{{Price: 4.40, Size: 3500}}},
					{SelectionID: 3, Name: "Golden Star", Status: "active", BackPrices: []models.PriceSize{{Price: 5.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 5.30, Size: 2500}}},
					{SelectionID: 4, Name: "Night Fury", Status: "active", BackPrices: []models.PriceSize{{Price: 7.50, Size: 2000}}, LayPrices: []models.PriceSize{{Price: 8.00, Size: 1500}}},
					{SelectionID: 5, Name: "Royal Duke", Status: "active", BackPrices: []models.PriceSize{{Price: 12.00, Size: 1000}}, LayPrices: []models.PriceSize{{Price: 13.00, Size: 800}}},
				},
			},
		}, nil

	case "evt-nba-001": // Basketball
		return []*models.Market{
			{ID: "mkt-nba-001-mo", EventID: eventID, Sport: models.SportBasketball, Name: "Lakers v Celtics - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(8 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "LA Lakers", Status: "active", BackPrices: []models.PriceSize{{Price: 2.20, Size: 50000}}, LayPrices: []models.PriceSize{{Price: 2.26, Size: 40000}}},
					{SelectionID: 2, Name: "Boston Celtics", Status: "active", BackPrices: []models.PriceSize{{Price: 1.72, Size: 60000}}, LayPrices: []models.PriceSize{{Price: 1.76, Size: 50000}}},
				},
			},
			{ID: "mkt-nba-001-ou", EventID: eventID, Sport: models.SportBasketball, Name: "Total Points Over/Under 220.5", MarketType: models.MarketTypeOverUnder, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(8 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Over 220.5", Status: "active", BackPrices: []models.PriceSize{{Price: 1.90, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.94, Size: 25000}}},
					{SelectionID: 2, Name: "Under 220.5", Status: "active", BackPrices: []models.PriceSize{{Price: 1.90, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.94, Size: 25000}}},
				},
			},
			m.handicapMarket("mkt-nba-001-hc", eventID, now.Add(8*time.Hour)),
		}, nil

	case "evt-cs-001": // Esports
		return []*models.Market{
			{ID: "mkt-cs-001-mo", EventID: eventID, Sport: models.SportEsports, Name: "Navi v FaZe - Match Winner", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-10 * time.Minute),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Natus Vincere", Status: "active", BackPrices: []models.PriceSize{{Price: 1.45, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 1.48, Size: 12000}}},
					{SelectionID: 2, Name: "FaZe Clan", Status: "active", BackPrices: []models.PriceSize{{Price: 2.80, Size: 12000}}, LayPrices: []models.PriceSize{{Price: 2.88, Size: 10000}}},
				},
				Score: &models.ScoreContext{MatchID: eventID, HomeScore: 1, AwayScore: 0, Period: "Map 2"},
			},
		}, nil

	case "evt-box-001": // Boxing
		return []*models.Market{
			{ID: "mkt-box-001-mo", EventID: eventID, Sport: models.SportBoxing, Name: "Crawford v Spence - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(72 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Terence Crawford", Status: "active", BackPrices: []models.PriceSize{{Price: 1.60, Size: 20000}}, LayPrices: []models.PriceSize{{Price: 1.64, Size: 18000}}},
					{SelectionID: 2, Name: "Errol Spence Jr", Status: "active", BackPrices: []models.PriceSize{{Price: 2.40, Size: 18000}}, LayPrices: []models.PriceSize{{Price: 2.46, Size: 15000}}},
					{SelectionID: 3, Name: "Draw", Status: "active", BackPrices: []models.PriceSize{{Price: 21.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 23.00, Size: 2000}}},
				},
			},
		}, nil

	case "evt-ufc-001": // MMA
		return []*models.Market{
			{ID: "mkt-ufc-001-mo", EventID: eventID, Sport: models.SportMMA, Name: "Adesanya v Du Plessis - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(48 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Israel Adesanya", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 2.16, Size: 12000}}},
					{SelectionID: 2, Name: "Dricus Du Plessis", Status: "active", BackPrices: []models.PriceSize{{Price: 1.78, Size: 18000}}, LayPrices: []models.PriceSize{{Price: 1.82, Size: 15000}}},
				},
			},
		}, nil

	case "evt-golf-001": // Golf outright
		return []*models.Market{
			{ID: "mkt-golf-001-out", EventID: eventID, Sport: models.SportGolf, Name: "The Masters 2026 - Outright Winner", MarketType: models.MarketTypeOutright, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(14 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Scottie Scheffler", Status: "active", BackPrices: []models.PriceSize{{Price: 4.50, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 4.80, Size: 8000}}},
					{SelectionID: 2, Name: "Rory McIlroy", Status: "active", BackPrices: []models.PriceSize{{Price: 8.00, Size: 8000}}, LayPrices: []models.PriceSize{{Price: 8.50, Size: 6000}}},
					{SelectionID: 3, Name: "Jon Rahm", Status: "active", BackPrices: []models.PriceSize{{Price: 9.00, Size: 7000}}, LayPrices: []models.PriceSize{{Price: 9.50, Size: 5500}}},
					{SelectionID: 4, Name: "Brooks Koepka", Status: "active", BackPrices: []models.PriceSize{{Price: 12.00, Size: 5000}}, LayPrices: []models.PriceSize{{Price: 13.00, Size: 4000}}},
					{SelectionID: 5, Name: "Xander Schauffele", Status: "active", BackPrices: []models.PriceSize{{Price: 11.00, Size: 5500}}, LayPrices: []models.PriceSize{{Price: 12.00, Size: 4500}}},
				},
			},
		}, nil
	}

	// Default: return a generic match_odds market for any unknown event
	return []*models.Market{
		{ID: fmt.Sprintf("mkt-%s-mo", eventID), EventID: eventID, Sport: models.SportCricket, Name: "Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(1 * time.Hour),
			Runners: []models.Runner{
				{SelectionID: 1, Name: "Home", Status: "active", BackPrices: []models.PriceSize{{Price: 1.85, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 1.88, Size: 8000}}},
				{SelectionID: 2, Name: "Away", Status: "active", BackPrices: []models.PriceSize{{Price: 2.05, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 2.10, Size: 8000}}},
			},
		},
	}, nil
}

// ---------------------------------------------------------------------------
// FetchMarkets -- legacy sport-level fetch (returns flagship markets)
// ---------------------------------------------------------------------------

func (m *MockProvider) FetchMarkets(_ context.Context, sport string) ([]*models.Market, error) {
	now := time.Now()

	sportMarkets := map[string][]*models.Market{
		"cricket": {
			m.cricketMatchOdds("mock-ipl-match-001", "mock-ipl-event-001", "Mumbai Indians v Chennai Super Kings - Match Odds", now.Add(-30*time.Minute), true),
			m.cricketFancy("mock-ipl-fancy-001", "mock-ipl-event-001", "Mumbai Indians Innings Runs - Session", now.Add(-30*time.Minute)),
			m.cricketMatchOdds("mock-ipl-match-002", "mock-ipl-event-002", "Royal Challengers v Kolkata Knight Riders - Match Odds", now.Add(2*time.Hour), false),
		},
		"football": {
			m.footballMatchOdds("mock-epl-match-001", "mock-epl-event-001", "Arsenal v Manchester City - Match Odds", now.Add(-45*time.Minute), true),
			m.overUnderMarket("mock-epl-ou-001", "mock-epl-event-001", "Over/Under 2.5 Goals", now.Add(-45*time.Minute)),
			m.footballMatchOdds("mock-epl-match-002", "mock-epl-event-002", "Liverpool v Chelsea - Match Odds", now.Add(3*time.Hour), false),
		},
		"tennis": {
			{ID: "mock-ten-match-001", EventID: "mock-ten-event-001", Sport: models.SportTennis, Name: "Djokovic v Alcaraz - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-1 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Novak Djokovic", Status: "active", BackPrices: []models.PriceSize{{Price: 1.65, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 1.67, Size: 20000}}},
					{SelectionID: 2, Name: "Carlos Alcaraz", Status: "active", BackPrices: []models.PriceSize{{Price: 2.30, Size: 20000}}, LayPrices: []models.PriceSize{{Price: 2.34, Size: 18000}}},
				}},
		},
		"horse_racing": {
			{ID: "mock-hr-match-001", EventID: "mock-hr-event-001", Sport: models.SportHorseRacing, Name: "Mumbai Race 5 - Win Market", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(1 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Thunder Bolt", Status: "active", BackPrices: []models.PriceSize{{Price: 3.50, Size: 5000}}, LayPrices: []models.PriceSize{{Price: 3.65, Size: 4000}}},
					{SelectionID: 2, Name: "Silver Arrow", Status: "active", BackPrices: []models.PriceSize{{Price: 4.20, Size: 4000}}, LayPrices: []models.PriceSize{{Price: 4.40, Size: 3500}}},
					{SelectionID: 3, Name: "Golden Star", Status: "active", BackPrices: []models.PriceSize{{Price: 5.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 5.30, Size: 2500}}},
				}},
		},
		"kabaddi": {
			{ID: "mock-pkl-match-001", EventID: "mock-pkl-event-001", Sport: models.SportKabaddi, Name: "Patna Pirates v Bengal Warriors - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-20 * time.Minute),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Patna Pirates", Status: "active", BackPrices: []models.PriceSize{{Price: 1.55, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.58, Size: 25000}}},
					{SelectionID: 2, Name: "Bengal Warriors", Status: "active", BackPrices: []models.PriceSize{{Price: 2.50, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 2.56, Size: 20000}}},
				}},
		},
		"basketball": {
			{ID: "mock-nba-match-001", EventID: "mock-nba-event-001", Sport: models.SportBasketball, Name: "Lakers v Celtics - Match Odds", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(8 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "LA Lakers", Status: "active", BackPrices: []models.PriceSize{{Price: 2.20, Size: 50000}}, LayPrices: []models.PriceSize{{Price: 2.26, Size: 40000}}},
					{SelectionID: 2, Name: "Boston Celtics", Status: "active", BackPrices: []models.PriceSize{{Price: 1.72, Size: 60000}}, LayPrices: []models.PriceSize{{Price: 1.76, Size: 50000}}},
				}},
		},
		"table_tennis": {
			{ID: "mock-tt-match-001", EventID: "mock-tt-event-001", Sport: models.SportTableTennis, Name: "Ma Long v Fan Zhendong", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(2 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Ma Long", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 2.14, Size: 8000}}},
					{SelectionID: 2, Name: "Fan Zhendong", Status: "active", BackPrices: []models.PriceSize{{Price: 1.78, Size: 12000}}, LayPrices: []models.PriceSize{{Price: 1.82, Size: 10000}}},
				}},
		},
		"esports": {
			{ID: "mock-cs-match-001", EventID: "mock-cs-event-001", Sport: models.SportEsports, Name: "Navi v FaZe Clan - Match Winner", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: true, StartTime: now.Add(-10 * time.Minute),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Natus Vincere", Status: "active", BackPrices: []models.PriceSize{{Price: 1.45, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 1.48, Size: 12000}}},
					{SelectionID: 2, Name: "FaZe Clan", Status: "active", BackPrices: []models.PriceSize{{Price: 2.80, Size: 12000}}, LayPrices: []models.PriceSize{{Price: 2.88, Size: 10000}}},
				}},
		},
		"volleyball": {
			{ID: "mock-vb-match-001", EventID: "mock-vb-event-001", Sport: models.SportVolleyball, Name: "Brazil v Poland", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(30 * 24 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Brazil", Status: "active", BackPrices: []models.PriceSize{{Price: 1.85, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 1.89, Size: 12000}}},
					{SelectionID: 2, Name: "Poland", Status: "active", BackPrices: []models.PriceSize{{Price: 2.00, Size: 14000}}, LayPrices: []models.PriceSize{{Price: 2.05, Size: 11000}}},
				}},
		},
		"ice_hockey": {
			{ID: "mock-nhl-match-001", EventID: "mock-nhl-event-001", Sport: models.SportIceHockey, Name: "Maple Leafs v Canadiens", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(9 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Toronto Maple Leafs", Status: "active", BackPrices: []models.PriceSize{{Price: 1.70, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 1.74, Size: 20000}}},
					{SelectionID: 2, Name: "Montreal Canadiens", Status: "active", BackPrices: []models.PriceSize{{Price: 2.20, Size: 20000}}, LayPrices: []models.PriceSize{{Price: 2.26, Size: 18000}}},
				}},
		},
		"boxing": {
			{ID: "mock-box-match-001", EventID: "mock-box-event-001", Sport: models.SportBoxing, Name: "Crawford v Spence Jr", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(72 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Terence Crawford", Status: "active", BackPrices: []models.PriceSize{{Price: 1.60, Size: 20000}}, LayPrices: []models.PriceSize{{Price: 1.64, Size: 18000}}},
					{SelectionID: 2, Name: "Errol Spence Jr", Status: "active", BackPrices: []models.PriceSize{{Price: 2.40, Size: 18000}}, LayPrices: []models.PriceSize{{Price: 2.46, Size: 15000}}},
					{SelectionID: 3, Name: "Draw", Status: "active", BackPrices: []models.PriceSize{{Price: 21.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 23.00, Size: 2000}}},
				}},
		},
		"mma": {
			{ID: "mock-ufc-match-001", EventID: "mock-ufc-event-001", Sport: models.SportMMA, Name: "Adesanya v Du Plessis", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(48 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Israel Adesanya", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 2.16, Size: 12000}}},
					{SelectionID: 2, Name: "Dricus Du Plessis", Status: "active", BackPrices: []models.PriceSize{{Price: 1.78, Size: 18000}}, LayPrices: []models.PriceSize{{Price: 1.82, Size: 15000}}},
				}},
		},
		"badminton": {
			{ID: "mock-bad-match-001", EventID: "mock-bad-event-001", Sport: models.SportBadminton, Name: "Axelsen v Momota", MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(5 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Viktor Axelsen", Status: "active", BackPrices: []models.PriceSize{{Price: 1.40, Size: 12000}}, LayPrices: []models.PriceSize{{Price: 1.43, Size: 10000}}},
					{SelectionID: 2, Name: "Kento Momota", Status: "active", BackPrices: []models.PriceSize{{Price: 3.00, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 3.10, Size: 8000}}},
				}},
		},
		"golf": {
			{ID: "mock-golf-outright-001", EventID: "mock-golf-event-001", Sport: models.SportGolf, Name: "The Masters 2026 - Outright Winner", MarketType: models.MarketTypeOutright, Status: models.MarketOpen, InPlay: false, StartTime: now.Add(14 * time.Hour),
				Runners: []models.Runner{
					{SelectionID: 1, Name: "Scottie Scheffler", Status: "active", BackPrices: []models.PriceSize{{Price: 4.50, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 4.80, Size: 8000}}},
					{SelectionID: 2, Name: "Rory McIlroy", Status: "active", BackPrices: []models.PriceSize{{Price: 8.00, Size: 8000}}, LayPrices: []models.PriceSize{{Price: 8.50, Size: 6000}}},
					{SelectionID: 3, Name: "Jon Rahm", Status: "active", BackPrices: []models.PriceSize{{Price: 9.00, Size: 7000}}, LayPrices: []models.PriceSize{{Price: 9.50, Size: 5500}}},
				}},
		},
	}

	if sport == "" {
		var all []*models.Market
		for _, mkts := range sportMarkets {
			all = append(all, mkts...)
		}
		return all, nil
	}
	if mkts, ok := sportMarkets[sport]; ok {
		return mkts, nil
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Subscribe -- streams live odds updates
// ---------------------------------------------------------------------------

func (m *MockProvider) Subscribe(ctx context.Context, marketIDs []string, updates chan<- *models.OddsUpdate) error {
	ticker := time.NewTicker(m.updateInterval)
	defer ticker.Stop()

	type runnerState struct {
		basePrice float64
		score     int
		overs     float64
		wickets   int
	}
	states := make(map[string]map[int64]*runnerState)

	for _, mid := range marketIDs {
		states[mid] = map[int64]*runnerState{
			1:  {basePrice: 1.85, score: 45, overs: 6.2, wickets: 1},
			2:  {basePrice: 2.10, score: 0, overs: 0, wickets: 0},
			3:  {basePrice: 15.0, score: 0, overs: 0, wickets: 0},
			4:  {basePrice: 1.95, score: 0, overs: 0, wickets: 0},
			5:  {basePrice: 1.90, score: 0, overs: 0, wickets: 0},
			6:  {basePrice: 20.0, score: 0, overs: 0, wickets: 0},
			10: {basePrice: 1.80, score: 45, overs: 6.2, wickets: 1},
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			for _, mid := range marketIDs {
				rs, ok := states[mid]
				if !ok {
					continue
				}

				var runners []models.Runner
				for selID, state := range rs {
					drift := 0.0
					diffusion := m.volatility * m.lockedNormFloat64()
					state.basePrice *= math.Exp(drift + diffusion)
					state.basePrice = math.Max(1.01, math.Min(1000.0, state.basePrice))

					if state.score > 0 || selID <= 2 {
						lambda := 1.5
						runs := m.poissonSample(lambda)
						state.score += runs

						if m.lockedFloat64() < 0.02 {
							state.wickets++
							state.basePrice *= 1.0 + m.volatility*3
						}

						state.overs += 0.1
						if math.Mod(state.overs*10, 10) >= 6 {
							state.overs = math.Floor(state.overs) + 1.0
						}
					}

					spread := state.basePrice * 0.02
					backPrice := math.Round((state.basePrice-spread)*100) / 100
					layPrice := math.Round((state.basePrice+spread)*100) / 100

					runners = append(runners, models.Runner{
						SelectionID: selID,
						Status:      "active",
						BackPrices: []models.PriceSize{
							{Price: backPrice, Size: m.randomSize()},
							{Price: math.Round((backPrice-0.02)*100) / 100, Size: m.randomSize()},
							{Price: math.Round((backPrice-0.04)*100) / 100, Size: m.randomSize()},
						},
						LayPrices: []models.PriceSize{
							{Price: layPrice, Size: m.randomSize()},
							{Price: math.Round((layPrice+0.02)*100) / 100, Size: m.randomSize()},
							{Price: math.Round((layPrice+0.04)*100) / 100, Size: m.randomSize()},
						},
						LastPrice: state.basePrice,
					})
				}

				var score *models.ScoreContext
				if rs[1] != nil && rs[1].score > 0 {
					score = &models.ScoreContext{
						MatchID:     mid,
						Score:       fmt.Sprintf("%d/%d", rs[1].score, rs[1].wickets),
						Overs:       fmt.Sprintf("%.1f", rs[1].overs),
						Wickets:     rs[1].wickets,
						RunRate:     float64(rs[1].score) / math.Max(rs[1].overs, 0.1),
						LastBall:    m.randomBallOutcome(),
						Innings:     1,
						BattingTeam: "Mumbai Indians",
					}
				}

				update := &models.OddsUpdate{
					MarketID:  mid,
					Runners:   runners,
					Score:     score,
					Timestamp: time.Now(),
				}

				select {
				case updates <- update:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
		}
	}
}

func (m *MockProvider) HealthCheck(_ context.Context) error {
	return nil
}

func (m *MockProvider) Close() error {
	return nil
}

// ---------------------------------------------------------------------------
// Market builder helpers
// ---------------------------------------------------------------------------

func (m *MockProvider) cricketMatchOdds(id, eventID, name string, startTime time.Time, inPlay bool) *models.Market {
	status := models.MarketOpen
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportCricket, Name: name,
		MarketType: models.MarketTypeMatchOdds, Status: status, InPlay: inPlay, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Home", Status: "active", BackPrices: []models.PriceSize{{Price: 1.85, Size: 50000}, {Price: 1.84, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.87, Size: 45000}, {Price: 1.88, Size: 25000}}},
			{SelectionID: 2, Name: "Away", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 40000}, {Price: 2.08, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 2.12, Size: 35000}, {Price: 2.14, Size: 20000}}},
			{SelectionID: 3, Name: "The Draw", Status: "active", BackPrices: []models.PriceSize{{Price: 15.00, Size: 5000}}, LayPrices: []models.PriceSize{{Price: 16.00, Size: 4000}}},
		},
	}
}

func (m *MockProvider) cricketFancy(id, eventID, name string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportCricket, Name: name,
		MarketType: models.MarketTypeFancy, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 10, Name: "Over 150.5 Runs", Status: "active",
				BackPrices: []models.PriceSize{{Price: 1.80, Size: 100000}},
				LayPrices:  []models.PriceSize{{Price: 1.82, Size: 100000}}},
		},
	}
}

func (m *MockProvider) cricketBookmaker(id, eventID, name string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportCricket, Name: name,
		MarketType: models.MarketTypeBookmaker, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Home", Status: "active", BackPrices: []models.PriceSize{{Price: 1.83, Size: 200000}}, LayPrices: []models.PriceSize{{Price: 1.87, Size: 200000}}},
			{SelectionID: 2, Name: "Away", Status: "active", BackPrices: []models.PriceSize{{Price: 2.08, Size: 200000}}, LayPrices: []models.PriceSize{{Price: 2.12, Size: 200000}}},
		},
	}
}

func (m *MockProvider) topBatsmanMarket(id, eventID string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportCricket, Name: "Top Batsman",
		MarketType: models.MarketTypeTopBatsman, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 101, Name: "Rohit Sharma", Status: "active", BackPrices: []models.PriceSize{{Price: 3.50, Size: 10000}}, LayPrices: []models.PriceSize{{Price: 3.70, Size: 8000}}},
			{SelectionID: 102, Name: "Suryakumar Yadav", Status: "active", BackPrices: []models.PriceSize{{Price: 4.00, Size: 8000}}, LayPrices: []models.PriceSize{{Price: 4.20, Size: 6000}}},
			{SelectionID: 103, Name: "Ishan Kishan", Status: "active", BackPrices: []models.PriceSize{{Price: 5.50, Size: 6000}}, LayPrices: []models.PriceSize{{Price: 5.80, Size: 5000}}},
		},
	}
}

func (m *MockProvider) footballMatchOdds(id, eventID, name string, startTime time.Time, inPlay bool) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportFootball, Name: name,
		MarketType: models.MarketTypeMatchOdds, Status: models.MarketOpen, InPlay: inPlay, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Home", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 60000}, {Price: 2.08, Size: 40000}}, LayPrices: []models.PriceSize{{Price: 2.14, Size: 55000}, {Price: 2.16, Size: 35000}}},
			{SelectionID: 2, Name: "Draw", Status: "active", BackPrices: []models.PriceSize{{Price: 3.40, Size: 25000}, {Price: 3.35, Size: 15000}}, LayPrices: []models.PriceSize{{Price: 3.50, Size: 22000}, {Price: 3.55, Size: 12000}}},
			{SelectionID: 3, Name: "Away", Status: "active", BackPrices: []models.PriceSize{{Price: 3.60, Size: 30000}, {Price: 3.55, Size: 20000}}, LayPrices: []models.PriceSize{{Price: 3.70, Size: 28000}, {Price: 3.75, Size: 18000}}},
		},
	}
}

func (m *MockProvider) overUnderMarket(id, eventID, name string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportFootball, Name: name,
		MarketType: models.MarketTypeOverUnder, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Over 2.5", Status: "active", BackPrices: []models.PriceSize{{Price: 1.85, Size: 40000}}, LayPrices: []models.PriceSize{{Price: 1.88, Size: 35000}}},
			{SelectionID: 2, Name: "Under 2.5", Status: "active", BackPrices: []models.PriceSize{{Price: 2.00, Size: 35000}}, LayPrices: []models.PriceSize{{Price: 2.04, Size: 30000}}},
		},
	}
}

func (m *MockProvider) correctScoreMarket(id, eventID string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportFootball, Name: "Correct Score",
		MarketType: models.MarketTypeCorrectScore, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "1-0", Status: "active", BackPrices: []models.PriceSize{{Price: 7.00, Size: 5000}}, LayPrices: []models.PriceSize{{Price: 7.50, Size: 4000}}},
			{SelectionID: 2, Name: "1-1", Status: "active", BackPrices: []models.PriceSize{{Price: 5.50, Size: 6000}}, LayPrices: []models.PriceSize{{Price: 5.80, Size: 5000}}},
			{SelectionID: 3, Name: "2-1", Status: "active", BackPrices: []models.PriceSize{{Price: 8.50, Size: 4000}}, LayPrices: []models.PriceSize{{Price: 9.00, Size: 3500}}},
			{SelectionID: 4, Name: "0-1", Status: "active", BackPrices: []models.PriceSize{{Price: 9.00, Size: 4000}}, LayPrices: []models.PriceSize{{Price: 9.50, Size: 3000}}},
			{SelectionID: 5, Name: "2-0", Status: "active", BackPrices: []models.PriceSize{{Price: 10.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 11.00, Size: 2500}}},
			{SelectionID: 6, Name: "0-0", Status: "active", BackPrices: []models.PriceSize{{Price: 11.00, Size: 3000}}, LayPrices: []models.PriceSize{{Price: 12.00, Size: 2500}}},
		},
	}
}

func (m *MockProvider) bttsMarket(id, eventID string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportFootball, Name: "Both Teams to Score",
		MarketType: models.MarketTypeBothTeamsToScore, Status: models.MarketOpen, InPlay: true, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Yes", Status: "active", BackPrices: []models.PriceSize{{Price: 1.75, Size: 30000}}, LayPrices: []models.PriceSize{{Price: 1.78, Size: 25000}}},
			{SelectionID: 2, Name: "No", Status: "active", BackPrices: []models.PriceSize{{Price: 2.10, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 2.14, Size: 20000}}},
		},
	}
}

func (m *MockProvider) handicapMarket(id, eventID string, startTime time.Time) *models.Market {
	return &models.Market{
		ID: id, EventID: eventID, Sport: models.SportFootball, Name: "Asian Handicap -0.5",
		MarketType: models.MarketTypeHandicap, Status: models.MarketOpen, InPlay: false, StartTime: startTime,
		Runners: []models.Runner{
			{SelectionID: 1, Name: "Home -0.5", Status: "active", BackPrices: []models.PriceSize{{Price: 2.05, Size: 25000}}, LayPrices: []models.PriceSize{{Price: 2.10, Size: 20000}}},
			{SelectionID: 2, Name: "Away +0.5", Status: "active", BackPrices: []models.PriceSize{{Price: 1.82, Size: 28000}}, LayPrices: []models.PriceSize{{Price: 1.86, Size: 22000}}},
		},
	}
}

// ---------------------------------------------------------------------------
// RNG helpers
// ---------------------------------------------------------------------------

// lockedNormFloat64 returns a normally-distributed random value, safe for concurrent use.
func (m *MockProvider) lockedNormFloat64() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rng.NormFloat64()
}

// lockedFloat64 returns a uniform [0,1) random value, safe for concurrent use.
func (m *MockProvider) lockedFloat64() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rng.Float64()
}

// lockedIntn returns a uniform random int in [0,n), safe for concurrent use.
func (m *MockProvider) lockedIntn(n int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rng.Intn(n)
}

func (m *MockProvider) poissonSample(lambda float64) int {
	L := math.Exp(-lambda)
	k := 0
	p := 1.0
	for {
		k++
		p *= m.lockedFloat64()
		if p <= L {
			break
		}
	}
	return k - 1
}

func (m *MockProvider) randomSize() float64 {
	sizes := []float64{100, 250, 500, 1000, 2500, 5000, 10000}
	return sizes[m.lockedIntn(len(sizes))]
}

func (m *MockProvider) randomBallOutcome() string {
	outcomes := []string{"0", "1", "1", "2", "4", "6", "W", "1", "0", "1", "2", "1"}
	return outcomes[m.lockedIntn(len(outcomes))]
}
