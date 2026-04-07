package main

import (
	"fmt"
	"math"
	"math/rand"
	"time"
)

func (s *Store) seedData() {
	now := time.Now()

	// ── Sports ──────────────────────────────────────────────────
	s.sports = []*Sport{
		{ID: "cricket", Name: "Cricket", Slug: "cricket", Active: true, SortOrder: 1},
		{ID: "football", Name: "Football", Slug: "football", Active: true, SortOrder: 2},
		{ID: "tennis", Name: "Tennis", Slug: "tennis", Active: true, SortOrder: 3},
		{ID: "basketball", Name: "Basketball", Slug: "basketball", Active: true, SortOrder: 4},
		{ID: "ice_hockey", Name: "Ice Hockey", Slug: "ice-hockey", Active: true, SortOrder: 5},
		{ID: "baseball", Name: "Baseball", Slug: "baseball", Active: true, SortOrder: 6},
		{ID: "american_football", Name: "American Football", Slug: "american-football", Active: true, SortOrder: 7},
		{ID: "boxing", Name: "Boxing", Slug: "boxing", Active: true, SortOrder: 8},
		{ID: "mma", Name: "MMA", Slug: "mma", Active: true, SortOrder: 9},
		{ID: "rugby", Name: "Rugby", Slug: "rugby", Active: true, SortOrder: 10},
		{ID: "golf", Name: "Golf", Slug: "golf", Active: true, SortOrder: 11},
		{ID: "kabaddi", Name: "Kabaddi", Slug: "kabaddi", Active: true, SortOrder: 12},
		{ID: "horse_racing", Name: "Horse Racing", Slug: "horse-racing", Active: true, SortOrder: 13},
		{ID: "table_tennis", Name: "Table Tennis", Slug: "table-tennis", Active: true, SortOrder: 14},
		{ID: "volleyball", Name: "Volleyball", Slug: "volleyball", Active: true, SortOrder: 15},
		{ID: "handball", Name: "Handball", Slug: "handball", Active: true, SortOrder: 16},
		{ID: "aussie_rules", Name: "Aussie Rules", Slug: "aussie-rules", Active: true, SortOrder: 17},
		{ID: "esports", Name: "Esports", Slug: "esports", Active: true, SortOrder: 18},
	}

	// ── Competitions ────────────────────────────────────────────
	s.competitions = []*Competition{
		{ID: "ipl-2026", SportID: "cricket", Name: "Indian Premier League 2026", Region: "India", Status: "live", MatchCount: 74},
		{ID: "psl-2026", SportID: "cricket", Name: "Pakistan Super League 2026", Region: "Pakistan", Status: "upcoming", MatchCount: 34},
		{ID: "epl-2026", SportID: "football", Name: "English Premier League 2025-26", Region: "England", Status: "live", MatchCount: 380},
		{ID: "ucl-2026", SportID: "football", Name: "UEFA Champions League 2025-26", Region: "Europe", Status: "live", MatchCount: 125},
		{ID: "atp-madrid", SportID: "tennis", Name: "ATP Madrid Open 2026", Region: "Spain", Status: "live", MatchCount: 56},
		{ID: "pkl-2026", SportID: "kabaddi", Name: "Pro Kabaddi League 2026", Region: "India", Status: "upcoming", MatchCount: 132},
		{ID: "nba-2026", SportID: "basketball", Name: "NBA 2025-26", Region: "USA", Status: "live", MatchCount: 82},
		{ID: "boxing-2026", SportID: "boxing", Name: "Boxing Major Bouts 2026", Region: "International", Status: "upcoming", MatchCount: 12},
		{ID: "mma-2026", SportID: "mma", Name: "UFC 2026", Region: "International", Status: "upcoming", MatchCount: 24},
	}

	// ── Events ──────────────────────────────────────────────────
	s.events = []*Event{
		{ID: "ipl-mi-csk", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Mumbai Indians v Chennai Super Kings", HomeTeam: "Mumbai Indians", AwayTeam: "Chennai Super Kings", StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), Status: "in_play", InPlay: true},
		{ID: "ipl-rcb-kkr", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Royal Challengers v Kolkata Knight Riders", HomeTeam: "Royal Challengers Bengaluru", AwayTeam: "Kolkata Knight Riders", StartTime: now.Add(3 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-dc-srh", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Delhi Capitals v Sunrisers Hyderabad", HomeTeam: "Delhi Capitals", AwayTeam: "Sunrisers Hyderabad", StartTime: now.Add(27 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-ars-che", CompetitionID: "epl-2026", SportID: "football", Name: "Arsenal v Chelsea", HomeTeam: "Arsenal", AwayTeam: "Chelsea", StartTime: now.Add(-30 * time.Minute).Format(time.RFC3339), Status: "in_play", InPlay: true},
		{ID: "epl-mun-liv", CompetitionID: "epl-2026", SportID: "football", Name: "Manchester United v Liverpool", HomeTeam: "Manchester United", AwayTeam: "Liverpool", StartTime: now.Add(5 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "atp-djok-alc", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Djokovic v Alcaraz", HomeTeam: "Novak Djokovic", AwayTeam: "Carlos Alcaraz", StartTime: now.Add(-1 * time.Hour).Format(time.RFC3339), Status: "in_play", InPlay: true},

		// ── IPL Additional ──
		{ID: "ipl-gt-lsg", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Gujarat Titans v Lucknow Super Giants", HomeTeam: "Gujarat Titans", AwayTeam: "Lucknow Super Giants", StartTime: now.Add(3 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-pbks-rr", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Punjab Kings v Rajasthan Royals", HomeTeam: "Punjab Kings", AwayTeam: "Rajasthan Royals", StartTime: now.Add(7 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-srh-dc", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Sunrisers Hyderabad v Delhi Capitals", HomeTeam: "Sunrisers Hyderabad", AwayTeam: "Delhi Capitals", StartTime: now.Add(27 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-csk-kkr", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Chennai Super Kings v Kolkata Knight Riders", HomeTeam: "Chennai Super Kings", AwayTeam: "Kolkata Knight Riders", StartTime: now.Add(31 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-mi-rcb", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Mumbai Indians v Royal Challengers Bengaluru", HomeTeam: "Mumbai Indians", AwayTeam: "Royal Challengers Bengaluru", StartTime: now.Add(51 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-dc-gt", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Delhi Capitals v Gujarat Titans", HomeTeam: "Delhi Capitals", AwayTeam: "Gujarat Titans", StartTime: now.Add(55 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-kkr-srh", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Kolkata Knight Riders v Sunrisers Hyderabad", HomeTeam: "Kolkata Knight Riders", AwayTeam: "Sunrisers Hyderabad", StartTime: now.Add(75 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ipl-rr-csk", CompetitionID: "ipl-2026", SportID: "cricket", Name: "Rajasthan Royals v Chennai Super Kings", HomeTeam: "Rajasthan Royals", AwayTeam: "Chennai Super Kings", StartTime: now.Add(79 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── PSL ──
		{ID: "psl-lq-iu", CompetitionID: "psl-2026", SportID: "cricket", Name: "Lahore Qalandars v Islamabad United", HomeTeam: "Lahore Qalandars", AwayTeam: "Islamabad United", StartTime: now.Add(5 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "psl-kk-pz", CompetitionID: "psl-2026", SportID: "cricket", Name: "Karachi Kings v Peshawar Zalmi", HomeTeam: "Karachi Kings", AwayTeam: "Peshawar Zalmi", StartTime: now.Add(29 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "psl-ms-qg", CompetitionID: "psl-2026", SportID: "cricket", Name: "Multan Sultans v Quetta Gladiators", HomeTeam: "Multan Sultans", AwayTeam: "Quetta Gladiators", StartTime: now.Add(53 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── EPL Additional ──
		{ID: "epl-tot-new", CompetitionID: "epl-2026", SportID: "football", Name: "Tottenham v Newcastle", HomeTeam: "Tottenham Hotspur", AwayTeam: "Newcastle United", StartTime: now.Add(4 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-bri-whu", CompetitionID: "epl-2026", SportID: "football", Name: "Brighton v West Ham", HomeTeam: "Brighton & Hove Albion", AwayTeam: "West Ham United", StartTime: now.Add(6 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-avl-wol", CompetitionID: "epl-2026", SportID: "football", Name: "Aston Villa v Wolves", HomeTeam: "Aston Villa", AwayTeam: "Wolverhampton Wanderers", StartTime: now.Add(28 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-lei-bre", CompetitionID: "epl-2026", SportID: "football", Name: "Leicester v Brentford", HomeTeam: "Leicester City", AwayTeam: "Brentford", StartTime: now.Add(30 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-cry-bou", CompetitionID: "epl-2026", SportID: "football", Name: "Crystal Palace v Bournemouth", HomeTeam: "Crystal Palace", AwayTeam: "AFC Bournemouth", StartTime: now.Add(52 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-ful-nfo", CompetitionID: "epl-2026", SportID: "football", Name: "Fulham v Nottingham Forest", HomeTeam: "Fulham", AwayTeam: "Nottingham Forest", StartTime: now.Add(54 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-eve-sou", CompetitionID: "epl-2026", SportID: "football", Name: "Everton v Southampton", HomeTeam: "Everton", AwayTeam: "Southampton", StartTime: now.Add(76 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "epl-lee-bur", CompetitionID: "epl-2026", SportID: "football", Name: "Leeds v Burnley", HomeTeam: "Leeds United", AwayTeam: "Burnley", StartTime: now.Add(78 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── UCL ──
		{ID: "ucl-rma-bay", CompetitionID: "ucl-2026", SportID: "football", Name: "Real Madrid v Bayern Munich", HomeTeam: "Real Madrid", AwayTeam: "Bayern Munich", StartTime: now.Add(48 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ucl-bar-psg", CompetitionID: "ucl-2026", SportID: "football", Name: "Barcelona v PSG", HomeTeam: "FC Barcelona", AwayTeam: "Paris Saint-Germain", StartTime: now.Add(48 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ucl-int-dor", CompetitionID: "ucl-2026", SportID: "football", Name: "Inter Milan v Dortmund", HomeTeam: "Inter Milan", AwayTeam: "Borussia Dortmund", StartTime: now.Add(72 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "ucl-atm-ars", CompetitionID: "ucl-2026", SportID: "football", Name: "Atletico Madrid v Arsenal", HomeTeam: "Atletico Madrid", AwayTeam: "Arsenal", StartTime: now.Add(72 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── ATP Madrid Additional ──
		{ID: "atp-nad-tsi", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Nadal v Tsitsipas", HomeTeam: "Rafael Nadal", AwayTeam: "Stefanos Tsitsipas", StartTime: now.Add(-40 * time.Minute).Format(time.RFC3339), Status: "in_play", InPlay: true},
		{ID: "atp-sin-med", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Sinner v Medvedev", HomeTeam: "Jannik Sinner", AwayTeam: "Daniil Medvedev", StartTime: now.Add(2 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "atp-run-zve", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Rune v Zverev", HomeTeam: "Holger Rune", AwayTeam: "Alexander Zverev", StartTime: now.Add(4 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "atp-fri-ruu", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Fritz v Ruud", HomeTeam: "Taylor Fritz", AwayTeam: "Casper Ruud", StartTime: now.Add(26 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "atp-rub-dim", CompetitionID: "atp-madrid", SportID: "tennis", Name: "Rublev v Dimitrov", HomeTeam: "Andrey Rublev", AwayTeam: "Grigor Dimitrov", StartTime: now.Add(28 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "atp-dem-dra", CompetitionID: "atp-madrid", SportID: "tennis", Name: "De Minaur v Draper", HomeTeam: "Alex de Minaur", AwayTeam: "Jack Draper", StartTime: now.Add(50 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── NBA ──
		{ID: "nba-lal-bos", CompetitionID: "nba-2026", SportID: "basketball", Name: "Lakers v Celtics", HomeTeam: "Los Angeles Lakers", AwayTeam: "Boston Celtics", StartTime: now.Add(8 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "nba-gsw-mil", CompetitionID: "nba-2026", SportID: "basketball", Name: "Warriors v Bucks", HomeTeam: "Golden State Warriors", AwayTeam: "Milwaukee Bucks", StartTime: now.Add(10 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "nba-mia-phi", CompetitionID: "nba-2026", SportID: "basketball", Name: "Heat v 76ers", HomeTeam: "Miami Heat", AwayTeam: "Philadelphia 76ers", StartTime: now.Add(32 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "nba-den-okc", CompetitionID: "nba-2026", SportID: "basketball", Name: "Nuggets v Thunder", HomeTeam: "Denver Nuggets", AwayTeam: "Oklahoma City Thunder", StartTime: now.Add(34 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── Boxing ──
		{ID: "box-fur-usy", CompetitionID: "boxing-2026", SportID: "boxing", Name: "Fury v Usyk III", HomeTeam: "Tyson Fury", AwayTeam: "Oleksandr Usyk", StartTime: now.Add(168 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "box-can-ben", CompetitionID: "boxing-2026", SportID: "boxing", Name: "Canelo v Benavidez", HomeTeam: "Canelo Alvarez", AwayTeam: "David Benavidez", StartTime: now.Add(336 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "box-cra-spe", CompetitionID: "boxing-2026", SportID: "boxing", Name: "Crawford v Spence", HomeTeam: "Terence Crawford", AwayTeam: "Errol Spence Jr", StartTime: now.Add(504 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},

		// ── MMA ──
		{ID: "mma-mcg-cha", CompetitionID: "mma-2026", SportID: "mma", Name: "McGregor v Chandler", HomeTeam: "Conor McGregor", AwayTeam: "Michael Chandler", StartTime: now.Add(240 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
		{ID: "mma-ade-dup", CompetitionID: "mma-2026", SportID: "mma", Name: "Adesanya v Du Plessis", HomeTeam: "Israel Adesanya", AwayTeam: "Dricus Du Plessis", StartTime: now.Add(480 * time.Hour).Format(time.RFC3339), Status: "upcoming", InPlay: false},
	}

	// ── Markets ─────────────────────────────────────────────────
	s.markets = map[string]*Market{
		// Cricket: MI vs CSK
		"ipl-mi-csk-mo": {ID: "ipl-mi-csk-mo", EventID: "ipl-mi-csk", Sport: "cricket", Name: "Mumbai Indians v Chennai Super Kings - Match Odds", MarketType: "match_odds", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 2500000},
		"ipl-mi-csk-bm": {ID: "ipl-mi-csk-bm", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI v CSK - Bookmaker", MarketType: "bookmaker", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 1200000},
		"ipl-mi-csk-fancy1": {ID: "ipl-mi-csk-fancy1", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI Innings Runs - Over 150.5", MarketType: "fancy", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 800000},
		"ipl-mi-csk-fancy2": {ID: "ipl-mi-csk-fancy2", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI Innings Runs - Over 170.5", MarketType: "fancy", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 450000},
		"ipl-mi-csk-fancy3": {ID: "ipl-mi-csk-fancy3", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI 6 Over Runs - Over 48.5", MarketType: "session", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 320000},
		"ipl-mi-csk-fancy4": {ID: "ipl-mi-csk-fancy4", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI 10 Over Runs - Over 82.5", MarketType: "session", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 280000},
		"ipl-mi-csk-fancy5": {ID: "ipl-mi-csk-fancy5", EventID: "ipl-mi-csk", Sport: "cricket", Name: "Rohit Sharma Runs - Over 28.5", MarketType: "fancy", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 550000},
		"ipl-mi-csk-fancy6": {ID: "ipl-mi-csk-fancy6", EventID: "ipl-mi-csk", Sport: "cricket", Name: "MI Total Boundaries - Over 14.5", MarketType: "fancy", Status: "open", InPlay: true, StartTime: now.Add(-45 * time.Minute).Format(time.RFC3339), TotalMatched: 190000},
		// Cricket: RCB vs KKR
		"ipl-rcb-kkr-mo": {ID: "ipl-rcb-kkr-mo", EventID: "ipl-rcb-kkr", Sport: "cricket", Name: "Royal Challengers v KKR - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(3 * time.Hour).Format(time.RFC3339), TotalMatched: 500000},
		// Cricket: DC vs SRH
		"ipl-dc-srh-mo": {ID: "ipl-dc-srh-mo", EventID: "ipl-dc-srh", Sport: "cricket", Name: "Delhi Capitals v SRH - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(27 * time.Hour).Format(time.RFC3339), TotalMatched: 150000},
		// Football: Arsenal vs Chelsea
		"epl-ars-che-mo": {ID: "epl-ars-che-mo", EventID: "epl-ars-che", Sport: "football", Name: "Arsenal v Chelsea - Match Odds", MarketType: "match_odds", Status: "open", InPlay: true, StartTime: now.Add(-30 * time.Minute).Format(time.RFC3339), TotalMatched: 3200000},
		"epl-ars-che-ou": {ID: "epl-ars-che-ou", EventID: "epl-ars-che", Sport: "football", Name: "Arsenal v Chelsea - Over/Under 2.5", MarketType: "over_under", Status: "open", InPlay: true, StartTime: now.Add(-30 * time.Minute).Format(time.RFC3339), TotalMatched: 1100000},
		// Football: Man Utd vs Liverpool
		"epl-mun-liv-mo": {ID: "epl-mun-liv-mo", EventID: "epl-mun-liv", Sport: "football", Name: "Man United v Liverpool - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(5 * time.Hour).Format(time.RFC3339), TotalMatched: 750000},
		// Tennis: Djokovic vs Alcaraz
		"atp-djok-alc-mo": {ID: "atp-djok-alc-mo", EventID: "atp-djok-alc", Sport: "tennis", Name: "Djokovic v Alcaraz - Match Odds", MarketType: "match_odds", Status: "open", InPlay: true, StartTime: now.Add(-1 * time.Hour).Format(time.RFC3339), TotalMatched: 1800000},

		// ── IPL Additional Markets ──
		"ipl-gt-lsg-mo":  {ID: "ipl-gt-lsg-mo", EventID: "ipl-gt-lsg", Sport: "cricket", Name: "Gujarat Titans v Lucknow Super Giants - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(3 * time.Hour).Format(time.RFC3339), TotalMatched: 320000},
		"ipl-pbks-rr-mo": {ID: "ipl-pbks-rr-mo", EventID: "ipl-pbks-rr", Sport: "cricket", Name: "Punjab Kings v Rajasthan Royals - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(7 * time.Hour).Format(time.RFC3339), TotalMatched: 280000},
		"ipl-srh-dc-mo":  {ID: "ipl-srh-dc-mo", EventID: "ipl-srh-dc", Sport: "cricket", Name: "Sunrisers Hyderabad v Delhi Capitals - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(27 * time.Hour).Format(time.RFC3339), TotalMatched: 190000},
		"ipl-csk-kkr-mo": {ID: "ipl-csk-kkr-mo", EventID: "ipl-csk-kkr", Sport: "cricket", Name: "Chennai Super Kings v Kolkata Knight Riders - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(31 * time.Hour).Format(time.RFC3339), TotalMatched: 410000},
		"ipl-mi-rcb-mo":  {ID: "ipl-mi-rcb-mo", EventID: "ipl-mi-rcb", Sport: "cricket", Name: "Mumbai Indians v Royal Challengers Bengaluru - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(51 * time.Hour).Format(time.RFC3339), TotalMatched: 350000},
		"ipl-dc-gt-mo":   {ID: "ipl-dc-gt-mo", EventID: "ipl-dc-gt", Sport: "cricket", Name: "Delhi Capitals v Gujarat Titans - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(55 * time.Hour).Format(time.RFC3339), TotalMatched: 160000},
		"ipl-kkr-srh-mo": {ID: "ipl-kkr-srh-mo", EventID: "ipl-kkr-srh", Sport: "cricket", Name: "Kolkata Knight Riders v Sunrisers Hyderabad - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(75 * time.Hour).Format(time.RFC3339), TotalMatched: 130000},
		"ipl-rr-csk-mo":  {ID: "ipl-rr-csk-mo", EventID: "ipl-rr-csk", Sport: "cricket", Name: "Rajasthan Royals v Chennai Super Kings - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(79 * time.Hour).Format(time.RFC3339), TotalMatched: 220000},

		// ── PSL Markets ──
		"psl-lq-iu-mo": {ID: "psl-lq-iu-mo", EventID: "psl-lq-iu", Sport: "cricket", Name: "Lahore Qalandars v Islamabad United - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(5 * time.Hour).Format(time.RFC3339), TotalMatched: 95000},
		"psl-kk-pz-mo": {ID: "psl-kk-pz-mo", EventID: "psl-kk-pz", Sport: "cricket", Name: "Karachi Kings v Peshawar Zalmi - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(29 * time.Hour).Format(time.RFC3339), TotalMatched: 72000},
		"psl-ms-qg-mo": {ID: "psl-ms-qg-mo", EventID: "psl-ms-qg", Sport: "cricket", Name: "Multan Sultans v Quetta Gladiators - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(53 * time.Hour).Format(time.RFC3339), TotalMatched: 68000},

		// ── EPL Additional Markets ──
		"epl-tot-new-mo": {ID: "epl-tot-new-mo", EventID: "epl-tot-new", Sport: "football", Name: "Tottenham v Newcastle - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(4 * time.Hour).Format(time.RFC3339), TotalMatched: 620000},
		"epl-bri-whu-mo": {ID: "epl-bri-whu-mo", EventID: "epl-bri-whu", Sport: "football", Name: "Brighton v West Ham - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(6 * time.Hour).Format(time.RFC3339), TotalMatched: 410000},
		"epl-avl-wol-mo": {ID: "epl-avl-wol-mo", EventID: "epl-avl-wol", Sport: "football", Name: "Aston Villa v Wolves - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(28 * time.Hour).Format(time.RFC3339), TotalMatched: 380000},
		"epl-lei-bre-mo": {ID: "epl-lei-bre-mo", EventID: "epl-lei-bre", Sport: "football", Name: "Leicester v Brentford - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(30 * time.Hour).Format(time.RFC3339), TotalMatched: 290000},
		"epl-cry-bou-mo": {ID: "epl-cry-bou-mo", EventID: "epl-cry-bou", Sport: "football", Name: "Crystal Palace v Bournemouth - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(52 * time.Hour).Format(time.RFC3339), TotalMatched: 250000},
		"epl-ful-nfo-mo": {ID: "epl-ful-nfo-mo", EventID: "epl-ful-nfo", Sport: "football", Name: "Fulham v Nottingham Forest - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(54 * time.Hour).Format(time.RFC3339), TotalMatched: 310000},
		"epl-eve-sou-mo": {ID: "epl-eve-sou-mo", EventID: "epl-eve-sou", Sport: "football", Name: "Everton v Southampton - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(76 * time.Hour).Format(time.RFC3339), TotalMatched: 180000},
		"epl-lee-bur-mo": {ID: "epl-lee-bur-mo", EventID: "epl-lee-bur", Sport: "football", Name: "Leeds v Burnley - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(78 * time.Hour).Format(time.RFC3339), TotalMatched: 200000},

		// ── UCL Markets ──
		"ucl-rma-bay-mo": {ID: "ucl-rma-bay-mo", EventID: "ucl-rma-bay", Sport: "football", Name: "Real Madrid v Bayern Munich - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(48 * time.Hour).Format(time.RFC3339), TotalMatched: 1500000},
		"ucl-bar-psg-mo": {ID: "ucl-bar-psg-mo", EventID: "ucl-bar-psg", Sport: "football", Name: "Barcelona v PSG - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(48 * time.Hour).Format(time.RFC3339), TotalMatched: 1350000},
		"ucl-int-dor-mo": {ID: "ucl-int-dor-mo", EventID: "ucl-int-dor", Sport: "football", Name: "Inter Milan v Dortmund - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(72 * time.Hour).Format(time.RFC3339), TotalMatched: 680000},
		"ucl-atm-ars-mo": {ID: "ucl-atm-ars-mo", EventID: "ucl-atm-ars", Sport: "football", Name: "Atletico Madrid v Arsenal - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(72 * time.Hour).Format(time.RFC3339), TotalMatched: 920000},

		// ── ATP Madrid Additional Markets ──
		"atp-nad-tsi-mo": {ID: "atp-nad-tsi-mo", EventID: "atp-nad-tsi", Sport: "tennis", Name: "Nadal v Tsitsipas - Match Odds", MarketType: "match_odds", Status: "open", InPlay: true, StartTime: now.Add(-40 * time.Minute).Format(time.RFC3339), TotalMatched: 1100000},
		"atp-sin-med-mo": {ID: "atp-sin-med-mo", EventID: "atp-sin-med", Sport: "tennis", Name: "Sinner v Medvedev - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(2 * time.Hour).Format(time.RFC3339), TotalMatched: 750000},
		"atp-run-zve-mo": {ID: "atp-run-zve-mo", EventID: "atp-run-zve", Sport: "tennis", Name: "Rune v Zverev - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(4 * time.Hour).Format(time.RFC3339), TotalMatched: 520000},
		"atp-fri-ruu-mo": {ID: "atp-fri-ruu-mo", EventID: "atp-fri-ruu", Sport: "tennis", Name: "Fritz v Ruud - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(26 * time.Hour).Format(time.RFC3339), TotalMatched: 380000},
		"atp-rub-dim-mo": {ID: "atp-rub-dim-mo", EventID: "atp-rub-dim", Sport: "tennis", Name: "Rublev v Dimitrov - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(28 * time.Hour).Format(time.RFC3339), TotalMatched: 340000},
		"atp-dem-dra-mo": {ID: "atp-dem-dra-mo", EventID: "atp-dem-dra", Sport: "tennis", Name: "De Minaur v Draper - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(50 * time.Hour).Format(time.RFC3339), TotalMatched: 210000},

		// ── NBA Markets ──
		"nba-lal-bos-mo": {ID: "nba-lal-bos-mo", EventID: "nba-lal-bos", Sport: "basketball", Name: "Lakers v Celtics - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(8 * time.Hour).Format(time.RFC3339), TotalMatched: 890000},
		"nba-gsw-mil-mo": {ID: "nba-gsw-mil-mo", EventID: "nba-gsw-mil", Sport: "basketball", Name: "Warriors v Bucks - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(10 * time.Hour).Format(time.RFC3339), TotalMatched: 720000},
		"nba-mia-phi-mo": {ID: "nba-mia-phi-mo", EventID: "nba-mia-phi", Sport: "basketball", Name: "Heat v 76ers - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(32 * time.Hour).Format(time.RFC3339), TotalMatched: 540000},
		"nba-den-okc-mo": {ID: "nba-den-okc-mo", EventID: "nba-den-okc", Sport: "basketball", Name: "Nuggets v Thunder - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(34 * time.Hour).Format(time.RFC3339), TotalMatched: 610000},

		// ── Boxing Markets ──
		"box-fur-usy-mo": {ID: "box-fur-usy-mo", EventID: "box-fur-usy", Sport: "boxing", Name: "Fury v Usyk III - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(168 * time.Hour).Format(time.RFC3339), TotalMatched: 2100000},
		"box-can-ben-mo": {ID: "box-can-ben-mo", EventID: "box-can-ben", Sport: "boxing", Name: "Canelo v Benavidez - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(336 * time.Hour).Format(time.RFC3339), TotalMatched: 1800000},
		"box-cra-spe-mo": {ID: "box-cra-spe-mo", EventID: "box-cra-spe", Sport: "boxing", Name: "Crawford v Spence - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(504 * time.Hour).Format(time.RFC3339), TotalMatched: 950000},

		// ── MMA Markets ──
		"mma-mcg-cha-mo": {ID: "mma-mcg-cha-mo", EventID: "mma-mcg-cha", Sport: "mma", Name: "McGregor v Chandler - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(240 * time.Hour).Format(time.RFC3339), TotalMatched: 3200000},
		"mma-ade-dup-mo": {ID: "mma-ade-dup-mo", EventID: "mma-ade-dup", Sport: "mma", Name: "Adesanya v Du Plessis - Match Odds", MarketType: "match_odds", Status: "open", InPlay: false, StartTime: now.Add(480 * time.Hour).Format(time.RFC3339), TotalMatched: 1400000},
	}

	// ── Runners ─────────────────────────────────────────────────
	s.runners = map[string][]*Runner{
		"ipl-mi-csk-mo": {
			{MarketID: "ipl-mi-csk-mo", SelectionID: 101, Name: "Mumbai Indians", Status: "active", BackPrices: []PriceSize{{1.85, 50000}, {1.84, 30000}, {1.83, 20000}}, LayPrices: []PriceSize{{1.87, 45000}, {1.88, 25000}, {1.89, 15000}}},
			{MarketID: "ipl-mi-csk-mo", SelectionID: 102, Name: "Chennai Super Kings", Status: "active", BackPrices: []PriceSize{{2.10, 40000}, {2.08, 28000}, {2.06, 18000}}, LayPrices: []PriceSize{{2.12, 38000}, {2.14, 22000}, {2.16, 12000}}},
		},
		"ipl-mi-csk-bm": {
			{MarketID: "ipl-mi-csk-bm", SelectionID: 201, Name: "Mumbai Indians", Status: "active", BackPrices: []PriceSize{{1.83, 100000}}, LayPrices: []PriceSize{{1.85, 100000}}},
			{MarketID: "ipl-mi-csk-bm", SelectionID: 202, Name: "Chennai Super Kings", Status: "active", BackPrices: []PriceSize{{2.05, 80000}}, LayPrices: []PriceSize{{2.08, 80000}}},
		},
		"ipl-mi-csk-fancy1": {
			{MarketID: "ipl-mi-csk-fancy1", SelectionID: 301, Name: "MI Innings Runs", Status: "active", BackPrices: []PriceSize{{1.75, 200000}}, LayPrices: []PriceSize{{1.80, 180000}}, RunValue: 150, YesRate: 82, NoRate: 86},
		},
		"ipl-mi-csk-fancy2": {
			{MarketID: "ipl-mi-csk-fancy2", SelectionID: 302, Name: "MI Innings Runs", Status: "active", BackPrices: []PriceSize{{2.10, 150000}}, LayPrices: []PriceSize{{2.15, 130000}}, RunValue: 170, YesRate: 90, NoRate: 95},
		},
		"ipl-mi-csk-fancy3": {
			{MarketID: "ipl-mi-csk-fancy3", SelectionID: 303, Name: "MI 6 Over Runs", Status: "active", BackPrices: []PriceSize{{1.85, 180000}}, LayPrices: []PriceSize{{1.90, 160000}}, RunValue: 48, YesRate: 85, NoRate: 90},
		},
		"ipl-mi-csk-fancy4": {
			{MarketID: "ipl-mi-csk-fancy4", SelectionID: 304, Name: "MI 10 Over Runs", Status: "active", BackPrices: []PriceSize{{1.90, 160000}}, LayPrices: []PriceSize{{1.95, 140000}}, RunValue: 82, YesRate: 88, NoRate: 92},
		},
		"ipl-mi-csk-fancy5": {
			{MarketID: "ipl-mi-csk-fancy5", SelectionID: 305, Name: "Rohit Sharma Runs", Status: "active", BackPrices: []PriceSize{{1.80, 250000}}, LayPrices: []PriceSize{{1.85, 220000}}, RunValue: 28, YesRate: 80, NoRate: 85},
		},
		"ipl-mi-csk-fancy6": {
			{MarketID: "ipl-mi-csk-fancy6", SelectionID: 306, Name: "MI Total Boundaries", Status: "active", BackPrices: []PriceSize{{1.95, 120000}}, LayPrices: []PriceSize{{2.00, 100000}}, RunValue: 14, YesRate: 78, NoRate: 83},
		},
		"ipl-rcb-kkr-mo": {
			{MarketID: "ipl-rcb-kkr-mo", SelectionID: 401, Name: "Royal Challengers Bengaluru", Status: "active", BackPrices: []PriceSize{{1.95, 35000}}, LayPrices: []PriceSize{{1.98, 30000}}},
			{MarketID: "ipl-rcb-kkr-mo", SelectionID: 402, Name: "Kolkata Knight Riders", Status: "active", BackPrices: []PriceSize{{1.95, 32000}}, LayPrices: []PriceSize{{1.98, 28000}}},
		},
		"ipl-dc-srh-mo": {
			{MarketID: "ipl-dc-srh-mo", SelectionID: 501, Name: "Delhi Capitals", Status: "active", BackPrices: []PriceSize{{2.20, 20000}}, LayPrices: []PriceSize{{2.24, 18000}}},
			{MarketID: "ipl-dc-srh-mo", SelectionID: 502, Name: "Sunrisers Hyderabad", Status: "active", BackPrices: []PriceSize{{1.75, 22000}}, LayPrices: []PriceSize{{1.78, 20000}}},
		},
		"epl-ars-che-mo": {
			{MarketID: "epl-ars-che-mo", SelectionID: 601, Name: "Arsenal", Status: "active", BackPrices: []PriceSize{{1.65, 80000}, {1.64, 50000}}, LayPrices: []PriceSize{{1.67, 75000}, {1.68, 45000}}},
			{MarketID: "epl-ars-che-mo", SelectionID: 602, Name: "Chelsea", Status: "active", BackPrices: []PriceSize{{5.50, 25000}}, LayPrices: []PriceSize{{5.60, 22000}}},
			{MarketID: "epl-ars-che-mo", SelectionID: 603, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.80, 30000}}, LayPrices: []PriceSize{{3.90, 28000}}},
		},
		"epl-ars-che-ou": {
			{MarketID: "epl-ars-che-ou", SelectionID: 611, Name: "Over 2.5 Goals", Status: "active", BackPrices: []PriceSize{{1.72, 60000}}, LayPrices: []PriceSize{{1.75, 55000}}},
			{MarketID: "epl-ars-che-ou", SelectionID: 612, Name: "Under 2.5 Goals", Status: "active", BackPrices: []PriceSize{{2.20, 50000}}, LayPrices: []PriceSize{{2.24, 45000}}},
		},
		"epl-mun-liv-mo": {
			{MarketID: "epl-mun-liv-mo", SelectionID: 701, Name: "Manchester United", Status: "active", BackPrices: []PriceSize{{3.20, 40000}}, LayPrices: []PriceSize{{3.25, 35000}}},
			{MarketID: "epl-mun-liv-mo", SelectionID: 702, Name: "Liverpool", Status: "active", BackPrices: []PriceSize{{2.10, 55000}}, LayPrices: []PriceSize{{2.14, 48000}}},
			{MarketID: "epl-mun-liv-mo", SelectionID: 703, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.50, 30000}}, LayPrices: []PriceSize{{3.60, 25000}}},
		},
		"atp-djok-alc-mo": {
			{MarketID: "atp-djok-alc-mo", SelectionID: 801, Name: "Novak Djokovic", Status: "active", BackPrices: []PriceSize{{2.40, 60000}, {2.38, 40000}}, LayPrices: []PriceSize{{2.44, 55000}, {2.46, 35000}}},
			{MarketID: "atp-djok-alc-mo", SelectionID: 802, Name: "Carlos Alcaraz", Status: "active", BackPrices: []PriceSize{{1.62, 70000}, {1.60, 45000}}, LayPrices: []PriceSize{{1.64, 65000}, {1.66, 40000}}},
		},

		// ── IPL Additional Runners ──
		"ipl-gt-lsg-mo": {
			{MarketID: "ipl-gt-lsg-mo", SelectionID: 1001, Name: "Gujarat Titans", Status: "active", BackPrices: []PriceSize{{1.90, 42000}, {1.89, 28000}, {1.88, 18000}}, LayPrices: []PriceSize{{1.92, 40000}, {1.93, 25000}, {1.95, 15000}}},
			{MarketID: "ipl-gt-lsg-mo", SelectionID: 1002, Name: "Lucknow Super Giants", Status: "active", BackPrices: []PriceSize{{2.00, 38000}, {1.98, 26000}, {1.96, 16000}}, LayPrices: []PriceSize{{2.02, 36000}, {2.04, 24000}, {2.06, 14000}}},
		},
		"ipl-pbks-rr-mo": {
			{MarketID: "ipl-pbks-rr-mo", SelectionID: 1003, Name: "Punjab Kings", Status: "active", BackPrices: []PriceSize{{2.30, 30000}, {2.28, 20000}, {2.26, 12000}}, LayPrices: []PriceSize{{2.34, 28000}, {2.36, 18000}, {2.38, 10000}}},
			{MarketID: "ipl-pbks-rr-mo", SelectionID: 1004, Name: "Rajasthan Royals", Status: "active", BackPrices: []PriceSize{{1.70, 35000}, {1.68, 24000}, {1.66, 15000}}, LayPrices: []PriceSize{{1.72, 33000}, {1.74, 22000}, {1.76, 13000}}},
		},
		"ipl-srh-dc-mo": {
			{MarketID: "ipl-srh-dc-mo", SelectionID: 1005, Name: "Sunrisers Hyderabad", Status: "active", BackPrices: []PriceSize{{1.80, 28000}, {1.78, 19000}, {1.76, 12000}}, LayPrices: []PriceSize{{1.82, 26000}, {1.84, 17000}, {1.86, 10000}}},
			{MarketID: "ipl-srh-dc-mo", SelectionID: 1006, Name: "Delhi Capitals", Status: "active", BackPrices: []PriceSize{{2.12, 25000}, {2.10, 17000}, {2.08, 11000}}, LayPrices: []PriceSize{{2.16, 23000}, {2.18, 15000}, {2.20, 9000}}},
		},
		"ipl-csk-kkr-mo": {
			{MarketID: "ipl-csk-kkr-mo", SelectionID: 1007, Name: "Chennai Super Kings", Status: "active", BackPrices: []PriceSize{{1.75, 50000}, {1.73, 35000}, {1.71, 22000}}, LayPrices: []PriceSize{{1.77, 48000}, {1.79, 32000}, {1.81, 20000}}},
			{MarketID: "ipl-csk-kkr-mo", SelectionID: 1008, Name: "Kolkata Knight Riders", Status: "active", BackPrices: []PriceSize{{2.18, 40000}, {2.16, 28000}, {2.14, 18000}}, LayPrices: []PriceSize{{2.22, 38000}, {2.24, 26000}, {2.26, 16000}}},
		},
		"ipl-mi-rcb-mo": {
			{MarketID: "ipl-mi-rcb-mo", SelectionID: 1009, Name: "Mumbai Indians", Status: "active", BackPrices: []PriceSize{{1.85, 45000}, {1.83, 30000}, {1.81, 20000}}, LayPrices: []PriceSize{{1.87, 42000}, {1.89, 28000}, {1.91, 18000}}},
			{MarketID: "ipl-mi-rcb-mo", SelectionID: 1010, Name: "Royal Challengers Bengaluru", Status: "active", BackPrices: []PriceSize{{2.06, 38000}, {2.04, 25000}, {2.02, 16000}}, LayPrices: []PriceSize{{2.08, 35000}, {2.10, 23000}, {2.12, 14000}}},
		},
		"ipl-dc-gt-mo": {
			{MarketID: "ipl-dc-gt-mo", SelectionID: 1011, Name: "Delhi Capitals", Status: "active", BackPrices: []PriceSize{{2.25, 22000}, {2.22, 15000}, {2.20, 10000}}, LayPrices: []PriceSize{{2.28, 20000}, {2.30, 14000}, {2.32, 9000}}},
			{MarketID: "ipl-dc-gt-mo", SelectionID: 1012, Name: "Gujarat Titans", Status: "active", BackPrices: []PriceSize{{1.72, 26000}, {1.70, 18000}, {1.68, 12000}}, LayPrices: []PriceSize{{1.74, 24000}, {1.76, 16000}, {1.78, 10000}}},
		},
		"ipl-kkr-srh-mo": {
			{MarketID: "ipl-kkr-srh-mo", SelectionID: 1013, Name: "Kolkata Knight Riders", Status: "active", BackPrices: []PriceSize{{1.95, 20000}, {1.93, 14000}, {1.91, 9000}}, LayPrices: []PriceSize{{1.98, 18000}, {2.00, 12000}, {2.02, 8000}}},
			{MarketID: "ipl-kkr-srh-mo", SelectionID: 1014, Name: "Sunrisers Hyderabad", Status: "active", BackPrices: []PriceSize{{1.95, 20000}, {1.93, 14000}, {1.91, 9000}}, LayPrices: []PriceSize{{1.98, 18000}, {2.00, 12000}, {2.02, 8000}}},
		},
		"ipl-rr-csk-mo": {
			{MarketID: "ipl-rr-csk-mo", SelectionID: 1015, Name: "Rajasthan Royals", Status: "active", BackPrices: []PriceSize{{2.10, 24000}, {2.08, 16000}, {2.06, 10000}}, LayPrices: []PriceSize{{2.14, 22000}, {2.16, 14000}, {2.18, 9000}}},
			{MarketID: "ipl-rr-csk-mo", SelectionID: 1016, Name: "Chennai Super Kings", Status: "active", BackPrices: []PriceSize{{1.82, 30000}, {1.80, 20000}, {1.78, 13000}}, LayPrices: []PriceSize{{1.84, 28000}, {1.86, 18000}, {1.88, 11000}}},
		},

		// ── PSL Runners ──
		"psl-lq-iu-mo": {
			{MarketID: "psl-lq-iu-mo", SelectionID: 1101, Name: "Lahore Qalandars", Status: "active", BackPrices: []PriceSize{{1.80, 15000}, {1.78, 10000}, {1.76, 7000}}, LayPrices: []PriceSize{{1.83, 14000}, {1.85, 9000}, {1.87, 6000}}},
			{MarketID: "psl-lq-iu-mo", SelectionID: 1102, Name: "Islamabad United", Status: "active", BackPrices: []PriceSize{{2.12, 13000}, {2.10, 9000}, {2.08, 6000}}, LayPrices: []PriceSize{{2.16, 12000}, {2.18, 8000}, {2.20, 5000}}},
		},
		"psl-kk-pz-mo": {
			{MarketID: "psl-kk-pz-mo", SelectionID: 1103, Name: "Karachi Kings", Status: "active", BackPrices: []PriceSize{{2.40, 11000}, {2.38, 8000}, {2.36, 5000}}, LayPrices: []PriceSize{{2.44, 10000}, {2.46, 7000}, {2.48, 4500}}},
			{MarketID: "psl-kk-pz-mo", SelectionID: 1104, Name: "Peshawar Zalmi", Status: "active", BackPrices: []PriceSize{{1.62, 14000}, {1.60, 10000}, {1.58, 7000}}, LayPrices: []PriceSize{{1.65, 13000}, {1.67, 9000}, {1.69, 6000}}},
		},
		"psl-ms-qg-mo": {
			{MarketID: "psl-ms-qg-mo", SelectionID: 1105, Name: "Multan Sultans", Status: "active", BackPrices: []PriceSize{{1.55, 16000}, {1.53, 11000}, {1.51, 8000}}, LayPrices: []PriceSize{{1.58, 15000}, {1.60, 10000}, {1.62, 7000}}},
			{MarketID: "psl-ms-qg-mo", SelectionID: 1106, Name: "Quetta Gladiators", Status: "active", BackPrices: []PriceSize{{2.60, 10000}, {2.56, 7000}, {2.52, 5000}}, LayPrices: []PriceSize{{2.64, 9000}, {2.68, 6000}, {2.72, 4000}}},
		},

		// ── EPL Additional Runners ──
		"epl-tot-new-mo": {
			{MarketID: "epl-tot-new-mo", SelectionID: 1201, Name: "Tottenham Hotspur", Status: "active", BackPrices: []PriceSize{{2.20, 55000}, {2.18, 38000}, {2.16, 25000}}, LayPrices: []PriceSize{{2.24, 52000}, {2.26, 35000}, {2.28, 22000}}},
			{MarketID: "epl-tot-new-mo", SelectionID: 1202, Name: "Newcastle United", Status: "active", BackPrices: []PriceSize{{3.10, 30000}, {3.05, 20000}, {3.00, 14000}}, LayPrices: []PriceSize{{3.15, 28000}, {3.20, 18000}, {3.25, 12000}}},
			{MarketID: "epl-tot-new-mo", SelectionID: 1203, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.50, 25000}, {3.45, 17000}, {3.40, 11000}}, LayPrices: []PriceSize{{3.55, 23000}, {3.60, 15000}, {3.65, 10000}}},
		},
		"epl-bri-whu-mo": {
			{MarketID: "epl-bri-whu-mo", SelectionID: 1204, Name: "Brighton & Hove Albion", Status: "active", BackPrices: []PriceSize{{1.85, 48000}, {1.83, 32000}, {1.81, 20000}}, LayPrices: []PriceSize{{1.87, 45000}, {1.89, 30000}, {1.91, 18000}}},
			{MarketID: "epl-bri-whu-mo", SelectionID: 1205, Name: "West Ham United", Status: "active", BackPrices: []PriceSize{{4.20, 18000}, {4.10, 12000}, {4.00, 8000}}, LayPrices: []PriceSize{{4.30, 16000}, {4.40, 10000}, {4.50, 7000}}},
			{MarketID: "epl-bri-whu-mo", SelectionID: 1206, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.80, 22000}, {3.75, 15000}, {3.70, 10000}}, LayPrices: []PriceSize{{3.85, 20000}, {3.90, 13000}, {3.95, 9000}}},
		},
		"epl-avl-wol-mo": {
			{MarketID: "epl-avl-wol-mo", SelectionID: 1207, Name: "Aston Villa", Status: "active", BackPrices: []PriceSize{{1.70, 52000}, {1.68, 35000}, {1.66, 22000}}, LayPrices: []PriceSize{{1.72, 50000}, {1.74, 33000}, {1.76, 20000}}},
			{MarketID: "epl-avl-wol-mo", SelectionID: 1208, Name: "Wolverhampton Wanderers", Status: "active", BackPrices: []PriceSize{{5.00, 14000}, {4.90, 10000}, {4.80, 7000}}, LayPrices: []PriceSize{{5.10, 12000}, {5.20, 9000}, {5.30, 6000}}},
			{MarketID: "epl-avl-wol-mo", SelectionID: 1209, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.90, 20000}, {3.85, 14000}, {3.80, 9000}}, LayPrices: []PriceSize{{3.95, 18000}, {4.00, 12000}, {4.05, 8000}}},
		},
		"epl-lei-bre-mo": {
			{MarketID: "epl-lei-bre-mo", SelectionID: 1210, Name: "Leicester City", Status: "active", BackPrices: []PriceSize{{2.60, 28000}, {2.56, 19000}, {2.52, 12000}}, LayPrices: []PriceSize{{2.64, 26000}, {2.68, 17000}, {2.72, 10000}}},
			{MarketID: "epl-lei-bre-mo", SelectionID: 1211, Name: "Brentford", Status: "active", BackPrices: []PriceSize{{2.70, 26000}, {2.66, 18000}, {2.62, 11000}}, LayPrices: []PriceSize{{2.74, 24000}, {2.78, 16000}, {2.82, 10000}}},
			{MarketID: "epl-lei-bre-mo", SelectionID: 1212, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.40, 22000}, {3.35, 15000}, {3.30, 10000}}, LayPrices: []PriceSize{{3.45, 20000}, {3.50, 13000}, {3.55, 9000}}},
		},
		"epl-cry-bou-mo": {
			{MarketID: "epl-cry-bou-mo", SelectionID: 1213, Name: "Crystal Palace", Status: "active", BackPrices: []PriceSize{{2.30, 30000}, {2.28, 20000}, {2.26, 13000}}, LayPrices: []PriceSize{{2.34, 28000}, {2.36, 18000}, {2.38, 11000}}},
			{MarketID: "epl-cry-bou-mo", SelectionID: 1214, Name: "AFC Bournemouth", Status: "active", BackPrices: []PriceSize{{3.20, 20000}, {3.15, 14000}, {3.10, 9000}}, LayPrices: []PriceSize{{3.25, 18000}, {3.30, 12000}, {3.35, 8000}}},
			{MarketID: "epl-cry-bou-mo", SelectionID: 1215, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.40, 22000}, {3.35, 15000}, {3.30, 10000}}, LayPrices: []PriceSize{{3.45, 20000}, {3.50, 13000}, {3.55, 9000}}},
		},
		"epl-ful-nfo-mo": {
			{MarketID: "epl-ful-nfo-mo", SelectionID: 1216, Name: "Fulham", Status: "active", BackPrices: []PriceSize{{2.50, 32000}, {2.48, 22000}, {2.46, 14000}}, LayPrices: []PriceSize{{2.54, 30000}, {2.56, 20000}, {2.58, 12000}}},
			{MarketID: "epl-ful-nfo-mo", SelectionID: 1217, Name: "Nottingham Forest", Status: "active", BackPrices: []PriceSize{{2.80, 24000}, {2.76, 16000}, {2.72, 10000}}, LayPrices: []PriceSize{{2.84, 22000}, {2.88, 14000}, {2.92, 9000}}},
			{MarketID: "epl-ful-nfo-mo", SelectionID: 1218, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.40, 20000}, {3.35, 14000}, {3.30, 9000}}, LayPrices: []PriceSize{{3.45, 18000}, {3.50, 12000}, {3.55, 8000}}},
		},
		"epl-eve-sou-mo": {
			{MarketID: "epl-eve-sou-mo", SelectionID: 1219, Name: "Everton", Status: "active", BackPrices: []PriceSize{{2.10, 25000}, {2.08, 17000}, {2.06, 11000}}, LayPrices: []PriceSize{{2.14, 23000}, {2.16, 15000}, {2.18, 10000}}},
			{MarketID: "epl-eve-sou-mo", SelectionID: 1220, Name: "Southampton", Status: "active", BackPrices: []PriceSize{{3.60, 15000}, {3.55, 10000}, {3.50, 7000}}, LayPrices: []PriceSize{{3.65, 14000}, {3.70, 9000}, {3.75, 6000}}},
			{MarketID: "epl-eve-sou-mo", SelectionID: 1221, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.40, 18000}, {3.35, 12000}, {3.30, 8000}}, LayPrices: []PriceSize{{3.45, 16000}, {3.50, 11000}, {3.55, 7000}}},
		},
		"epl-lee-bur-mo": {
			{MarketID: "epl-lee-bur-mo", SelectionID: 1222, Name: "Leeds United", Status: "active", BackPrices: []PriceSize{{2.00, 28000}, {1.98, 19000}, {1.96, 12000}}, LayPrices: []PriceSize{{2.04, 26000}, {2.06, 17000}, {2.08, 10000}}},
			{MarketID: "epl-lee-bur-mo", SelectionID: 1223, Name: "Burnley", Status: "active", BackPrices: []PriceSize{{3.80, 16000}, {3.75, 11000}, {3.70, 7000}}, LayPrices: []PriceSize{{3.85, 14000}, {3.90, 10000}, {3.95, 6000}}},
			{MarketID: "epl-lee-bur-mo", SelectionID: 1224, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.50, 20000}, {3.45, 14000}, {3.40, 9000}}, LayPrices: []PriceSize{{3.55, 18000}, {3.60, 12000}, {3.65, 8000}}},
		},

		// ── UCL Runners ──
		"ucl-rma-bay-mo": {
			{MarketID: "ucl-rma-bay-mo", SelectionID: 1301, Name: "Real Madrid", Status: "active", BackPrices: []PriceSize{{2.10, 95000}, {2.08, 65000}, {2.06, 42000}}, LayPrices: []PriceSize{{2.14, 90000}, {2.16, 60000}, {2.18, 38000}}},
			{MarketID: "ucl-rma-bay-mo", SelectionID: 1302, Name: "Bayern Munich", Status: "active", BackPrices: []PriceSize{{3.20, 50000}, {3.15, 35000}, {3.10, 22000}}, LayPrices: []PriceSize{{3.25, 48000}, {3.30, 32000}, {3.35, 20000}}},
			{MarketID: "ucl-rma-bay-mo", SelectionID: 1303, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.60, 40000}, {3.55, 28000}, {3.50, 18000}}, LayPrices: []PriceSize{{3.65, 38000}, {3.70, 25000}, {3.75, 16000}}},
		},
		"ucl-bar-psg-mo": {
			{MarketID: "ucl-bar-psg-mo", SelectionID: 1304, Name: "FC Barcelona", Status: "active", BackPrices: []PriceSize{{2.00, 88000}, {1.98, 60000}, {1.96, 40000}}, LayPrices: []PriceSize{{2.04, 85000}, {2.06, 58000}, {2.08, 37000}}},
			{MarketID: "ucl-bar-psg-mo", SelectionID: 1305, Name: "Paris Saint-Germain", Status: "active", BackPrices: []PriceSize{{3.40, 42000}, {3.35, 28000}, {3.30, 18000}}, LayPrices: []PriceSize{{3.45, 40000}, {3.50, 26000}, {3.55, 16000}}},
			{MarketID: "ucl-bar-psg-mo", SelectionID: 1306, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.70, 35000}, {3.65, 24000}, {3.60, 15000}}, LayPrices: []PriceSize{{3.75, 33000}, {3.80, 22000}, {3.85, 14000}}},
		},
		"ucl-int-dor-mo": {
			{MarketID: "ucl-int-dor-mo", SelectionID: 1307, Name: "Inter Milan", Status: "active", BackPrices: []PriceSize{{1.80, 62000}, {1.78, 42000}, {1.76, 28000}}, LayPrices: []PriceSize{{1.83, 58000}, {1.85, 40000}, {1.87, 25000}}},
			{MarketID: "ucl-int-dor-mo", SelectionID: 1308, Name: "Borussia Dortmund", Status: "active", BackPrices: []PriceSize{{4.20, 22000}, {4.10, 15000}, {4.00, 10000}}, LayPrices: []PriceSize{{4.30, 20000}, {4.40, 13000}, {4.50, 9000}}},
			{MarketID: "ucl-int-dor-mo", SelectionID: 1309, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.80, 28000}, {3.75, 19000}, {3.70, 12000}}, LayPrices: []PriceSize{{3.85, 26000}, {3.90, 17000}, {3.95, 11000}}},
		},
		"ucl-atm-ars-mo": {
			{MarketID: "ucl-atm-ars-mo", SelectionID: 1310, Name: "Atletico Madrid", Status: "active", BackPrices: []PriceSize{{2.80, 55000}, {2.76, 38000}, {2.72, 24000}}, LayPrices: []PriceSize{{2.84, 52000}, {2.88, 35000}, {2.92, 22000}}},
			{MarketID: "ucl-atm-ars-mo", SelectionID: 1311, Name: "Arsenal", Status: "active", BackPrices: []PriceSize{{2.40, 60000}, {2.38, 40000}, {2.36, 26000}}, LayPrices: []PriceSize{{2.44, 57000}, {2.46, 38000}, {2.48, 24000}}},
			{MarketID: "ucl-atm-ars-mo", SelectionID: 1312, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{3.40, 32000}, {3.35, 22000}, {3.30, 14000}}, LayPrices: []PriceSize{{3.45, 30000}, {3.50, 20000}, {3.55, 13000}}},
		},

		// ── ATP Madrid Additional Runners ──
		"atp-nad-tsi-mo": {
			{MarketID: "atp-nad-tsi-mo", SelectionID: 1401, Name: "Rafael Nadal", Status: "active", BackPrices: []PriceSize{{2.80, 50000}, {2.76, 35000}, {2.72, 22000}}, LayPrices: []PriceSize{{2.84, 48000}, {2.88, 32000}, {2.92, 20000}}},
			{MarketID: "atp-nad-tsi-mo", SelectionID: 1402, Name: "Stefanos Tsitsipas", Status: "active", BackPrices: []PriceSize{{1.48, 65000}, {1.46, 45000}, {1.44, 30000}}, LayPrices: []PriceSize{{1.50, 62000}, {1.52, 42000}, {1.54, 28000}}},
		},
		"atp-sin-med-mo": {
			{MarketID: "atp-sin-med-mo", SelectionID: 1403, Name: "Jannik Sinner", Status: "active", BackPrices: []PriceSize{{1.55, 58000}, {1.53, 40000}, {1.51, 26000}}, LayPrices: []PriceSize{{1.57, 55000}, {1.59, 38000}, {1.61, 24000}}},
			{MarketID: "atp-sin-med-mo", SelectionID: 1404, Name: "Daniil Medvedev", Status: "active", BackPrices: []PriceSize{{2.55, 42000}, {2.52, 28000}, {2.48, 18000}}, LayPrices: []PriceSize{{2.58, 40000}, {2.62, 26000}, {2.66, 16000}}},
		},
		"atp-run-zve-mo": {
			{MarketID: "atp-run-zve-mo", SelectionID: 1405, Name: "Holger Rune", Status: "active", BackPrices: []PriceSize{{2.90, 30000}, {2.86, 20000}, {2.82, 13000}}, LayPrices: []PriceSize{{2.94, 28000}, {2.98, 18000}, {3.02, 12000}}},
			{MarketID: "atp-run-zve-mo", SelectionID: 1406, Name: "Alexander Zverev", Status: "active", BackPrices: []PriceSize{{1.45, 52000}, {1.43, 36000}, {1.41, 24000}}, LayPrices: []PriceSize{{1.47, 50000}, {1.49, 34000}, {1.51, 22000}}},
		},
		"atp-fri-ruu-mo": {
			{MarketID: "atp-fri-ruu-mo", SelectionID: 1407, Name: "Taylor Fritz", Status: "active", BackPrices: []PriceSize{{2.10, 35000}, {2.08, 24000}, {2.06, 15000}}, LayPrices: []PriceSize{{2.14, 33000}, {2.16, 22000}, {2.18, 14000}}},
			{MarketID: "atp-fri-ruu-mo", SelectionID: 1408, Name: "Casper Ruud", Status: "active", BackPrices: []PriceSize{{1.80, 40000}, {1.78, 28000}, {1.76, 18000}}, LayPrices: []PriceSize{{1.83, 38000}, {1.85, 26000}, {1.87, 16000}}},
		},
		"atp-rub-dim-mo": {
			{MarketID: "atp-rub-dim-mo", SelectionID: 1409, Name: "Andrey Rublev", Status: "active", BackPrices: []PriceSize{{1.72, 38000}, {1.70, 26000}, {1.68, 17000}}, LayPrices: []PriceSize{{1.74, 36000}, {1.76, 24000}, {1.78, 15000}}},
			{MarketID: "atp-rub-dim-mo", SelectionID: 1410, Name: "Grigor Dimitrov", Status: "active", BackPrices: []PriceSize{{2.20, 30000}, {2.18, 20000}, {2.16, 13000}}, LayPrices: []PriceSize{{2.24, 28000}, {2.26, 18000}, {2.28, 12000}}},
		},
		"atp-dem-dra-mo": {
			{MarketID: "atp-dem-dra-mo", SelectionID: 1411, Name: "Alex de Minaur", Status: "active", BackPrices: []PriceSize{{1.90, 25000}, {1.88, 17000}, {1.86, 11000}}, LayPrices: []PriceSize{{1.93, 23000}, {1.95, 15000}, {1.97, 10000}}},
			{MarketID: "atp-dem-dra-mo", SelectionID: 1412, Name: "Jack Draper", Status: "active", BackPrices: []PriceSize{{2.00, 24000}, {1.98, 16000}, {1.96, 10000}}, LayPrices: []PriceSize{{2.04, 22000}, {2.06, 14000}, {2.08, 9000}}},
		},

		// ── NBA Runners ──
		"nba-lal-bos-mo": {
			{MarketID: "nba-lal-bos-mo", SelectionID: 1501, Name: "Los Angeles Lakers", Status: "active", BackPrices: []PriceSize{{2.30, 65000}, {2.28, 45000}, {2.26, 30000}}, LayPrices: []PriceSize{{2.34, 62000}, {2.36, 42000}, {2.38, 28000}}},
			{MarketID: "nba-lal-bos-mo", SelectionID: 1502, Name: "Boston Celtics", Status: "active", BackPrices: []PriceSize{{1.65, 78000}, {1.63, 52000}, {1.61, 35000}}, LayPrices: []PriceSize{{1.67, 75000}, {1.69, 50000}, {1.71, 32000}}},
		},
		"nba-gsw-mil-mo": {
			{MarketID: "nba-gsw-mil-mo", SelectionID: 1503, Name: "Golden State Warriors", Status: "active", BackPrices: []PriceSize{{2.10, 55000}, {2.08, 38000}, {2.06, 25000}}, LayPrices: []PriceSize{{2.14, 52000}, {2.16, 35000}, {2.18, 23000}}},
			{MarketID: "nba-gsw-mil-mo", SelectionID: 1504, Name: "Milwaukee Bucks", Status: "active", BackPrices: []PriceSize{{1.80, 62000}, {1.78, 42000}, {1.76, 28000}}, LayPrices: []PriceSize{{1.83, 58000}, {1.85, 40000}, {1.87, 26000}}},
		},
		"nba-mia-phi-mo": {
			{MarketID: "nba-mia-phi-mo", SelectionID: 1505, Name: "Miami Heat", Status: "active", BackPrices: []PriceSize{{1.95, 48000}, {1.93, 32000}, {1.91, 20000}}, LayPrices: []PriceSize{{1.98, 45000}, {2.00, 30000}, {2.02, 18000}}},
			{MarketID: "nba-mia-phi-mo", SelectionID: 1506, Name: "Philadelphia 76ers", Status: "active", BackPrices: []PriceSize{{1.95, 46000}, {1.93, 31000}, {1.91, 20000}}, LayPrices: []PriceSize{{1.98, 44000}, {2.00, 29000}, {2.02, 18000}}},
		},
		"nba-den-okc-mo": {
			{MarketID: "nba-den-okc-mo", SelectionID: 1507, Name: "Denver Nuggets", Status: "active", BackPrices: []PriceSize{{2.20, 50000}, {2.18, 34000}, {2.16, 22000}}, LayPrices: []PriceSize{{2.24, 48000}, {2.26, 32000}, {2.28, 20000}}},
			{MarketID: "nba-den-okc-mo", SelectionID: 1508, Name: "Oklahoma City Thunder", Status: "active", BackPrices: []PriceSize{{1.72, 58000}, {1.70, 40000}, {1.68, 26000}}, LayPrices: []PriceSize{{1.74, 55000}, {1.76, 38000}, {1.78, 24000}}},
		},

		// ── Boxing Runners ──
		"box-fur-usy-mo": {
			{MarketID: "box-fur-usy-mo", SelectionID: 1601, Name: "Tyson Fury", Status: "active", BackPrices: []PriceSize{{2.50, 120000}, {2.48, 80000}, {2.46, 52000}}, LayPrices: []PriceSize{{2.54, 115000}, {2.56, 75000}, {2.58, 48000}}},
			{MarketID: "box-fur-usy-mo", SelectionID: 1602, Name: "Oleksandr Usyk", Status: "active", BackPrices: []PriceSize{{1.60, 150000}, {1.58, 100000}, {1.56, 65000}}, LayPrices: []PriceSize{{1.62, 145000}, {1.64, 95000}, {1.66, 60000}}},
			{MarketID: "box-fur-usy-mo", SelectionID: 1603, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{21.00, 8000}, {20.00, 5000}, {19.00, 3000}}, LayPrices: []PriceSize{{23.00, 6000}, {25.00, 4000}, {27.00, 2500}}},
		},
		"box-can-ben-mo": {
			{MarketID: "box-can-ben-mo", SelectionID: 1604, Name: "Canelo Alvarez", Status: "active", BackPrices: []PriceSize{{1.75, 110000}, {1.73, 75000}, {1.71, 50000}}, LayPrices: []PriceSize{{1.77, 105000}, {1.79, 70000}, {1.81, 45000}}},
			{MarketID: "box-can-ben-mo", SelectionID: 1605, Name: "David Benavidez", Status: "active", BackPrices: []PriceSize{{2.15, 90000}, {2.12, 60000}, {2.10, 40000}}, LayPrices: []PriceSize{{2.18, 85000}, {2.20, 55000}, {2.22, 38000}}},
			{MarketID: "box-can-ben-mo", SelectionID: 1606, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{26.00, 6000}, {25.00, 4000}, {24.00, 2500}}, LayPrices: []PriceSize{{28.00, 5000}, {30.00, 3500}, {32.00, 2000}}},
		},
		"box-cra-spe-mo": {
			{MarketID: "box-cra-spe-mo", SelectionID: 1607, Name: "Terence Crawford", Status: "active", BackPrices: []PriceSize{{1.50, 85000}, {1.48, 58000}, {1.46, 38000}}, LayPrices: []PriceSize{{1.52, 80000}, {1.54, 55000}, {1.56, 35000}}},
			{MarketID: "box-cra-spe-mo", SelectionID: 1608, Name: "Errol Spence Jr", Status: "active", BackPrices: []PriceSize{{2.70, 55000}, {2.66, 38000}, {2.62, 25000}}, LayPrices: []PriceSize{{2.74, 52000}, {2.78, 35000}, {2.82, 22000}}},
			{MarketID: "box-cra-spe-mo", SelectionID: 1609, Name: "The Draw", Status: "active", BackPrices: []PriceSize{{23.00, 5000}, {22.00, 3500}, {21.00, 2000}}, LayPrices: []PriceSize{{25.00, 4000}, {27.00, 3000}, {29.00, 1800}}},
		},

		// ── MMA Runners ──
		"mma-mcg-cha-mo": {
			{MarketID: "mma-mcg-cha-mo", SelectionID: 1701, Name: "Conor McGregor", Status: "active", BackPrices: []PriceSize{{2.40, 180000}, {2.38, 120000}, {2.36, 80000}}, LayPrices: []PriceSize{{2.44, 170000}, {2.46, 115000}, {2.48, 75000}}},
			{MarketID: "mma-mcg-cha-mo", SelectionID: 1702, Name: "Michael Chandler", Status: "active", BackPrices: []PriceSize{{1.62, 200000}, {1.60, 135000}, {1.58, 90000}}, LayPrices: []PriceSize{{1.64, 190000}, {1.66, 130000}, {1.68, 85000}}},
		},
		"mma-ade-dup-mo": {
			{MarketID: "mma-ade-dup-mo", SelectionID: 1703, Name: "Israel Adesanya", Status: "active", BackPrices: []PriceSize{{2.80, 70000}, {2.76, 48000}, {2.72, 32000}}, LayPrices: []PriceSize{{2.84, 65000}, {2.88, 45000}, {2.92, 30000}}},
			{MarketID: "mma-ade-dup-mo", SelectionID: 1704, Name: "Dricus Du Plessis", Status: "active", BackPrices: []PriceSize{{1.48, 95000}, {1.46, 65000}, {1.44, 42000}}, LayPrices: []PriceSize{{1.50, 90000}, {1.52, 62000}, {1.54, 40000}}},
		},
	}

	// ── Casino Games ────────────────────────────────────────────
	s.casinoGames = []*CasinoGame{
		// Live Casino
		{ID: "teen_patti", Name: "Teen Patti", Category: "live_casino", Provider: "evolution", MinBet: 100, MaxBet: 500000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Teen+Patti", StreamURL: "https://stream.evolution.com/game/teen_patti/live", IframeURL: "https://games.evolution.com/embed/teen_patti?token=DEMO", RTP: 96.63, Tags: []string{"popular", "live", "indian", "cards"}, Popular: true, New: false},
		{ID: "andar_bahar", Name: "Andar Bahar", Category: "live_casino", Provider: "ezugi", MinBet: 100, MaxBet: 500000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Andar+Bahar", StreamURL: "https://stream.ezugi.com/game/andar_bahar/live", IframeURL: "https://games.ezugi.com/embed/andar_bahar?token=DEMO", RTP: 97.85, Tags: []string{"popular", "live", "indian", "cards"}, Popular: true, New: false},
		{ID: "dragon_tiger", Name: "Dragon Tiger", Category: "live_casino", Provider: "evolution", MinBet: 50, MaxBet: 300000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Dragon+Tiger", StreamURL: "https://stream.evolution.com/game/dragon_tiger/live", IframeURL: "https://games.evolution.com/embed/dragon_tiger?token=DEMO", RTP: 96.27, Tags: []string{"popular", "live", "asian", "cards"}, Popular: true, New: false},
		{ID: "roulette", Name: "Roulette", Category: "live_casino", Provider: "evolution", MinBet: 10, MaxBet: 1000000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Roulette", StreamURL: "https://stream.evolution.com/game/roulette/live", IframeURL: "https://games.evolution.com/embed/roulette?token=DEMO", RTP: 97.30, Tags: []string{"popular", "live", "classic", "table"}, Popular: true, New: false},
		{ID: "baccarat", Name: "Baccarat", Category: "live_casino", Provider: "evolution", MinBet: 50, MaxBet: 500000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Baccarat", StreamURL: "https://stream.evolution.com/game/baccarat/live", IframeURL: "https://games.evolution.com/embed/baccarat?token=DEMO", RTP: 98.94, Tags: []string{"popular", "live", "classic", "cards"}, Popular: true, New: false},
		{ID: "blackjack", Name: "Blackjack", Category: "live_casino", Provider: "evolution", MinBet: 100, MaxBet: 500000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Blackjack", StreamURL: "https://stream.evolution.com/game/blackjack/live", IframeURL: "https://games.evolution.com/embed/blackjack?token=DEMO", RTP: 99.50, Tags: []string{"popular", "live", "classic", "cards"}, Popular: true, New: false},
		{ID: "poker", Name: "Casino Hold'em", Category: "live_casino", Provider: "evolution", MinBet: 100, MaxBet: 300000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Casino+Holdem", StreamURL: "https://stream.evolution.com/game/poker/live", IframeURL: "https://games.evolution.com/embed/poker?token=DEMO", RTP: 97.84, Tags: []string{"live", "classic", "cards", "poker"}, Popular: false, New: false},
		{ID: "lucky_7", Name: "Lucky 7", Category: "live_casino", Provider: "betgames", MinBet: 50, MaxBet: 200000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Lucky+7", StreamURL: "https://stream.betgames.com/game/lucky_7/live", IframeURL: "https://games.betgames.com/embed/lucky_7?token=DEMO", RTP: 95.80, Tags: []string{"live", "indian", "fast"}, Popular: false, New: false},
		{ID: "thirty_two_cards", Name: "32 Cards", Category: "live_casino", Provider: "superspade", MinBet: 100, MaxBet: 300000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=32+Cards", StreamURL: "https://stream.superspade.com/game/thirty_two_cards/live", IframeURL: "https://games.superspade.com/embed/thirty_two_cards?token=DEMO", RTP: 96.10, Tags: []string{"live", "indian", "cards"}, Popular: false, New: false},
		{ID: "hi_lo", Name: "Hi-Lo", Category: "live_casino", Provider: "evolution", MinBet: 10, MaxBet: 100000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Hi-Lo", StreamURL: "https://stream.evolution.com/game/hi_lo/live", IframeURL: "https://games.evolution.com/embed/hi_lo?token=DEMO", RTP: 96.58, Tags: []string{"live", "fast", "simple"}, Popular: false, New: false},
		{ID: "bollywood_casino", Name: "Bollywood Casino", Category: "live_casino", Provider: "superspade", MinBet: 100, MaxBet: 200000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Bollywood+Casino", StreamURL: "https://stream.superspade.com/game/bollywood_casino/live", IframeURL: "https://games.superspade.com/embed/bollywood_casino?token=DEMO", RTP: 95.50, Tags: []string{"live", "indian", "themed"}, Popular: false, New: false},
		{ID: "lightning_roulette", Name: "Lightning Roulette", Category: "live_casino", Provider: "evolution", MinBet: 20, MaxBet: 500000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Lightning+Roulette", StreamURL: "https://stream.evolution.com/game/lightning_roulette/live", IframeURL: "https://games.evolution.com/embed/lightning_roulette?token=DEMO", RTP: 97.30, Tags: []string{"popular", "live", "featured", "table"}, Popular: true, New: false},
		{ID: "crazy_time", Name: "Crazy Time", Category: "live_casino", Provider: "evolution", MinBet: 10, MaxBet: 250000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Crazy+Time", StreamURL: "https://stream.evolution.com/game/crazy_time/live", IframeURL: "https://games.evolution.com/embed/crazy_time?token=DEMO", RTP: 96.08, Tags: []string{"popular", "live", "gameshow", "featured"}, Popular: true, New: false},
		{ID: "dream_catcher", Name: "Dream Catcher", Category: "live_casino", Provider: "evolution", MinBet: 10, MaxBet: 250000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Dream+Catcher", StreamURL: "https://stream.evolution.com/game/dream_catcher/live", IframeURL: "https://games.evolution.com/embed/dream_catcher?token=DEMO", RTP: 96.58, Tags: []string{"live", "gameshow", "wheel"}, Popular: false, New: false},
		{ID: "monopoly_live", Name: "Monopoly Live", Category: "live_casino", Provider: "evolution", MinBet: 10, MaxBet: 250000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Monopoly+Live", StreamURL: "https://stream.evolution.com/game/monopoly_live/live", IframeURL: "https://games.evolution.com/embed/monopoly_live?token=DEMO", RTP: 96.23, Tags: []string{"live", "gameshow", "branded"}, Popular: false, New: true},
		{ID: "teen_patti_2020", Name: "Teen Patti 20-20", Category: "live_casino", Provider: "superspade", MinBet: 100, MaxBet: 200000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Teen+Patti+20-20", StreamURL: "https://stream.superspade.com/game/teen_patti_2020/live", IframeURL: "https://games.superspade.com/embed/teen_patti_2020?token=DEMO", RTP: 96.50, Tags: []string{"live", "indian", "cards", "fast"}, Popular: false, New: true},
		{ID: "instant_worli", Name: "Instant Worli", Category: "live_casino", Provider: "superspade", MinBet: 50, MaxBet: 100000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a1a2e/ffd700?text=Instant+Worli", StreamURL: "https://stream.superspade.com/game/instant_worli/live", IframeURL: "https://games.superspade.com/embed/instant_worli?token=DEMO", RTP: 95.20, Tags: []string{"live", "indian", "fast"}, Popular: false, New: true},

		// Virtual Sports
		{ID: "virtual_cricket", Name: "Virtual Cricket", Category: "virtual_sports", Provider: "betgames", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0d1b2a/00e5ff?text=Virtual+Cricket", StreamURL: "https://stream.betgames.com/game/virtual_cricket/live", IframeURL: "https://games.betgames.com/embed/virtual_cricket?token=DEMO", RTP: 95.00, Tags: []string{"virtual", "sports", "indian", "cricket"}, Popular: true, New: false},
		{ID: "virtual_football", Name: "Virtual Football", Category: "virtual_sports", Provider: "betgames", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0d1b2a/00e5ff?text=Virtual+Football", StreamURL: "https://stream.betgames.com/game/virtual_football/live", IframeURL: "https://games.betgames.com/embed/virtual_football?token=DEMO", RTP: 95.00, Tags: []string{"virtual", "sports", "football"}, Popular: false, New: false},
		{ID: "virtual_tennis", Name: "Virtual Tennis", Category: "virtual_sports", Provider: "betgames", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0d1b2a/00e5ff?text=Virtual+Tennis", StreamURL: "https://stream.betgames.com/game/virtual_tennis/live", IframeURL: "https://games.betgames.com/embed/virtual_tennis?token=DEMO", RTP: 95.50, Tags: []string{"virtual", "sports", "tennis"}, Popular: false, New: false},
		{ID: "virtual_horse_racing", Name: "Virtual Horse Racing", Category: "virtual_sports", Provider: "betgames", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0d1b2a/00e5ff?text=Virtual+Horse+Racing", StreamURL: "https://stream.betgames.com/game/virtual_horse_racing/live", IframeURL: "https://games.betgames.com/embed/virtual_horse_racing?token=DEMO", RTP: 95.00, Tags: []string{"virtual", "sports", "racing", "horses"}, Popular: false, New: true},

		// Slots
		{ID: "slots_classic", Name: "Classic Slots", Category: "slots", Provider: "pragmatic_play", MinBet: 1, MaxBet: 10000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/2d1b69/ff6b6b?text=Classic+Slots", StreamURL: "", IframeURL: "https://games.pragmaticplay.com/embed/slots_classic?token=DEMO", RTP: 96.50, Tags: []string{"slots", "classic", "retro"}, Popular: false, New: false},
		{ID: "slots_video", Name: "Video Slots", Category: "slots", Provider: "pragmatic_play", MinBet: 1, MaxBet: 10000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/2d1b69/ff6b6b?text=Video+Slots", StreamURL: "", IframeURL: "https://games.pragmaticplay.com/embed/slots_video?token=DEMO", RTP: 96.71, Tags: []string{"slots", "video", "bonus"}, Popular: false, New: false},
		{ID: "slots_megaways", Name: "Megaways Slots", Category: "slots", Provider: "pragmatic_play", MinBet: 5, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/2d1b69/ff6b6b?text=Megaways+Slots", StreamURL: "", IframeURL: "https://games.pragmaticplay.com/embed/slots_megaways?token=DEMO", RTP: 97.00, Tags: []string{"slots", "megaways", "popular", "high-variance"}, Popular: true, New: true},

		// Crash Games
		{ID: "aviator", Name: "Aviator", Category: "crash_games", Provider: "pragmatic_play", MinBet: 10, MaxBet: 100000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0a0a23/ff4444?text=Aviator", StreamURL: "https://stream.pragmaticplay.com/game/aviator/live", IframeURL: "https://games.pragmaticplay.com/embed/aviator?token=DEMO", RTP: 97.00, Tags: []string{"popular", "crash", "trending", "fast"}, Popular: true, New: false},
		{ID: "spaceman", Name: "Spaceman", Category: "crash_games", Provider: "pragmatic_play", MinBet: 10, MaxBet: 100000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0a0a23/ff4444?text=Spaceman", StreamURL: "https://stream.pragmaticplay.com/game/spaceman/live", IframeURL: "https://games.pragmaticplay.com/embed/spaceman?token=DEMO", RTP: 96.50, Tags: []string{"crash", "space", "trending"}, Popular: false, New: true},
		{ID: "mines", Name: "Mines", Category: "crash_games", Provider: "pragmatic_play", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0a0a23/ff4444?text=Mines", StreamURL: "", IframeURL: "https://games.pragmaticplay.com/embed/mines?token=DEMO", RTP: 97.00, Tags: []string{"crash", "strategy", "instant"}, Popular: false, New: false},
		{ID: "plinko", Name: "Plinko", Category: "crash_games", Provider: "pragmatic_play", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/0a0a23/ff4444?text=Plinko", StreamURL: "", IframeURL: "https://games.pragmaticplay.com/embed/plinko?token=DEMO", RTP: 97.00, Tags: []string{"crash", "fun", "instant"}, Popular: false, New: true},

		// Card Games
		{ID: "worli_matka", Name: "Worli Matka", Category: "card_games", Provider: "superspade", MinBet: 10, MaxBet: 50000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a0a2e/ffaa00?text=Worli+Matka", StreamURL: "https://stream.superspade.com/game/worli_matka/live", IframeURL: "https://games.superspade.com/embed/worli_matka?token=DEMO", RTP: 95.00, Tags: []string{"indian", "traditional", "numbers"}, Popular: true, New: false},
		{ID: "lottery", Name: "Lottery", Category: "card_games", Provider: "betgames", MinBet: 10, MaxBet: 10000, Active: true,
			Thumbnail: "https://via.placeholder.com/300x200/1a0a2e/ffaa00?text=Lottery", StreamURL: "https://stream.betgames.com/game/lottery/live", IframeURL: "https://games.betgames.com/embed/lottery?token=DEMO", RTP: 95.50, Tags: []string{"numbers", "draw", "simple"}, Popular: false, New: false},
	}
}

// ─── Live odds fluctuation ──────────────────────────────────────────────────

// StartOddsFluctuation launches a background goroutine that randomly drifts
// prices and sizes every 2-3 seconds, simulating a live exchange where odds
// blink and change in real time.
func (s *Store) StartOddsFluctuation(stop <-chan struct{}) {
	go func() {
		for {
			// Random interval between 2 and 3 seconds
			delay := time.Duration(2000+rand.Intn(1000)) * time.Millisecond
			select {
			case <-stop:
				return
			case <-time.After(delay):
			}

			s.mu.Lock()
			for marketID, runners := range s.runners {
				// Only fluctuate markets that are open and in-play
				m, ok := s.markets[marketID]
				if !ok || m.Status != "open" {
					continue
				}

				for _, runner := range runners {
					if runner.Status != "active" {
						continue
					}
					fluctuateRunner(runner)
				}

				// Also drift the TotalMatched slightly upwards
				if m.InPlay {
					m.TotalMatched += float64(rand.Intn(5000)) + 500
				}
			}
			s.mu.Unlock()
		}
	}()
}

// fluctuateRunner randomly adjusts prices by +/- 0.01 to 0.05 and sizes by
// +/- 5% to 20%.  This keeps the back/lay spread valid (lay >= back).
func fluctuateRunner(r *Runner) {
	// Fluctuate back prices
	for i := range r.BackPrices {
		r.BackPrices[i].Price = driftPrice(r.BackPrices[i].Price)
		r.BackPrices[i].Size = driftSize(r.BackPrices[i].Size)
	}

	// Fluctuate lay prices
	for i := range r.LayPrices {
		r.LayPrices[i].Price = driftPrice(r.LayPrices[i].Price)
		r.LayPrices[i].Size = driftSize(r.LayPrices[i].Size)
	}

	// Ensure lay >= back to maintain a valid spread
	if len(r.BackPrices) > 0 && len(r.LayPrices) > 0 {
		bestBack := r.BackPrices[0].Price
		bestLay := r.LayPrices[0].Price
		if bestLay <= bestBack {
			r.LayPrices[0].Price = bestBack + 0.01 + rand.Float64()*0.02
			r.LayPrices[0].Price = math.Round(r.LayPrices[0].Price*100) / 100
		}
	}

	// Ensure back prices are in descending order and lay prices in ascending order
	for i := 1; i < len(r.BackPrices); i++ {
		if r.BackPrices[i].Price >= r.BackPrices[i-1].Price {
			r.BackPrices[i].Price = r.BackPrices[i-1].Price - 0.01
		}
	}
	for i := 1; i < len(r.LayPrices); i++ {
		if r.LayPrices[i].Price <= r.LayPrices[i-1].Price {
			r.LayPrices[i].Price = r.LayPrices[i-1].Price + 0.01
		}
	}

	// Ensure all prices stay above the minimum 1.01
	for i := range r.BackPrices {
		if r.BackPrices[i].Price < 1.01 {
			r.BackPrices[i].Price = 1.01
		}
		r.BackPrices[i].Price = math.Round(r.BackPrices[i].Price*100) / 100
	}
	for i := range r.LayPrices {
		if r.LayPrices[i].Price < 1.02 {
			r.LayPrices[i].Price = 1.02
		}
		r.LayPrices[i].Price = math.Round(r.LayPrices[i].Price*100) / 100
	}

	// Fluctuate fancy market YES/NO rates
	if r.YesRate > 0 {
		drift := float64(rand.Intn(7) - 3) // -3 to +3
		r.YesRate = math.Max(50, math.Min(120, r.YesRate+drift))
		r.YesRate = math.Round(r.YesRate)
	}
	if r.NoRate > 0 {
		drift := float64(rand.Intn(7) - 3) // -3 to +3
		r.NoRate = math.Max(50, math.Min(120, r.NoRate+drift))
		r.NoRate = math.Round(r.NoRate)
		// Ensure NoRate > YesRate (valid spread)
		if r.YesRate > 0 && r.NoRate <= r.YesRate {
			r.NoRate = r.YesRate + float64(rand.Intn(3)) + 2
		}
	}
}

// driftPrice shifts a price by a random amount between -0.05 and +0.05.
func driftPrice(price float64) float64 {
	// Drift: -0.05 to +0.05
	drift := (rand.Float64() - 0.5) * 0.10
	newPrice := price + drift
	if newPrice < 1.01 {
		newPrice = 1.01
	}
	return math.Round(newPrice*100) / 100
}

// driftSize changes a size by +/- 5% to 20%.
func driftSize(size float64) float64 {
	// Factor between 0.80 and 1.20
	factor := 0.80 + rand.Float64()*0.40
	newSize := size * factor
	if newSize < 100 {
		newSize = 100
	}
	return math.Round(newSize)
}

// ─── Live Score Simulation ──────────────────────────────────────────────────

// StartScoreSimulation simulates a live cricket match for MI vs CSK,
// advancing runs, wickets, and overs every ~10 seconds.
func (s *Store) StartScoreSimulation(stop <-chan struct{}) {
	// Initialize scores for in-play cricket matches
	s.mu.Lock()
	s.liveScores["ipl-mi-csk"] = &LiveScoreData{
		EventID:     "ipl-mi-csk",
		Home:        "Mumbai Indians",
		Away:        "Chennai Super Kings",
		HomeScore:   "87/2",
		AwayScore:   "",
		Overs:       "12.3",
		RunRate:     "6.96",
		LastWicket:  "S Yadav c Jadeja b Chahar 24(18)",
		Partnership: "32(26)",
	}
	s.mu.Unlock()

	go func() {
		runs := 87
		wickets := 2
		balls := 75 // 12.3 overs = 12*6+3 = 75 balls
		partnerRuns := 32
		partnerBalls := 26
		lastWicket := "S Yadav c Jadeja b Chahar 24(18)"

		batsmen := []string{"R Sharma", "I Kishan", "S Yadav", "H Pandya", "T David", "K Pollard", "J Bumrah", "P Chawla", "J Archer", "R Chahar"}
		bowlers := []string{"D Chahar", "R Jadeja", "M Theekshana", "T Curran", "M Ali"}

		for {
			delay := time.Duration(8000+rand.Intn(4000)) * time.Millisecond
			select {
			case <-stop:
				return
			case <-time.After(delay):
			}

			// Simulate a delivery
			balls++
			partnerBalls++

			// Random runs: 0-6
			delivery := rand.Intn(10)
			var scored int
			switch {
			case delivery < 3:
				scored = 0 // dot ball
			case delivery < 5:
				scored = 1
			case delivery < 7:
				scored = 2
			case delivery < 8:
				scored = 4 // boundary
			case delivery < 9:
				scored = 6 // six
			default:
				scored = 3
			}

			runs += scored
			partnerRuns += scored

			// Wicket chance ~5%
			if rand.Intn(20) == 0 && wickets < 9 {
				wickets++
				bowlerName := bowlers[rand.Intn(len(bowlers))]
				batsmanName := batsmen[wickets]
				lastWicket = fmt.Sprintf("%s b %s %d(%d)", batsmanName, bowlerName, partnerRuns, partnerBalls)
				partnerRuns = 0
				partnerBalls = 0
			}

			overs := balls / 6
			ballsInOver := balls % 6
			oversStr := fmt.Sprintf("%d.%d", overs, ballsInOver)
			runRate := 0.0
			if overs > 0 || ballsInOver > 0 {
				totalOvers := float64(overs) + float64(ballsInOver)/6.0
				runRate = float64(runs) / totalOvers
			}

			// Cap at 20 overs
			if overs >= 20 {
				balls = 120
				oversStr = "20.0"
			}

			s.mu.Lock()
			score := s.liveScores["ipl-mi-csk"]
			if score != nil {
				score.HomeScore = fmt.Sprintf("%d/%d", runs, wickets)
				score.Overs = oversStr
				score.RunRate = fmt.Sprintf("%.2f", runRate)
				score.Partnership = fmt.Sprintf("%d(%d)", partnerRuns, partnerBalls)
				score.LastWicket = lastWicket
				if overs >= 6 {
					// After powerplay, add required rate for a target of ~180
					target := 180
					remaining := target - runs
					remainingOvers := 20.0 - (float64(overs) + float64(ballsInOver)/6.0)
					if remainingOvers > 0 {
						score.RequiredRate = fmt.Sprintf("%.2f", float64(remaining)/remainingOvers)
					}
				}
			}
			s.mu.Unlock()

			// Stop advancing after 20 overs
			if overs >= 20 {
				return
			}
		}
	}()
}
