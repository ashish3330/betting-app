package odds

import (
	"context"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

// OddsProvider is the pluggable interface for odds feeds.
// Implementations: MockProvider, EntitySportsProvider
type OddsProvider interface {
	Name() string

	// Sport-level queries
	FetchMarkets(ctx context.Context, sport string) ([]*models.Market, error)
	FetchCompetitions(ctx context.Context, sport string) ([]*models.Competition, error)

	// Competition / event drill-down
	FetchEvents(ctx context.Context, competitionID string) ([]*models.Event, error)
	FetchMarketsByEvent(ctx context.Context, eventID string) ([]*models.Market, error)

	// Streaming
	Subscribe(ctx context.Context, marketIDs []string, updates chan<- *models.OddsUpdate) error

	// Health
	HealthCheck(ctx context.Context) error
	Close() error
}
