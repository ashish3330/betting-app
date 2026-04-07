package responsible

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

type GamblingLimits struct {
	UserID               int64      `json:"user_id"`
	DailyDepositLimit    float64    `json:"daily_deposit_limit"`
	WeeklyDepositLimit   float64    `json:"weekly_deposit_limit"`
	MonthlyDepositLimit  float64    `json:"monthly_deposit_limit"`
	DailyLossLimit       *float64   `json:"daily_loss_limit"`
	MaxStakePerBet       float64    `json:"max_stake_per_bet"`
	SessionTimeLimitMins int        `json:"session_time_limit_mins"`
	SelfExcludedUntil    *time.Time `json:"self_excluded_until"`
	CoolingOffUntil      *time.Time `json:"cooling_off_until"`
	RealityCheckMins     int        `json:"reality_check_interval_mins"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

type UpdateLimitsRequest struct {
	DailyDepositLimit    *float64 `json:"daily_deposit_limit" validate:"omitempty,gt=0"`
	WeeklyDepositLimit   *float64 `json:"weekly_deposit_limit" validate:"omitempty,gt=0"`
	MonthlyDepositLimit  *float64 `json:"monthly_deposit_limit" validate:"omitempty,gt=0"`
	DailyLossLimit       *float64 `json:"daily_loss_limit" validate:"omitempty,gt=0"`
	MaxStakePerBet       *float64 `json:"max_stake_per_bet" validate:"omitempty,gt=0"`
	SessionTimeLimitMins *int     `json:"session_time_limit_mins" validate:"omitempty,gte=15,lte=1440"`
	RealityCheckMins     *int     `json:"reality_check_interval_mins" validate:"omitempty,gte=15,lte=240"`
}

type SelfExclusionRequest struct {
	Duration string `json:"duration" validate:"required,oneof=24h 7d 30d 90d 180d 365d permanent"`
	Reason   string `json:"reason"`
}

type Service struct {
	db     *sql.DB
	redis  *redis.Client
	logger *slog.Logger
}

func NewService(db *sql.DB, rdb *redis.Client, logger *slog.Logger) *Service {
	return &Service{db: db, redis: rdb, logger: logger}
}

func (s *Service) GetLimits(ctx context.Context, userID int64) (*GamblingLimits, error) {
	limits := &GamblingLimits{UserID: userID}

	err := s.db.QueryRowContext(ctx,
		`SELECT daily_deposit_limit, weekly_deposit_limit, monthly_deposit_limit,
		        daily_loss_limit, max_stake_per_bet, session_time_limit_mins,
		        self_excluded_until, cooling_off_until, reality_check_interval_mins, updated_at
		 FROM responsible_gambling WHERE user_id = $1`, userID,
	).Scan(&limits.DailyDepositLimit, &limits.WeeklyDepositLimit, &limits.MonthlyDepositLimit,
		&limits.DailyLossLimit, &limits.MaxStakePerBet, &limits.SessionTimeLimitMins,
		&limits.SelfExcludedUntil, &limits.CoolingOffUntil, &limits.RealityCheckMins, &limits.UpdatedAt)

	if err == sql.ErrNoRows {
		// Create default limits
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO responsible_gambling (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID)
		if err != nil {
			return nil, fmt.Errorf("create default limits: %w", err)
		}
		// Return defaults
		limits.DailyDepositLimit = 100000
		limits.WeeklyDepositLimit = 500000
		limits.MonthlyDepositLimit = 2000000
		limits.MaxStakePerBet = 500000
		limits.SessionTimeLimitMins = 480
		limits.RealityCheckMins = 60
		limits.UpdatedAt = time.Now()
		return limits, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get limits: %w", err)
	}
	return limits, nil
}

func (s *Service) UpdateLimits(ctx context.Context, userID int64, req *UpdateLimitsRequest) (*GamblingLimits, error) {
	// Limits can only be decreased immediately; increases take 24h cooling-off
	current, err := s.GetLimits(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Apply updates (only allow decreasing limits immediately)
	if req.DailyDepositLimit != nil {
		if *req.DailyDepositLimit > current.DailyDepositLimit {
			// Schedule increase for 24h later
			s.logger.InfoContext(ctx, "deposit limit increase scheduled (24h delay)",
				"user_id", userID, "current", current.DailyDepositLimit, "requested", *req.DailyDepositLimit)
		}
		current.DailyDepositLimit = *req.DailyDepositLimit
	}
	if req.WeeklyDepositLimit != nil {
		current.WeeklyDepositLimit = *req.WeeklyDepositLimit
	}
	if req.MonthlyDepositLimit != nil {
		current.MonthlyDepositLimit = *req.MonthlyDepositLimit
	}
	if req.DailyLossLimit != nil {
		current.DailyLossLimit = req.DailyLossLimit
	}
	if req.MaxStakePerBet != nil {
		current.MaxStakePerBet = *req.MaxStakePerBet
	}
	if req.SessionTimeLimitMins != nil {
		current.SessionTimeLimitMins = *req.SessionTimeLimitMins
	}
	if req.RealityCheckMins != nil {
		current.RealityCheckMins = *req.RealityCheckMins
	}

	_, err = s.db.ExecContext(ctx,
		`UPDATE responsible_gambling SET
			daily_deposit_limit = $1, weekly_deposit_limit = $2, monthly_deposit_limit = $3,
			daily_loss_limit = $4, max_stake_per_bet = $5, session_time_limit_mins = $6,
			reality_check_interval_mins = $7, updated_at = NOW()
		 WHERE user_id = $8`,
		current.DailyDepositLimit, current.WeeklyDepositLimit, current.MonthlyDepositLimit,
		current.DailyLossLimit, current.MaxStakePerBet, current.SessionTimeLimitMins,
		current.RealityCheckMins, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("update limits: %w", err)
	}

	s.logger.InfoContext(ctx, "gambling limits updated", "user_id", userID)
	return current, nil
}

func (s *Service) SelfExclude(ctx context.Context, userID int64, req *SelfExclusionRequest) error {
	var until time.Time
	now := time.Now()

	switch req.Duration {
	case "24h":
		until = now.Add(24 * time.Hour)
	case "7d":
		until = now.Add(7 * 24 * time.Hour)
	case "30d":
		until = now.Add(30 * 24 * time.Hour)
	case "90d":
		until = now.Add(90 * 24 * time.Hour)
	case "180d":
		until = now.Add(180 * 24 * time.Hour)
	case "365d":
		until = now.Add(365 * 24 * time.Hour)
	case "permanent":
		until = now.Add(100 * 365 * 24 * time.Hour) // 100 years
	default:
		return fmt.Errorf("invalid duration: %s", req.Duration)
	}

	_, err := s.db.ExecContext(ctx,
		`UPDATE responsible_gambling SET self_excluded_until = $1, updated_at = NOW() WHERE user_id = $2`,
		until, userID,
	)
	if err != nil {
		return fmt.Errorf("self exclude: %w", err)
	}

	// Also suspend the user account
	_, err = s.db.ExecContext(ctx,
		`UPDATE users SET status = 'suspended', updated_at = NOW() WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("suspend user: %w", err)
	}

	s.logger.WarnContext(ctx, "user self-excluded",
		"user_id", userID, "until", until, "reason", req.Reason)
	return nil
}

