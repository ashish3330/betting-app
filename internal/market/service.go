package market

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/lotus-exchange/lotus-exchange/internal/models"
)

type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewService(db *sql.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

func (s *Service) Create(ctx context.Context, m *models.Market) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO markets (id, event_id, sport, name, market_type, status, in_play, start_time, created_at, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())`,
		m.ID, m.EventID, m.Sport, m.Name, m.MarketType, m.Status, m.InPlay, m.StartTime,
	)
	if err != nil {
		return fmt.Errorf("create market: %w", err)
	}

	// Insert runners
	for _, r := range m.Runners {
		_, err = s.db.ExecContext(ctx,
			`INSERT INTO runners (market_id, selection_id, name, status)
			 VALUES ($1, $2, $3, $4)`,
			m.ID, r.SelectionID, r.Name, r.Status,
		)
		if err != nil {
			return fmt.Errorf("create runner: %w", err)
		}
	}

	s.logger.InfoContext(ctx, "market created", "id", m.ID, "name", m.Name)
	return nil
}

func (s *Service) Get(ctx context.Context, marketID string) (*models.Market, error) {
	m := &models.Market{}
	err := s.db.QueryRowContext(ctx,
		`SELECT id, event_id, sport, name, market_type, status, in_play, start_time, total_matched, created_at, updated_at
		 FROM markets WHERE id = $1`, marketID,
	).Scan(&m.ID, &m.EventID, &m.Sport, &m.Name, &m.MarketType, &m.Status,
		&m.InPlay, &m.StartTime, &m.TotalMatched, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get market: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT selection_id, name, status FROM runners WHERE market_id = $1", marketID)
	if err != nil {
		return nil, fmt.Errorf("get runners: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var r models.Runner
		if err := rows.Scan(&r.SelectionID, &r.Name, &r.Status); err != nil {
			return nil, fmt.Errorf("scan runner: %w", err)
		}
		m.Runners = append(m.Runners, r)
	}

	return m, rows.Err()
}

func (s *Service) List(ctx context.Context, sport string, status string, inPlay *bool) ([]*models.Market, error) {
	query := "SELECT id, event_id, sport, name, market_type, status, in_play, start_time, total_matched, created_at, updated_at FROM markets WHERE 1=1"
	var args []interface{}
	argIdx := 1

	if sport != "" {
		query += fmt.Sprintf(" AND sport = $%d", argIdx)
		args = append(args, sport)
		argIdx++
	}
	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIdx)
		args = append(args, status)
		argIdx++
	}
	if inPlay != nil {
		query += fmt.Sprintf(" AND in_play = $%d", argIdx)
		args = append(args, *inPlay)
		argIdx++
	}
	query += " ORDER BY start_time DESC LIMIT 100"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list markets: %w", err)
	}
	defer rows.Close()

	var markets []*models.Market
	for rows.Next() {
		m := &models.Market{}
		if err := rows.Scan(&m.ID, &m.EventID, &m.Sport, &m.Name, &m.MarketType,
			&m.Status, &m.InPlay, &m.StartTime, &m.TotalMatched, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan market: %w", err)
		}
		markets = append(markets, m)
	}
	return markets, rows.Err()
}

func (s *Service) UpdateStatus(ctx context.Context, marketID string, status models.MarketStatus) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE markets SET status = $1, updated_at = NOW() WHERE id = $2",
		status, marketID,
	)
	return err
}

func (s *Service) SyncFromProvider(ctx context.Context, markets []*models.Market) (created, updated int, err error) {
	for _, m := range markets {
		existing, getErr := s.Get(ctx, m.ID)
		if getErr != nil {
			// Market doesn't exist, create it
			if createErr := s.Create(ctx, m); createErr != nil {
				return created, updated, createErr
			}
			created++
		} else {
			// Update if status changed
			if existing.Status != m.Status || existing.InPlay != m.InPlay {
				_, updateErr := s.db.ExecContext(ctx,
					"UPDATE markets SET status = $1, in_play = $2, updated_at = NOW() WHERE id = $3",
					m.Status, m.InPlay, m.ID,
				)
				if updateErr != nil {
					return created, updated, updateErr
				}
				updated++
			}
		}
	}

	s.logger.InfoContext(ctx, "markets synced", "created", created, "updated", updated)
	return created, updated, nil
}

// ListSports returns all supported sports.
func (s *Service) ListSports(_ context.Context) ([]models.Sport, error) {
	return models.AllSports(), nil
}

// ListCompetitions fetches competitions for a sport from the database.
func (s *Service) ListCompetitions(ctx context.Context, sport string) ([]*models.Competition, error) {
	query := "SELECT id, sport, name, region, start_date, end_date, status, match_count FROM competitions WHERE 1=1"
	var args []interface{}
	argIdx := 1

	if sport != "" {
		query += fmt.Sprintf(" AND sport = $%d", argIdx)
		args = append(args, sport)
		argIdx++
	}
	_ = argIdx
	query += " ORDER BY start_date DESC LIMIT 100"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list competitions: %w", err)
	}
	defer rows.Close()

	var comps []*models.Competition
	for rows.Next() {
		c := &models.Competition{}
		if err := rows.Scan(&c.ID, &c.Sport, &c.Name, &c.Region, &c.StartDate, &c.EndDate, &c.Status, &c.MatchCount); err != nil {
			return nil, fmt.Errorf("scan competition: %w", err)
		}
		comps = append(comps, c)
	}
	return comps, rows.Err()
}

// ListEvents fetches events for a competition from the database.
func (s *Service) ListEvents(ctx context.Context, competitionID string) ([]*models.Event, error) {
	query := `SELECT id, competition_id, sport, name, home_team, away_team, start_time, status, in_play, score
			  FROM events WHERE competition_id = $1 ORDER BY start_time ASC LIMIT 100`

	rows, err := s.db.QueryContext(ctx, query, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var events []*models.Event
	for rows.Next() {
		e := &models.Event{}
		if err := rows.Scan(&e.ID, &e.CompetitionID, &e.Sport, &e.Name, &e.HomeTeam, &e.AwayTeam, &e.StartTime, &e.Status, &e.InPlay, &e.Score); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// ListEventMarkets returns all markets for a given event.
func (s *Service) ListEventMarkets(ctx context.Context, eventID string) ([]*models.Market, error) {
	query := `SELECT id, event_id, sport, name, market_type, status, in_play, start_time, total_matched, created_at, updated_at
			  FROM markets WHERE event_id = $1 ORDER BY created_at ASC`

	rows, err := s.db.QueryContext(ctx, query, eventID)
	if err != nil {
		return nil, fmt.Errorf("list event markets: %w", err)
	}
	defer rows.Close()

	var markets []*models.Market
	for rows.Next() {
		m := &models.Market{}
		if err := rows.Scan(&m.ID, &m.EventID, &m.Sport, &m.Name, &m.MarketType,
			&m.Status, &m.InPlay, &m.StartTime, &m.TotalMatched, &m.CreatedAt, &m.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan market: %w", err)
		}
		markets = append(markets, m)
	}
	return markets, rows.Err()
}

func (s *Service) RecordMatch(ctx context.Context, marketID string, matchedAmount float64) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE markets SET total_matched = total_matched + $1, updated_at = $2 WHERE id = $3",
		matchedAmount, time.Now(), marketID,
	)
	return err
}
