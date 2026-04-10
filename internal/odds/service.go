package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
)

// NATSPublisher is the interface for publishing messages to NATS.
// Implemented by pkg/nats.Client.
type NATSPublisher interface {
	Publish(ctx context.Context, subject string, data []byte) error
}

// Service manages odds providers and caches updates in Redis.
type Service struct {
	provider OddsProvider
	redis    *redis.Client
	logger   *slog.Logger
	nats     NATSPublisher // optional; when set, publishes each update to NATS once
}

func NewService(provider OddsProvider, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{
		provider: provider,
		redis:    rdb,
		logger:   logger,
	}
}

// SetNATS configures the NATS publisher so the service publishes each odds
// update exactly once, rather than per-client.
func (s *Service) SetNATS(n NATSPublisher) {
	s.nats = n
}

func (s *Service) Provider() OddsProvider { return s.provider }

// ---------------------------------------------------------------------------
// Market queries
// ---------------------------------------------------------------------------

func (s *Service) FetchMarkets(ctx context.Context, sport string) ([]*models.Market, error) {
	markets, err := s.provider.FetchMarkets(ctx, sport)
	if err != nil {
		return nil, fmt.Errorf("fetch markets from %s: %w", s.provider.Name(), err)
	}

	// Cache each market in Redis
	for _, m := range markets {
		data, _ := json.Marshal(m)
		s.redis.Set(ctx, fmt.Sprintf("market:%s", m.ID), data, 60*time.Second)
	}

	return markets, nil
}

func (s *Service) GetCachedMarket(ctx context.Context, marketID string) (*models.Market, error) {
	data, err := s.redis.Get(ctx, fmt.Sprintf("market:%s", marketID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("market not cached: %w", err)
	}

	var market models.Market
	if err := json.Unmarshal(data, &market); err != nil {
		return nil, fmt.Errorf("unmarshal market: %w", err)
	}
	return &market, nil
}

// ---------------------------------------------------------------------------
// Competition & event queries
// ---------------------------------------------------------------------------

func (s *Service) FetchCompetitions(ctx context.Context, sport string) ([]*models.Competition, error) {
	comps, err := s.provider.FetchCompetitions(ctx, sport)
	if err != nil {
		return nil, fmt.Errorf("fetch competitions from %s: %w", s.provider.Name(), err)
	}

	// Cache competitions
	for _, c := range comps {
		data, _ := json.Marshal(c)
		s.redis.Set(ctx, fmt.Sprintf("competition:%s", c.ID), data, 5*time.Minute)
	}

	return comps, nil
}

func (s *Service) FetchEvents(ctx context.Context, competitionID string) ([]*models.Event, error) {
	events, err := s.provider.FetchEvents(ctx, competitionID)
	if err != nil {
		return nil, fmt.Errorf("fetch events from %s: %w", s.provider.Name(), err)
	}

	// Cache events
	for _, e := range events {
		data, _ := json.Marshal(e)
		s.redis.Set(ctx, fmt.Sprintf("event:%s", e.ID), data, 2*time.Minute)
	}

	return events, nil
}

func (s *Service) FetchMarketsByEvent(ctx context.Context, eventID string) ([]*models.Market, error) {
	markets, err := s.provider.FetchMarketsByEvent(ctx, eventID)
	if err != nil {
		return nil, fmt.Errorf("fetch markets by event from %s: %w", s.provider.Name(), err)
	}

	// Cache each market
	for _, m := range markets {
		data, _ := json.Marshal(m)
		s.redis.Set(ctx, fmt.Sprintf("market:%s", m.ID), data, 60*time.Second)
	}

	return markets, nil
}

func (s *Service) GetCachedEvent(ctx context.Context, eventID string) (*models.Event, error) {
	data, err := s.redis.Get(ctx, fmt.Sprintf("event:%s", eventID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("event not cached: %w", err)
	}

	var event models.Event
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, fmt.Errorf("unmarshal event: %w", err)
	}
	return &event, nil
}

func (s *Service) GetCachedCompetition(ctx context.Context, competitionID string) (*models.Competition, error) {
	data, err := s.redis.Get(ctx, fmt.Sprintf("competition:%s", competitionID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("competition not cached: %w", err)
	}

	var comp models.Competition
	if err := json.Unmarshal(data, &comp); err != nil {
		return nil, fmt.Errorf("unmarshal competition: %w", err)
	}
	return &comp, nil
}

// ---------------------------------------------------------------------------
// Streaming
// ---------------------------------------------------------------------------

func (s *Service) StartSubscription(ctx context.Context, marketIDs []string) (<-chan *models.OddsUpdate, error) {
	updates := make(chan *models.OddsUpdate, 1000)

	go func() {
		defer close(updates)
		if err := s.provider.Subscribe(ctx, marketIDs, updates); err != nil {
			s.logger.ErrorContext(ctx, "subscription ended", "error", err)
		}
	}()

	// Cache updates as they come in; warn and drop if consumer is too slow.
	cached := make(chan *models.OddsUpdate, 1000)
	go func() {
		defer close(cached)
		for update := range updates {
			// Cache latest odds
			data, _ := json.Marshal(update)
			s.redis.Set(ctx, fmt.Sprintf("odds:market:%s", update.MarketID), data, 30*time.Second)

			// Publish to NATS once (not per-client).
			if s.nats != nil {
				if pubErr := s.nats.Publish(ctx, "odds.update."+update.MarketID, data); pubErr != nil {
					s.logger.ErrorContext(ctx, "failed to publish odds update to NATS",
						"market_id", update.MarketID, "error", pubErr)
				}
			}

			select {
			case cached <- update:
			case <-ctx.Done():
				return
			default:
				s.logger.WarnContext(ctx, "subscription channel full, dropping update for slow consumer",
					"market_id", update.MarketID)
			}
		}
	}()

	return cached, nil
}

func (s *Service) GetLatestOdds(ctx context.Context, marketID string) (*models.OddsUpdate, error) {
	data, err := s.redis.Get(ctx, fmt.Sprintf("odds:market:%s", marketID)).Bytes()
	if err != nil {
		return nil, fmt.Errorf("no cached odds: %w", err)
	}

	var update models.OddsUpdate
	if err := json.Unmarshal(data, &update); err != nil {
		return nil, fmt.Errorf("unmarshal odds: %w", err)
	}
	return &update, nil
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (s *Service) HealthCheck(ctx context.Context) error {
	return s.provider.HealthCheck(ctx)
}