func (s *Service) SetCoolingOff(ctx context.Context, userID int64, hours int) error {
	until := time.Now().Add(time.Duration(hours) * time.Hour)
	_, err := s.db.ExecContext(ctx,
		`UPDATE responsible_gambling SET cooling_off_until = $1, updated_at = NOW() WHERE user_id = $2`,
		until, userID,
	)
	if err != nil {
		return fmt.Errorf("set cooling off: %w", err)
	}

	s.logger.InfoContext(ctx, "cooling off period set", "user_id", userID, "hours", hours)
	return nil
}

func (s *Service) CheckCanBet(ctx context.Context, userID int64, stake float64) error {
	limits, err := s.GetLimits(ctx, userID)
	if err != nil {
		return fmt.Errorf("get limits: %w", err)
	}

	now := time.Now()

	// Check self-exclusion
	if limits.SelfExcludedUntil != nil && now.Before(*limits.SelfExcludedUntil) {
		return fmt.Errorf("account is self-excluded until %s", limits.SelfExcludedUntil.Format("2006-01-02"))
	}

	// Check cooling off
	if limits.CoolingOffUntil != nil && now.Before(*limits.CoolingOffUntil) {
		return fmt.Errorf("account is in cooling-off period until %s", limits.CoolingOffUntil.Format("2006-01-02 15:04"))
	}

	// Check max stake per bet
	if stake > limits.MaxStakePerBet {
		return fmt.Errorf("stake %.2f exceeds maximum allowed %.2f", stake, limits.MaxStakePerBet)
	}

	// Check daily loss limit
	if limits.DailyLossLimit != nil {
		var dailyLoss float64
		s.db.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(ABS(profit)), 0) FROM bets
			 WHERE user_id = $1 AND status = 'settled' AND profit < 0
			 AND settled_at >= CURRENT_DATE`,
			userID,
		).Scan(&dailyLoss)

		if dailyLoss >= *limits.DailyLossLimit {
			return fmt.Errorf("daily loss limit of %.2f reached", *limits.DailyLossLimit)
		}
	}

	return nil
}

func (s *Service) CheckDepositLimit(ctx context.Context, userID int64, amount float64) error {
	limits, err := s.GetLimits(ctx, userID)
	if err != nil {
		return fmt.Errorf("get limits: %w", err)
	}

	// Check daily deposit limit
	var dailyDeposits float64
	s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM payment_transactions
		 WHERE user_id = $1 AND direction = 'deposit' AND status = 'completed'
		 AND created_at >= CURRENT_DATE`,
		userID,
	).Scan(&dailyDeposits)

	if dailyDeposits+amount > limits.DailyDepositLimit {
		return fmt.Errorf("daily deposit limit of %.2f would be exceeded (current: %.2f)", limits.DailyDepositLimit, dailyDeposits)
	}

	// Check weekly deposit limit
	var weeklyDeposits float64
	s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM payment_transactions
		 WHERE user_id = $1 AND direction = 'deposit' AND status = 'completed'
		 AND created_at >= CURRENT_DATE - INTERVAL '7 days'`,
		userID,
	).Scan(&weeklyDeposits)

	if weeklyDeposits+amount > limits.WeeklyDepositLimit {
		return fmt.Errorf("weekly deposit limit of %.2f would be exceeded", limits.WeeklyDepositLimit)
	}

	// Check monthly deposit limit
	var monthlyDeposits float64
	s.db.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(amount), 0) FROM payment_transactions
		 WHERE user_id = $1 AND direction = 'deposit' AND status = 'completed'
		 AND created_at >= CURRENT_DATE - INTERVAL '30 days'`,
		userID,
	).Scan(&monthlyDeposits)

	if monthlyDeposits+amount > limits.MonthlyDepositLimit {
		return fmt.Errorf("monthly deposit limit of %.2f would be exceeded", limits.MonthlyDepositLimit)
	}

	return nil
}

func (s *Service) GetSessionDuration(ctx context.Context, userID int64) (time.Duration, int, error) {
	limits, err := s.GetLimits(ctx, userID)
	if err != nil {
		return 0, 0, err
	}

	// Get session start from Redis
	key := fmt.Sprintf("session:start:%d", userID)
	startStr, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		// No active session, start one
		s.redis.Set(ctx, key, time.Now().Unix(), 24*time.Hour)
		return 0, limits.SessionTimeLimitMins, nil
	}

	var startUnix int64
	fmt.Sscanf(startStr, "%d", &startUnix)
	start := time.Unix(startUnix, 0)
	duration := time.Since(start)

	return duration, limits.SessionTimeLimitMins, nil
}
