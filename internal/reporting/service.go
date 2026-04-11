package reporting

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

type Service struct {
	clickhouse *sql.DB
	postgres   *sql.DB
	logger     *slog.Logger
}

func NewService(clickhouse, postgres *sql.DB, logger *slog.Logger) *Service {
	return &Service{clickhouse: clickhouse, postgres: postgres, logger: logger}
}

type PnLReport struct {
	UserID       int64   `json:"user_id"`
	Period       string  `json:"period"`
	TotalBets    int     `json:"total_bets"`
	TotalStake   float64 `json:"total_stake"`
	TotalPayout  float64 `json:"total_payout"`
	GrossProfit  float64 `json:"gross_profit"`
	Commission   float64 `json:"commission"`
	NetProfit    float64 `json:"net_profit"`
	WinRate      float64 `json:"win_rate"`
}

type MarketReport struct {
	MarketID      string  `json:"market_id"`
	MarketName    string  `json:"market_name"`
	TotalBets     int     `json:"total_bets"`
	TotalMatched  float64 `json:"total_matched"`
	BackVolume    float64 `json:"back_volume"`
	LayVolume     float64 `json:"lay_volume"`
	NetPosition   float64 `json:"net_position"`
}

type DashboardStats struct {
	ActiveUsers       int     `json:"active_users"`
	TotalBetsToday    int     `json:"total_bets_today"`
	TotalVolumeToday  float64 `json:"total_volume_today"`
	ActiveMarkets     int     `json:"active_markets"`
	TotalExposure     float64 `json:"total_exposure"`
	RevenueToday      float64 `json:"revenue_today"`
	OnlineUsers       int     `json:"online_users"`
	PeakConcurrent    int     `json:"peak_concurrent"`
}

type BetVolumePoint struct {
	Timestamp time.Time `json:"timestamp"`
	Volume    float64   `json:"volume"`
	Count     int       `json:"count"`
}

type ExposureTrend struct {
	Timestamp time.Time `json:"timestamp"`
	Exposure  float64   `json:"exposure"`
	MarketID  string    `json:"market_id"`
}

func (s *Service) GetUserPnL(ctx context.Context, userID int64, from, to time.Time) (*PnLReport, error) {
	report := &PnLReport{
		UserID: userID,
		Period: fmt.Sprintf("%s to %s", from.Format("2006-01-02"), to.Format("2006-01-02")),
	}

	err := s.postgres.QueryRowContext(ctx,
		`SELECT
			COUNT(*),
			COALESCE(SUM(stake), 0),
			COALESCE(SUM(CASE WHEN profit > 0 THEN profit + stake ELSE 0 END), 0),
			COALESCE(SUM(profit), 0),
			COALESCE(SUM(CASE WHEN profit > 0 THEN 1 ELSE 0 END)::float / NULLIF(COUNT(*), 0), 0)
		 FROM bets
		 WHERE user_id = $1 AND status = 'settled' AND settled_at BETWEEN $2 AND $3`,
		userID, from, to,
	).Scan(&report.TotalBets, &report.TotalStake, &report.TotalPayout,
		&report.GrossProfit, &report.WinRate)
	if err != nil {
		return nil, fmt.Errorf("get pnl: %w", err)
	}

	// Get commission
	err = s.postgres.QueryRowContext(ctx,
		`SELECT COALESCE(SUM(ABS(amount)), 0) FROM ledger
		 WHERE user_id = $1 AND type = 'commission' AND created_at BETWEEN $2 AND $3`,
		userID, from, to,
	).Scan(&report.Commission)
	if err != nil {
		return nil, fmt.Errorf("get commission: %w", err)
	}

	report.NetProfit = report.GrossProfit - report.Commission
	return report, nil
}

