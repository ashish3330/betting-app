package fraud

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

type Alert struct {
	ID         string     `json:"id"`
	UserID     int64      `json:"user_id"`
	Type       string     `json:"type"`
	Risk       RiskLevel  `json:"risk_level"`
	Details    string     `json:"details"`
	Score      float64    `json:"score"`
	Resolved   bool       `json:"resolved"`
	ResolvedBy *int64     `json:"resolved_by,omitempty"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	Resolution string     `json:"resolution,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type UserRiskProfile struct {
	UserID           int64     `json:"user_id"`
	RiskScore        float64   `json:"risk_score"`
	RiskLevel        RiskLevel `json:"risk_level"`
	BetFrequency     float64   `json:"bet_frequency_per_min"`
	AvgStake         float64   `json:"avg_stake"`
	MaxStake         float64   `json:"max_stake"`
	WinRate          float64   `json:"win_rate"`
	IPCount          int       `json:"distinct_ip_count"`
	DeviceCount      int       `json:"distinct_device_count"`
	SuspiciousFlags  []string  `json:"suspicious_flags"`
	LastAssessedAt   time.Time `json:"last_assessed_at"`
}

type BetPattern struct {
	UserID       int64
	Timestamp    time.Time
	Stake        float64
	Price        float64
	Side         string
	MarketID     string
	IPAddress    string
	DeviceID     string
}

// Thresholds for fraud detection
type Thresholds struct {
	MaxBetsPerMinute  float64
	MaxStakeSingle    float64
	MaxWinRate        float64
	MinOdds           float64
	MaxIPsPerUser     int
	MaxDevicesPerUser int
	ArbSpreadLimit    float64
}

// luaFreqCount is a Lua script that atomically performs the sliding window
// frequency count: remove expired entries, add the new entry, set expiry,
// and return the current count.
var luaFreqCount = redis.NewScript(`
	redis.call('ZREMRANGEBYSCORE', KEYS[1], '0', ARGV[1])
	redis.call('ZADD', KEYS[1], ARGV[2], ARGV[3])
	redis.call('EXPIRE', KEYS[1], 120)
	return redis.call('ZCARD', KEYS[1])
`)

type Service struct {
	db         *sql.DB
	redis      *redis.Client
	logger     *slog.Logger
	thresholds Thresholds
}

func NewService(db *sql.DB, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{
		db:     db,
		redis:  rdb,
		logger: logger,
		thresholds: Thresholds{
			MaxBetsPerMinute:  30,
			MaxStakeSingle:    500000,
			MaxWinRate:        0.85,
			MinOdds:           1.01,
			MaxIPsPerUser:     5,
			MaxDevicesPerUser: 3,
			ArbSpreadLimit:    0.02,
		},
	}
}

