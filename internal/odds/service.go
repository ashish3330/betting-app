package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
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

	// sf coalesces concurrent upstream fetches for the same key so that
	// only one request reaches the provider during a cache-miss stampede.
	// The rest of the callers block on the in-flight request and share
	// the result. This is the classic cache-stampede / thundering-herd
	// fix and is especially valuable for the mock/Entity Sports provider
	// during odds TTL expiry on hot markets.
	sf singleflight.Group
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
	// Singleflight coalesces concurrent cache-miss fetches for the same
	// sport so we only hit the upstream provider once per wave.
	v, err, _ := s.sf.Do("markets:"+sport, func() (interface{}, error) {
		markets, err := s.provider.FetchMarkets(ctx, sport)
		if err != nil {
			return nil, fmt.Errorf("fetch markets from %s: %w", s.provider.Name(), err)
		}

		// Cache each market in Redis with a single pipelined round-trip
		// instead of N separate SET RPCs.
		if len(markets) > 0 {
			pipe := s.redis.Pipeline()
			for _, m := range markets {
				data, _ := json.Marshal(m)
				pipe.Set(ctx, fmt.Sprintf("market:%s", m.ID), data, 60*time.Second)
			}
			if _, perr := pipe.Exec(ctx); perr != nil {
				s.logger.WarnContext(ctx, "odds redis pipeline failed",
					"op", "FetchMarkets", "sport", sport, "error", perr)
			}
		}

		return markets, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]*models.Market), nil
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
	v, err, _ := s.sf.Do("competitions:"+sport, func() (interface{}, error) {
		comps, err := s.provider.FetchCompetitions(ctx, sport)
		if err != nil {
			return nil, fmt.Errorf("fetch competitions from %s: %w", s.provider.Name(), err)
		}

		// Pipelined cache writes — one round-trip instead of N.
		if len(comps) > 0 {
			pipe := s.redis.Pipeline()
			for _, c := range comps {
				data, _ := json.Marshal(c)
				pipe.Set(ctx, fmt.Sprintf("competition:%s", c.ID), data, 5*time.Minute)
			}
			if _, perr := pipe.Exec(ctx); perr != nil {
				s.logger.WarnContext(ctx, "odds redis pipeline failed",
					"op", "FetchCompetitions", "sport", sport, "error", perr)
			}
		}

		return comps, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]*models.Competition), nil
}

func (s *Service) FetchEvents(ctx context.Context, competitionID string) ([]*models.Event, error) {
	v, err, _ := s.sf.Do("events:"+competitionID, func() (interface{}, error) {
		events, err := s.provider.FetchEvents(ctx, competitionID)
		if err != nil {
			return nil, fmt.Errorf("fetch events from %s: %w", s.provider.Name(), err)
		}

		// Pipelined cache writes — one round-trip instead of N.
		if len(events) > 0 {
			pipe := s.redis.Pipeline()
			for _, e := range events {
				data, _ := json.Marshal(e)
				pipe.Set(ctx, fmt.Sprintf("event:%s", e.ID), data, 2*time.Minute)
			}
			if _, perr := pipe.Exec(ctx); perr != nil {
				s.logger.WarnContext(ctx, "odds redis pipeline failed",
					"op", "FetchEvents", "competition", competitionID, "error", perr)
			}
		}

		return events, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]*models.Event), nil
}

func (s *Service) FetchMarketsByEvent(ctx context.Context, eventID string) ([]*models.Market, error) {
	v, err, _ := s.sf.Do("markets-by-event:"+eventID, func() (interface{}, error) {
		markets, err := s.provider.FetchMarketsByEvent(ctx, eventID)
		if err != nil {
			return nil, fmt.Errorf("fetch markets by event from %s: %w", s.provider.Name(), err)
		}

		// Pipelined cache writes — one round-trip instead of N.
		if len(markets) > 0 {
			pipe := s.redis.Pipeline()
			for _, m := range markets {
				data, _ := json.Marshal(m)
				pipe.Set(ctx, fmt.Sprintf("market:%s", m.ID), data, 60*time.Second)
			}
			if _, perr := pipe.Exec(ctx); perr != nil {
				s.logger.WarnContext(ctx, "odds redis pipeline failed",
					"op", "FetchMarketsByEvent", "event", eventID, "error", perr)
			}
		}

		return markets, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]*models.Market), nil
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
	// Singleflight guards against concurrent Redis lookups for the same
	// market during a cache miss: a storm of clients all hitting the
	// same hot market will collapse onto one Redis GET + Unmarshal.
	v, err, _ := s.sf.Do("odds:"+marketID, func() (interface{}, error) {
		data, err := s.redis.Get(ctx, fmt.Sprintf("odds:market:%s", marketID)).Bytes()
		if err != nil {
			return nil, fmt.Errorf("no cached odds: %w", err)
		}

		var update models.OddsUpdate
		if err := json.Unmarshal(data, &update); err != nil {
			return nil, fmt.Errorf("unmarshal odds: %w", err)
		}
		return &update, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*models.OddsUpdate), nil
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (s *Service) HealthCheck(ctx context.Context) error {
	return s.provider.HealthCheck(ctx)
}
