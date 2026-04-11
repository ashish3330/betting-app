package odds

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
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

	// statusMu guards the seed-status counters below. The counters are
	// written once by SeedMockMarkets at startup and read by GetStatus on
	// every /api/v1/odds/status request, so a plain RWMutex is enough.
	statusMu       sync.RWMutex
	marketsLoaded  int
	runnersLoaded  int
	inPlayCount    int
	lastRefresh    time.Time
}

// Status is the payload returned by GET /api/v1/odds/status. It summarises
// the odds-service's current view of the world: which provider is active,
// how many markets/runners are currently cached, the last time the cache
// was refreshed, and how many of those markets are in-play right now.
type Status struct {
	Provider      string    `json:"provider"`
	MarketsLoaded int       `json:"markets_loaded"`
	RunnersLoaded int       `json:"runners_loaded"`
	LastRefresh   time.Time `json:"last_refresh"`
	InPlayCount   int       `json:"in_play_count"`
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
// Seeding & status
// ---------------------------------------------------------------------------

// SeedMockMarkets pulls every mock market the configured provider knows about
// (across all sports) and writes them into Redis under both `market:{id}` and
// `odds:market:{id}` so that
//
//   - GET /api/v1/markets/{id}/odds returns live prices on a cold cache, and
//   - the matching-engine can immediately accept bets on those markets, and
//   - GET /api/v1/odds/status reports accurate counts.
//
// The TTL is long (5 minutes) so the seeded entries survive across test runs
// and don't disappear during the brief window between startup and the first
// live subscription. The entries are refreshed opportunistically as clients
// hit the normal FetchMarkets / Subscribe paths.
//
// This is intentionally idempotent: calling it repeatedly just overwrites
// the same keys with fresh data. It returns the counts it wrote so callers
// can log a summary at startup.
func (s *Service) SeedMockMarkets(ctx context.Context) (markets, runners int, err error) {
	// Pull every market the provider knows about (sport="" means "all").
	all, err := s.provider.FetchMarkets(ctx, "")
	if err != nil {
		return 0, 0, fmt.Errorf("seed: fetch markets: %w", err)
	}

	const seedTTL = 5 * time.Minute

	pipe := s.redis.Pipeline()
	inPlay := 0
	for _, m := range all {
		if m == nil {
			continue
		}

		// 1. Cache the market document itself so that downstream services
		//    can look it up by ID without hammering the provider.
		marketData, mErr := json.Marshal(m)
		if mErr != nil {
			s.logger.WarnContext(ctx, "seed: marshal market failed", "market_id", m.ID, "error", mErr)
			continue
		}
		pipe.Set(ctx, fmt.Sprintf("market:%s", m.ID), marketData, seedTTL)

		// 2. Cache an OddsUpdate so GetLatestOdds(marketID) returns prices
		//    immediately instead of 404'ing on cold starts. We copy the
		//    runners straight from the market snapshot — the mock provider
		//    already populates back/lay ladders on every market it returns.
		update := models.OddsUpdate{
			MarketID:  m.ID,
			Runners:   m.Runners,
			Timestamp: time.Now(),
		}
		updateData, uErr := json.Marshal(update)
		if uErr != nil {
			s.logger.WarnContext(ctx, "seed: marshal odds update failed", "market_id", m.ID, "error", uErr)
			continue
		}
		pipe.Set(ctx, fmt.Sprintf("odds:market:%s", m.ID), updateData, seedTTL)

		markets++
		runners += len(m.Runners)
		if m.InPlay {
			inPlay++
		}
	}

	if _, pErr := pipe.Exec(ctx); pErr != nil {
		// Redis pipeline failures are logged but not fatal: the service can
		// still run, clients will fall through to provider fetches on demand.
		s.logger.WarnContext(ctx, "seed: redis pipeline failed", "error", pErr)
		return markets, runners, fmt.Errorf("seed: redis pipeline: %w", pErr)
	}

	s.statusMu.Lock()
	s.marketsLoaded = markets
	s.runnersLoaded = runners
	s.inPlayCount = inPlay
	s.lastRefresh = time.Now()
	s.statusMu.Unlock()

	s.logger.InfoContext(ctx, "seeded mock odds cache",
		"provider", s.provider.Name(),
		"markets", markets,
		"runners", runners,
		"in_play", inPlay,
	)
	return markets, runners, nil
}

// GetStatus returns a snapshot of the odds-service's seed/cache health. It is
// served by GET /api/v1/odds/status and is intentionally cheap: the counters
// are already in memory, we just lock briefly to read them. If no seed has
// run yet the counters will all be zero and LastRefresh will be the zero
// time, which is still a valid response.
func (s *Service) GetStatus(_ context.Context) Status {
	s.statusMu.RLock()
	defer s.statusMu.RUnlock()
	return Status{
		Provider:      s.provider.Name(),
		MarketsLoaded: s.marketsLoaded,
		RunnersLoaded: s.runnersLoaded,
		LastRefresh:   s.lastRefresh,
		InPlayCount:   s.inPlayCount,
	}
}

// ---------------------------------------------------------------------------
// Health
// ---------------------------------------------------------------------------

func (s *Service) HealthCheck(ctx context.Context) error {
	return s.provider.HealthCheck(ctx)
}
