package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/lotus-exchange/lotus-exchange/pkg/circuit"
	"github.com/sony/gobreaker/v2"
)

// EntitySportsProvider implements OddsProvider for Entity Sports Cricket API.
type EntitySportsProvider struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	breaker    *gobreaker.CircuitBreaker[[]byte]
	logger     *slog.Logger
}

func NewEntitySportsProvider(apiKey, baseURL string, logger *slog.Logger) *EntitySportsProvider {
	return &EntitySportsProvider{
		apiKey:  apiKey,
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		breaker: circuit.NewBytes(circuit.Settings{
			Name:                      "entity_sports",
			MaxRequests:               3,
			Interval:                  60 * time.Second,
			Timeout:                   30 * time.Second,
			ConsecutiveFailuresToTrip: 5,
		}, logger),
		logger: logger,
	}
}

// doRequest executes an HTTP GET through the circuit breaker and returns the
// response body. When the breaker is open, calls fail fast without touching
// the network, which keeps us from hammering a degraded upstream.
func (e *EntitySportsProvider) doRequest(ctx context.Context, url string) ([]byte, error) {
	return e.breaker.Execute(func() ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		resp, err := e.httpClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("http do: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 500 {
			return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10 MB limit
		if err != nil {
			return nil, fmt.Errorf("read response: %w", err)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("upstream status %d: %s", resp.StatusCode, string(body))
		}
		return body, nil
	})
}

func (e *EntitySportsProvider) Name() string { return "entity_sports" }

type esMatchResponse struct {
	Status  string `json:"status"`
	Response struct {
		Items []esMatch `json:"items"`
	} `json:"response"`
}

type esMatch struct {
	MatchID    int    `json:"match_id"`
	Title      string `json:"title"`
	Status     int    `json:"status"`
	StatusStr  string `json:"status_str"`
	Competition struct {
		Title string `json:"title"`
	} `json:"competition"`
	TeamA struct {
		Name string `json:"name"`
	} `json:"teama"`
	TeamB struct {
		Name string `json:"name"`
	} `json:"teamb"`
	DateStart string `json:"date_start"`
}

func (e *EntitySportsProvider) FetchMarkets(ctx context.Context, sport string) ([]*models.Market, error) {
	if sport != string(models.SportCricket) && sport != "" {
		return nil, nil
	}

	url := fmt.Sprintf("%s/matches?status=3&token=%s&per_page=50", e.baseURL, e.apiKey)
	body, err := e.doRequest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("fetch matches: %w", err)
	}

	var esResp esMatchResponse
	if err := json.Unmarshal(body, &esResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var markets []*models.Market
	for _, match := range esResp.Response.Items {
		startTime, _ := time.Parse("2006-01-02 15:04:05", match.DateStart)

		m := &models.Market{
			ID:         fmt.Sprintf("es-%d-mo", match.MatchID),
			EventID:    fmt.Sprintf("es-%d", match.MatchID),
			Sport:      models.SportCricket,
			Name:       fmt.Sprintf("%s - Match Odds", match.Title),
			MarketType: models.MarketTypeMatchOdds,
			Status:     models.MarketOpen,
			InPlay:     match.Status == 3,
			StartTime:  startTime,
			Runners: []models.Runner{
				{SelectionID: int64(match.MatchID*10 + 1), Name: match.TeamA.Name, Status: "active"},
				{SelectionID: int64(match.MatchID*10 + 2), Name: match.TeamB.Name, Status: "active"},
				{SelectionID: int64(match.MatchID*10 + 3), Name: "The Draw", Status: "active"},
			},
		}
		markets = append(markets, m)
	}

	e.logger.InfoContext(ctx, "fetched Entity Sports markets", "count", len(markets))
	return markets, nil
}