func (s *Service) GetMarketReport(ctx context.Context, marketID string) (*MarketReport, error) {
	report := &MarketReport{MarketID: marketID}

	err := s.postgres.QueryRowContext(ctx,
		`SELECT
			m.name,
			COUNT(b.id),
			COALESCE(m.total_matched, 0),
			COALESCE(SUM(CASE WHEN b.side = 'back' THEN b.matched_stake ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN b.side = 'lay' THEN b.matched_stake ELSE 0 END), 0)
		 FROM markets m
		 LEFT JOIN bets b ON b.market_id = m.id
		 WHERE m.id = $1
		 GROUP BY m.id, m.name, m.total_matched`,
		marketID,
	).Scan(&report.MarketName, &report.TotalBets, &report.TotalMatched,
		&report.BackVolume, &report.LayVolume)
	if err != nil {
		return nil, fmt.Errorf("get market report: %w", err)
	}

	report.NetPosition = report.LayVolume - report.BackVolume
	return report, nil
}

func (s *Service) GetDashboardStats(ctx context.Context) (*DashboardStats, error) {
	stats := &DashboardStats{}
	today := time.Now().Truncate(24 * time.Hour)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	g, ctx := errgroup.WithContext(ctx)

	// Active users (bet in last 24h)
	g.Go(func() error {
		return s.postgres.QueryRowContext(ctx,
			"SELECT COUNT(DISTINCT user_id) FROM bets WHERE created_at >= $1",
			today,
		).Scan(&stats.ActiveUsers)
	})

	// Bets today
	g.Go(func() error {
		return s.postgres.QueryRowContext(ctx,
			"SELECT COUNT(*), COALESCE(SUM(stake), 0) FROM bets WHERE created_at >= $1",
			today,
		).Scan(&stats.TotalBetsToday, &stats.TotalVolumeToday)
	})

	// Active markets
	g.Go(func() error {
		return s.postgres.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM markets WHERE status = 'open'",
		).Scan(&stats.ActiveMarkets)
	})

	// Total exposure
	g.Go(func() error {
		return s.postgres.QueryRowContext(ctx,
			"SELECT COALESCE(SUM(exposure), 0) FROM users WHERE status = 'active'",
		).Scan(&stats.TotalExposure)
	})

	// Revenue (commissions) today
	g.Go(func() error {
		return s.postgres.QueryRowContext(ctx,
			`SELECT COALESCE(SUM(ABS(amount)), 0) FROM ledger
			 WHERE type = 'commission' AND created_at >= $1`,
			today,
		).Scan(&stats.RevenueToday)
	})

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("dashboard stats: %w", err)
	}

	return stats, nil
}