func (s *Service) AnalyzeBet(ctx context.Context, pattern *BetPattern) (*UserRiskProfile, error) {
	profile := &UserRiskProfile{
		UserID:         pattern.UserID,
		LastAssessedAt: time.Now(),
	}

	var flags []string
	var riskScore float64

	// 1. Bet frequency check (Redis sliding window via atomic Lua script)
	freqKey := fmt.Sprintf("fraud:freq:%d", pattern.UserID)
	now := time.Now().UnixMilli()
	windowStart := now - 60000 // 1 minute window

	count, err := luaFreqCount.Run(ctx, s.redis, []string{freqKey},
		fmt.Sprintf("%d", windowStart),
		float64(now),
		now,
	).Int64()
	if err != nil {
		s.logger.WarnContext(ctx, "frequency check failed", "user_id", pattern.UserID, "error", err)
		count = 0
	}
	profile.BetFrequency = float64(count)

	if profile.BetFrequency > s.thresholds.MaxBetsPerMinute {
		flags = append(flags, "high_bet_frequency")
		riskScore += 30
	}

	// 2. Stake size anomaly
	if pattern.Stake > s.thresholds.MaxStakeSingle {
		flags = append(flags, "excessive_stake")
		riskScore += 25
	}

	// 3. Check historical avg stake and detect sudden changes. Any error
	// leaves avgStake as 0, which disables the stake-spike check for the
	// call — acceptable for a heuristic risk score.
	var avgStake float64
	_ = s.db.QueryRowContext(ctx,
		"SELECT COALESCE(AVG(stake), 0) FROM bets WHERE user_id = $1 AND created_at > NOW() - INTERVAL '7 days'",
		pattern.UserID,
	).Scan(&avgStake)
	profile.AvgStake = avgStake

	if avgStake > 0 && pattern.Stake > avgStake*5 {
		flags = append(flags, "stake_spike")
		riskScore += 20
	}

	// 4. Win rate analysis. Scan errors are tolerated — heuristic metric only.
	var totalBets, wonBets int
	_ = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*), COALESCE(SUM(CASE WHEN profit > 0 THEN 1 ELSE 0 END), 0)
		 FROM bets WHERE user_id = $1 AND status = 'settled'`,
		pattern.UserID,
	).Scan(&totalBets, &wonBets)

	if totalBets > 20 {
		profile.WinRate = float64(wonBets) / float64(totalBets)
		if profile.WinRate > s.thresholds.MaxWinRate {
			flags = append(flags, "abnormal_win_rate")
			riskScore += 35
		}
	}

	// 5. Multi-IP detection
	ipKey := fmt.Sprintf("fraud:ips:%d", pattern.UserID)
	if pattern.IPAddress != "" {
		s.redis.SAdd(ctx, ipKey, pattern.IPAddress)
		s.redis.Expire(ctx, ipKey, 24*time.Hour)
	}
	ipCount, _ := s.redis.SCard(ctx, ipKey).Result()
	profile.IPCount = int(ipCount)

	if profile.IPCount > s.thresholds.MaxIPsPerUser {
		flags = append(flags, "multiple_ips")
		riskScore += 15
	}

	// 6. Multi-device detection
	devKey := fmt.Sprintf("fraud:devices:%d", pattern.UserID)
	if pattern.DeviceID != "" {
		s.redis.SAdd(ctx, devKey, pattern.DeviceID)
		s.redis.Expire(ctx, devKey, 24*time.Hour)
	}
	devCount, _ := s.redis.SCard(ctx, devKey).Result()
	profile.DeviceCount = int(devCount)

	if profile.DeviceCount > s.thresholds.MaxDevicesPerUser {
		flags = append(flags, "multiple_devices")
		riskScore += 15
	}

	// 7. Arbitrage detection (back+lay on same market near-instantly)
	arbKey := fmt.Sprintf("fraud:arb:%d:%s", pattern.UserID, pattern.MarketID)
	lastSide, err := s.redis.Get(ctx, arbKey).Result()
	if err == nil && lastSide != pattern.Side {
		flags = append(flags, "potential_arbitrage")
		riskScore += 40
	}
	s.redis.Set(ctx, arbKey, pattern.Side, 5*time.Second)

	// 8. Odds too close to 1.0 (suspicious)
	if pattern.Price < 1.05 && pattern.Side == "back" {
		flags = append(flags, "suspicious_low_odds")
		riskScore += 10
	}

	// Calculate final risk
	riskScore = math.Min(riskScore, 100)
	profile.RiskScore = riskScore
	profile.SuspiciousFlags = flags

	switch {
	case riskScore >= 80:
		profile.RiskLevel = RiskCritical
	case riskScore >= 60:
		profile.RiskLevel = RiskHigh
	case riskScore >= 30:
		profile.RiskLevel = RiskMedium
	default:
		profile.RiskLevel = RiskLow
	}

	// Create alert if high risk
	if profile.RiskLevel == RiskHigh || profile.RiskLevel == RiskCritical {
		alert := &Alert{
			ID:        fmt.Sprintf("alert-%d-%s", pattern.UserID, uuid.New().String()),
			UserID:    pattern.UserID,
			Type:      "bet_analysis",
			Risk:      profile.RiskLevel,
			Details:   fmt.Sprintf("flags: %v, score: %.1f", flags, riskScore),
			Score:     riskScore,
			CreatedAt: time.Now(),
		}
		s.createAlert(ctx, alert)
	}

	// Cache profile
	s.redis.Set(ctx, fmt.Sprintf("fraud:profile:%d", pattern.UserID), riskScore, 10*time.Minute)

	return profile, nil
}

func (s *Service) createAlert(ctx context.Context, alert *Alert) {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO fraud_alerts (id, user_id, type, risk_level, details, score, resolved, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		alert.ID, alert.UserID, alert.Type, alert.Risk, alert.Details, alert.Score, false, alert.CreatedAt,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to create fraud alert", "error", err)
		return
	}

	s.logger.WarnContext(ctx, "fraud alert created",
		"alert_id", alert.ID, "user_id", alert.UserID, "risk", alert.Risk, "score", alert.Score)
}

func (s *Service) GetAlerts(ctx context.Context, resolved *bool, limit int) ([]*Alert, error) {
	query := "SELECT id, user_id, type, risk_level, details, score, resolved, created_at FROM fraud_alerts"
	var args []interface{}

	if resolved != nil {
		query += " WHERE resolved = $1"
		args = append(args, *resolved)
	}
	query += " ORDER BY created_at DESC LIMIT $" + fmt.Sprintf("%d", len(args)+1)
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var alerts []*Alert
	for rows.Next() {
		a := &Alert{}
		if err := rows.Scan(&a.ID, &a.UserID, &a.Type, &a.Risk, &a.Details,
			&a.Score, &a.Resolved, &a.CreatedAt); err != nil {
			return nil, err
		}
		alerts = append(alerts, a)
	}
	return alerts, rows.Err()
}

func (s *Service) ResolveAlert(ctx context.Context, alertID string, adminID int64, resolution string) error {
	now := time.Now()
	_, err := s.db.ExecContext(ctx,
		`UPDATE fraud_alerts SET resolved = true, resolved_by = $1, resolved_at = $2, resolution = $3
		 WHERE id = $4`,
		adminID, now, resolution, alertID)
	if err != nil {
		return fmt.Errorf("resolve alert: %w", err)
	}
	s.logger.InfoContext(ctx, "fraud alert resolved",
		"alert_id", alertID, "admin_id", adminID, "resolved_at", now)
	return nil
}

func (s *Service) GetUserRiskScore(ctx context.Context, userID int64) (float64, RiskLevel, error) {
	// Check cache first
	key := fmt.Sprintf("fraud:profile:%d", userID)
	score, err := s.redis.Get(ctx, key).Float64()
	if err == nil {
		level := RiskLow
		switch {
		case score >= 80:
			level = RiskCritical
		case score >= 60:
			level = RiskHigh
		case score >= 30:
			level = RiskMedium
		}
		return score, level, nil
	}

	return 0, RiskLow, nil
}

func (s *Service) ShouldBlockBet(ctx context.Context, userID int64) (bool, string) {
	score, level, _ := s.GetUserRiskScore(ctx, userID)
	if level == RiskCritical {
		return true, fmt.Sprintf("account flagged for review (risk score: %.0f)", score)
	}
	return false, ""
}