func (e *EntitySportsProvider) Subscribe(ctx context.Context, marketIDs []string, updates chan<- *models.OddsUpdate) error {
	// Poll-based subscription with concurrent fetching per market.
	// Each market gets its own goroutine with independent backoff.
	var wg sync.WaitGroup

	for _, mid := range marketIDs {
		wg.Add(1)
		go func(marketID string) {
			defer wg.Done()

			ticker := time.NewTicker(2 * time.Second)
			defer ticker.Stop()

			backoff := time.Second
			maxBackoff := 30 * time.Second

			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					update, err := e.fetchLiveOdds(ctx, marketID)
					if err != nil {
						e.logger.WarnContext(ctx, "fetch odds failed, backing off",
							"market", marketID, "error", err, "backoff", backoff)
						select {
						case <-time.After(backoff):
						case <-ctx.Done():
							return
						}
						backoff = min(backoff*2, maxBackoff)
						continue
					}
					backoff = time.Second // reset on success

					select {
					case updates <- update:
					case <-ctx.Done():
						return
					}
				}
			}
		}(mid)
	}

	wg.Wait()
	return ctx.Err()
}

func (e *EntitySportsProvider) fetchLiveOdds(ctx context.Context, marketID string) (*models.OddsUpdate, error) {
	// Extract match ID from market ID (format: es-{matchID}-mo)
	var matchID int
	_, _ = fmt.Sscanf(marketID, "es-%d-mo", &matchID)

	url := fmt.Sprintf("%s/matches/%d/scorecard?token=%s", e.baseURL, matchID, e.apiKey)
	if _, err := e.doRequest(ctx, url); err != nil {
		return nil, err
	}

	// Parse and normalize to internal OddsUpdate format
	// In production this would parse Entity Sports specific response format
	return &models.OddsUpdate{
		MarketID:  marketID,
		Timestamp: time.Now(),
	}, nil
}

// FetchCompetitions returns competitions from Entity Sports.
// Currently only supports cricket; other sports return empty.
func (e *EntitySportsProvider) FetchCompetitions(ctx context.Context, sport string) ([]*models.Competition, error) {
	if sport != string(models.SportCricket) && sport != "" {
		return nil, nil
	}
	// Entity Sports competition endpoint
	url := fmt.Sprintf("%s/competitions?status=1&token=%s", e.baseURL, e.apiKey)
	if _, err := e.doRequest(ctx, url); err != nil {
		return nil, fmt.Errorf("fetch competitions: %w", err)
	}

	// Placeholder: in production, parse Entity Sports response into models.Competition
	e.logger.InfoContext(ctx, "fetched Entity Sports competitions", "sport", sport)
	return nil, nil
}

// FetchEvents returns events for a competition from Entity Sports.
func (e *EntitySportsProvider) FetchEvents(ctx context.Context, competitionID string) ([]*models.Event, error) {
	url := fmt.Sprintf("%s/competitions/%s/matches?token=%s&per_page=50", e.baseURL, competitionID, e.apiKey)
	if _, err := e.doRequest(ctx, url); err != nil {
		return nil, fmt.Errorf("fetch events: %w", err)
	}

	// Placeholder: in production, parse Entity Sports response into models.Event
	e.logger.InfoContext(ctx, "fetched Entity Sports events", "competition", competitionID)
	return nil, nil
}

// FetchMarketsByEvent returns markets for a specific event from Entity Sports.
func (e *EntitySportsProvider) FetchMarketsByEvent(ctx context.Context, eventID string) ([]*models.Market, error) {
	var matchID int
	_, _ = fmt.Sscanf(eventID, "es-%d", &matchID)

	url := fmt.Sprintf("%s/matches/%d/odds?token=%s", e.baseURL, matchID, e.apiKey)
	if _, err := e.doRequest(ctx, url); err != nil {
		return nil, fmt.Errorf("fetch event markets: %w", err)
	}

	// Placeholder: in production, parse Entity Sports response into models.Market
	e.logger.InfoContext(ctx, "fetched Entity Sports event markets", "event", eventID)
	return nil, nil
}

func (e *EntitySportsProvider) HealthCheck(ctx context.Context) error {
	url := fmt.Sprintf("%s/matches?status=3&token=%s&per_page=1", e.baseURL, e.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check: status %d", resp.StatusCode)
	}
	return nil
}

func (e *EntitySportsProvider) Close() error {
	e.httpClient.CloseIdleConnections()
	return nil
}