func (s *Service) GetBetVolumeTrend(ctx context.Context, from, to time.Time, intervalMinutes int) ([]BetVolumePoint, error) {
	allowedIntervals := map[int]bool{1: true, 5: true, 10: true, 15: true, 30: true, 60: true}
	if !allowedIntervals[intervalMinutes] {
		return nil, fmt.Errorf("invalid interval: must be one of 1, 5, 10, 15, 30, 60")
	}

	// Safe: intervalMinutes is validated against a fixed allowlist above,
	// so no user-controlled value reaches the SQL string.
	//nolint:gosec // G201: intervalMinutes is validated against a fixed allowlist above
	query := fmt.Sprintf(`
		SELECT
			date_trunc('minute', created_at) - (EXTRACT(MINUTE FROM created_at)::int %% %d) * INTERVAL '1 minute' AS bucket,
			COALESCE(SUM(stake), 0),
			COUNT(*)
		FROM bets
		WHERE created_at BETWEEN $1 AND $2
		GROUP BY bucket
		ORDER BY bucket`, intervalMinutes)

	rows, err := s.postgres.QueryContext(ctx, query, from, to)
	if err != nil {
		return nil, fmt.Errorf("get bet volume trend: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var points []BetVolumePoint
	for rows.Next() {
		var p BetVolumePoint
		if err := rows.Scan(&p.Timestamp, &p.Volume, &p.Count); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (s *Service) GetHierarchyPnL(ctx context.Context, parentUserID int64, from, to time.Time) ([]PnLReport, error) {
	rows, err := s.postgres.QueryContext(ctx,
		`SELECT
			u.id,
			COUNT(b.id),
			COALESCE(SUM(b.stake), 0),
			COALESCE(SUM(CASE WHEN b.profit > 0 THEN b.profit + b.stake ELSE 0 END), 0),
			COALESCE(SUM(b.profit), 0)
		 FROM users u
		 LEFT JOIN bets b ON b.user_id = u.id AND b.status = 'settled' AND b.settled_at BETWEEN $2 AND $3
		 WHERE u.path <@ (SELECT path FROM users WHERE id = $1) AND u.id != $1
		 GROUP BY u.id`,
		parentUserID, from, to,
	)
	if err != nil {
		return nil, fmt.Errorf("hierarchy pnl: %w", err)
	}
	defer rows.Close()

	var reports []PnLReport
	for rows.Next() {
		var r PnLReport
		if err := rows.Scan(&r.UserID, &r.TotalBets, &r.TotalStake, &r.TotalPayout, &r.GrossProfit); err != nil {
			return nil, err
		}
		r.Period = fmt.Sprintf("%s to %s", from.Format("2006-01-02"), to.Format("2006-01-02"))
		r.NetProfit = r.GrossProfit
		reports = append(reports, r)
	}
	return reports, rows.Err()
}

// IngestToClickHouse copies settled bets to ClickHouse for analytics
func (s *Service) IngestToClickHouse(ctx context.Context, since time.Time) (int, error) {
	const batchLimit = 5000

	rows, err := s.postgres.QueryContext(ctx,
		`SELECT id, market_id, user_id, side, price, stake, matched_stake, profit, status, created_at, matched_at, settled_at
		 FROM bets
		 WHERE settled_at >= $1 AND status = 'settled'
		 ORDER BY settled_at ASC
		 LIMIT $2`,
		since, batchLimit,
	)
	if err != nil {
		return 0, fmt.Errorf("fetch settled bets: %w", err)
	}
	defer rows.Close()

	// Collect all rows first for batch insert
	type betRow struct {
		id, marketID, side, status string
		userID                     int64
		price, stake, matchedStake, profit float64
		createdAt                  time.Time
		matchedAt, settledAt       *time.Time
	}

	var batch []betRow
	for rows.Next() {
		var b betRow
		if err := rows.Scan(&b.id, &b.marketID, &b.userID, &b.side, &b.price, &b.stake,
			&b.matchedStake, &b.profit, &b.status, &b.createdAt, &b.matchedAt, &b.settledAt); err != nil {
			return 0, fmt.Errorf("scan bet row: %w", err)
		}
		batch = append(batch, b)
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("iterate bet rows: %w", err)
	}

	if len(batch) == 0 {
		return 0, nil
	}

	// Batch insert into ClickHouse using a transaction
	tx, err := s.clickhouse.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin clickhouse tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO raw_bets (bet_id, market_id, user_id, side, price, stake, matched_stake, profit, status, timestamp, matched_at, settled_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`)
	if err != nil {
		return 0, fmt.Errorf("prepare clickhouse stmt: %w", err)
	}
	defer stmt.Close()

	count := 0
	for _, b := range batch {
		_, err := stmt.ExecContext(ctx,
			b.id, b.marketID, b.userID, b.side, b.price, b.stake,
			b.matchedStake, b.profit, b.status, b.createdAt, b.matchedAt, b.settledAt,
		)
		if err != nil {
			s.logger.WarnContext(ctx, "clickhouse insert failed", "bet_id", b.id, "error", err)
			continue
		}
		count++
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit clickhouse tx: %w", err)
	}

	s.logger.InfoContext(ctx, "ingested to clickhouse", "count", count)
	return count, nil
}
