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

// UpdateLimits uses SELECT ... FOR UPDATE within a transaction to prevent
// concurrent updates from causing a TOCTOU race (last-write-wins).
func (s *Service) UpdateLimits(ctx context.Context, userID int64, req *UpdateLimitsRequest) (*GamblingLimits, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update-limits tx: %w", err)
	}
	defer tx.Rollback()

	// Lock the row to prevent concurrent modifications
	current := &GamblingLimits{UserID: userID}
	err = tx.QueryRowContext(ctx,
		`SELECT daily_deposit_limit, weekly_deposit_limit, monthly_deposit_limit,
		        daily_loss_limit, max_stake_per_bet, session_time_limit_mins,
		        self_excluded_until, cooling_off_until, reality_check_interval_mins, updated_at
		 FROM responsible_gambling WHERE user_id = $1 FOR UPDATE`, userID,
	).Scan(&current.DailyDepositLimit, &current.WeeklyDepositLimit, &current.MonthlyDepositLimit,
		&current.DailyLossLimit, &current.MaxStakePerBet, &current.SessionTimeLimitMins,
		&current.SelfExcludedUntil, &current.CoolingOffUntil, &current.RealityCheckMins, &current.UpdatedAt)
	if err == sql.ErrNoRows {
		// Create default row if missing, then re-select with lock
		_, err = tx.ExecContext(ctx,
			`INSERT INTO responsible_gambling (user_id) VALUES ($1) ON CONFLICT (user_id) DO NOTHING`, userID)
		if err != nil {
			return nil, fmt.Errorf("create default limits: %w", err)
		}
		err = tx.QueryRowContext(ctx,
			`SELECT daily_deposit_limit, weekly_deposit_limit, monthly_deposit_limit,
			        daily_loss_limit, max_stake_per_bet, session_time_limit_mins,
			        self_excluded_until, cooling_off_until, reality_check_interval_mins, updated_at
			 FROM responsible_gambling WHERE user_id = $1 FOR UPDATE`, userID,
		).Scan(&current.DailyDepositLimit, &current.WeeklyDepositLimit, &current.MonthlyDepositLimit,
			&current.DailyLossLimit, &current.MaxStakePerBet, &current.SessionTimeLimitMins,
			&current.SelfExcludedUntil, &current.CoolingOffUntil, &current.RealityCheckMins, &current.UpdatedAt)
		if err != nil {
			return nil, fmt.Errorf("get newly created limits: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("get limits for update: %w", err)
	}

	var pendingIncreases []string

	// Apply updates: decreases take effect immediately, increases are deferred 24h
	if req.DailyDepositLimit != nil {
		if *req.DailyDepositLimit > current.DailyDepositLimit {
			s.scheduleLimitIncrease(ctx, userID, "daily_deposit_limit", *req.DailyDepositLimit)
			pendingIncreases = append(pendingIncreases, "daily_deposit_limit")
		} else {
			current.DailyDepositLimit = *req.DailyDepositLimit
		}
	}
	if req.WeeklyDepositLimit != nil {
		if *req.WeeklyDepositLimit > current.WeeklyDepositLimit {
			s.scheduleLimitIncrease(ctx, userID, "weekly_deposit_limit", *req.WeeklyDepositLimit)
			pendingIncreases = append(pendingIncreases, "weekly_deposit_limit")
		} else {
			current.WeeklyDepositLimit = *req.WeeklyDepositLimit
		}
	}
	if req.MonthlyDepositLimit != nil {
		if *req.MonthlyDepositLimit > current.MonthlyDepositLimit {
			s.scheduleLimitIncrease(ctx, userID, "monthly_deposit_limit", *req.MonthlyDepositLimit)
			pendingIncreases = append(pendingIncreases, "monthly_deposit_limit")
		} else {
			current.MonthlyDepositLimit = *req.MonthlyDepositLimit
		}
	}
	if req.DailyLossLimit != nil {
		if current.DailyLossLimit != nil && *req.DailyLossLimit > *current.DailyLossLimit {
			s.scheduleLimitIncrease(ctx, userID, "daily_loss_limit", *req.DailyLossLimit)
			pendingIncreases = append(pendingIncreases, "daily_loss_limit")
		} else {
			current.DailyLossLimit = req.DailyLossLimit
		}
	}
	if req.MaxStakePerBet != nil {
		if *req.MaxStakePerBet > current.MaxStakePerBet {
			s.scheduleLimitIncrease(ctx, userID, "max_stake_per_bet", *req.MaxStakePerBet)
			pendingIncreases = append(pendingIncreases, "max_stake_per_bet")
		} else {
			current.MaxStakePerBet = *req.MaxStakePerBet
		}
	}
	if req.SessionTimeLimitMins != nil {
		if *req.SessionTimeLimitMins > current.SessionTimeLimitMins {
			s.scheduleLimitIncrease(ctx, userID, "session_time_limit_mins", float64(*req.SessionTimeLimitMins))
			pendingIncreases = append(pendingIncreases, "session_time_limit_mins")
		} else {
			current.SessionTimeLimitMins = *req.SessionTimeLimitMins
		}
	}
	if req.RealityCheckMins != nil {
		if *req.RealityCheckMins > current.RealityCheckMins {
			s.scheduleLimitIncrease(ctx, userID, "reality_check_interval_mins", float64(*req.RealityCheckMins))
			pendingIncreases = append(pendingIncreases, "reality_check_interval_mins")
		} else {
			current.RealityCheckMins = *req.RealityCheckMins
		}
	}

	_, err = tx.ExecContext(ctx,
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

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update-limits tx: %w", err)
	}

	if len(pendingIncreases) > 0 {
		s.logger.InfoContext(ctx, "gambling limits updated with pending increases",
			"user_id", userID, "pending", pendingIncreases)
	} else {
		s.logger.InfoContext(ctx, "gambling limits updated", "user_id", userID)
	}
	return current, nil
}

// scheduleLimitIncrease stores a pending limit increase that takes effect after 24h.
func (s *Service) scheduleLimitIncrease(ctx context.Context, userID int64, field string, newValue float64) {
	effectiveAt := time.Now().Add(24 * time.Hour)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO pending_limit_increases (user_id, field_name, new_value, effective_at, created_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (user_id, field_name) DO UPDATE SET new_value = $3, effective_at = $4`,
		userID, field, newValue, effectiveAt,
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to schedule limit increase",
			"user_id", userID, "field", field, "error", err)
		return
	}

	s.logger.InfoContext(ctx, "limit increase scheduled (24h delay)",
		"user_id", userID, "field", field, "new_value", newValue, "effective_at", effectiveAt)
}

// SelfExclude wraps all DB operations (update responsible_gambling, suspend user,
// void bets) in a single transaction to prevent partial application.
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin self-exclude tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.ExecContext(ctx,
		`UPDATE responsible_gambling SET self_excluded_until = $1, updated_at = NOW() WHERE user_id = $2`,
		until, userID,
	)
	if err != nil {
		return fmt.Errorf("self exclude: %w", err)
	}

	// Suspend the user account
	_, err = tx.ExecContext(ctx,
		`UPDATE users SET status = 'suspended', updated_at = NOW() WHERE id = $1`, userID)
	if err != nil {
		return fmt.Errorf("suspend user: %w", err)
	}

	// Cancel/void any open bets for the excluded user
	result, err := tx.ExecContext(ctx,
		`UPDATE bets SET status = 'voided', settled_at = NOW(), profit = 0
		 WHERE user_id = $1 AND status IN ('open', 'partial', 'matched')`, userID)
	if err != nil {
		return fmt.Errorf("void open bets during self-exclusion: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit self-exclude tx: %w", err)
	}

	voidedCount, _ := result.RowsAffected()
	if voidedCount > 0 {
		s.logger.WarnContext(ctx, "voided open bets due to self-exclusion",
			"user_id", userID, "voided_count", voidedCount)
	}

	// Invalidate all active sessions by marking exclusion in Redis
	// so auth middleware can check this during login/request validation
	exclusionKey := fmt.Sprintf("self_excluded:%d", userID)
	s.redis.Set(ctx, exclusionKey, until.Unix(), time.Until(until))

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

// CheckCanBet runs the limit check and daily loss aggregation within a single
// REPEATABLE READ transaction to prevent TOCTOU races where limits could change
// or losses could accumulate between the two queries.
func (s *Service) CheckCanBet(ctx context.Context, userID int64, stake float64) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{
		Isolation: sql.LevelRepeatableRead,
		ReadOnly:  true,
	})
	if err != nil {
		return fmt.Errorf("begin check-can-bet tx: %w", err)
	}
	defer tx.Rollback()

	limits := &GamblingLimits{UserID: userID}
	err = tx.QueryRowContext(ctx,
		`SELECT daily_deposit_limit, weekly_deposit_limit, monthly_deposit_limit,
		        daily_loss_limit, max_stake_per_bet, session_time_limit_mins,
		        self_excluded_until, cooling_off_until, reality_check_interval_mins, updated_at
		 FROM responsible_gambling WHERE user_id = $1`, userID,
	).Scan(&limits.DailyDepositLimit, &limits.WeeklyDepositLimit, &limits.MonthlyDepositLimit,
		&limits.DailyLossLimit, &limits.MaxStakePerBet, &limits.SessionTimeLimitMins,
		&limits.SelfExcludedUntil, &limits.CoolingOffUntil, &limits.RealityCheckMins, &limits.UpdatedAt)
	if err == sql.ErrNoRows {
		// No limits configured — use defaults, allow the bet
		_ = tx.Rollback()
		return nil
	}
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

	// Check daily loss limit — within the same snapshot
	if limits.DailyLossLimit != nil {
		var dailyLoss float64
		err := tx.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(ABS(profit)), 0) FROM bets
			 WHERE user_id = $1 AND status = 'settled' AND profit < 0
			 AND settled_at >= CURRENT_DATE`,
			userID,
		).Scan(&dailyLoss)
		if err != nil {
			return fmt.Errorf("check daily loss: %w", err)
		}

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

// IsUserSelfExcluded checks if a user is currently self-excluded.
// This should be called at the auth/login level to prevent excluded users from accessing the platform.
func (s *Service) IsUserSelfExcluded(ctx context.Context, userID int64) (bool, error) {
	// Fast path: check Redis cache first
	exclusionKey := fmt.Sprintf("self_excluded:%d", userID)
	_, err := s.redis.Get(ctx, exclusionKey).Result()
	if err == nil {
		return true, nil
	}

	// Fallback: check database
	var excludedUntil *time.Time
	err = s.db.QueryRowContext(ctx,
		`SELECT self_excluded_until FROM responsible_gambling WHERE user_id = $1`, userID,
	).Scan(&excludedUntil)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("check self-exclusion: %w", err)
	}

	if excludedUntil != nil && time.Now().Before(*excludedUntil) {
		return true, nil
	}
	return false, nil
}
