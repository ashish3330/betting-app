package risk

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"
)

type Service struct {
	db     *sql.DB
	redis  *redis.Client
	logger *slog.Logger
}

func NewService(db *sql.DB, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{db: db, redis: rdb, logger: logger}
}

type MarketExposure struct {
	MarketID       string  `json:"market_id"`
	TotalBackStake float64 `json:"total_back_stake"`
	TotalLayStake  float64 `json:"total_lay_stake"`
	NetExposure    float64 `json:"net_exposure"`
}

type UserExposure struct {
	UserID         int64              `json:"user_id"`
	TotalExposure  float64            `json:"total_exposure"`
	ByMarket       map[string]float64 `json:"by_market"`
}

func (s *Service) GetMarketExposure(ctx context.Context, marketID string) (*MarketExposure, error) {
	var backStake, layStake float64
	err := s.db.QueryRowContext(ctx,
		`SELECT
			COALESCE(SUM(CASE WHEN side = 'back' THEN stake ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN side = 'lay' THEN stake * (price - 1) ELSE 0 END), 0)
		 FROM bets
		 WHERE market_id = $1 AND status IN ('matched', 'partial')`,
		marketID,
	).Scan(&backStake, &layStake)
	if err != nil {
		return nil, fmt.Errorf("get market exposure: %w", err)
	}

	return &MarketExposure{
		MarketID:       marketID,
		TotalBackStake: backStake,
		TotalLayStake:  layStake,
		NetExposure:    layStake - backStake,
	}, nil
}

func (s *Service) GetUserExposure(ctx context.Context, userID int64) (*UserExposure, error) {
	// Get from Redis first (fast path)
	exposureKey := fmt.Sprintf("exposure:user:%d", userID)
	byMarket, err := s.redis.HGetAll(ctx, exposureKey).Result()
	if err == nil && len(byMarket) > 0 {
		ue := &UserExposure{
			UserID:   userID,
			ByMarket: make(map[string]float64),
		}
		for market, amountStr := range byMarket {
			var amount float64
			fmt.Sscanf(amountStr, "%f", &amount)
			ue.ByMarket[market] = amount
			ue.TotalExposure += amount
		}
		return ue, nil
	}

	// Fallback to DB
	rows, err := s.db.QueryContext(ctx,
		`SELECT market_id,
			SUM(CASE WHEN side = 'back' THEN stake ELSE stake * (price - 1) END) as exposure
		 FROM bets
		 WHERE user_id = $1 AND status IN ('pending', 'partial', 'unmatched')
		 GROUP BY market_id`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get user exposure: %w", err)
	}
	defer rows.Close()

	ue := &UserExposure{
		UserID:   userID,
		ByMarket: make(map[string]float64),
	}
	for rows.Next() {
		var marketID string
		var exposure float64
		if err := rows.Scan(&marketID, &exposure); err != nil {
			return nil, err
		}
		ue.ByMarket[marketID] = exposure
		ue.TotalExposure += exposure
	}

	return ue, rows.Err()
}

func (s *Service) ShouldSuspendMarket(ctx context.Context, marketID string, maxExposure float64) (bool, error) {
	exposure, err := s.GetMarketExposure(ctx, marketID)
	if err != nil {
		return false, err
	}

	if exposure.NetExposure > maxExposure || exposure.NetExposure < -maxExposure {
		s.logger.WarnContext(ctx, "market exposure exceeds threshold",
			"market", marketID, "exposure", exposure.NetExposure, "threshold", maxExposure)
		return true, nil
	}
	return false, nil
}

func (s *Service) CheckNegativeBalance(ctx context.Context, userID int64) (bool, float64, error) {
	var balance, exposure float64
	err := s.db.QueryRowContext(ctx,
		"SELECT balance, exposure FROM users WHERE id = $1", userID,
	).Scan(&balance, &exposure)
	if err != nil {
		return false, 0, err
	}

	available := balance - exposure
	return available < 0, available, nil
}
