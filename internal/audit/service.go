package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/lotus-exchange/lotus-exchange/internal/middleware"
)

type Service struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewService(db *sql.DB, logger *slog.Logger) *Service {
	return &Service{db: db, logger: logger}
}

type AuditEntry struct {
	ActorID    int64       `json:"actor_id"`
	Action     string      `json:"action"`
	EntityType string      `json:"entity_type"`
	EntityID   string      `json:"entity_id"`
	OldValue   interface{} `json:"old_value,omitempty"`
	NewValue   interface{} `json:"new_value,omitempty"`
	IPAddress  string      `json:"ip_address,omitempty"`
}

func (s *Service) Log(ctx context.Context, entry *AuditEntry) {
	oldJSON, _ := json.Marshal(entry.OldValue)
	newJSON, _ := json.Marshal(entry.NewValue)

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (actor_id, action, entity_type, entity_id, old_value, new_value, ip_address, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7::inet, NOW())`,
		entry.ActorID, entry.Action, entry.EntityType, entry.EntityID,
		oldJSON, newJSON, nullIfEmpty(entry.IPAddress),
	)
	if err != nil {
		s.logger.ErrorContext(ctx, "failed to write audit log",
			"error", err, "action", entry.Action, "entity", entry.EntityType)
	}
}

func (s *Service) LogFromRequest(r *http.Request, action, entityType, entityID string, oldValue, newValue interface{}) {
	actorID := middleware.UserIDFromContext(r.Context())
	// Actually get IP from request
	reqIP := r.Header.Get("X-Real-IP")
	if reqIP == "" {
		reqIP = r.RemoteAddr
	}

	s.Log(r.Context(), &AuditEntry{
		ActorID:    actorID,
		Action:     action,
		EntityType: entityType,
		EntityID:   entityID,
		OldValue:   oldValue,
		NewValue:   newValue,
		IPAddress:  reqIP,
	})
}

func (s *Service) GetAuditLog(ctx context.Context, entityType, entityID string, limit int) ([]map[string]interface{}, error) {
	query := `SELECT actor_id, action, entity_type, entity_id, old_value, new_value, ip_address, created_at
	          FROM audit_log WHERE 1=1`
	var args []interface{}
	argIdx := 1

	if entityType != "" {
		query += " AND entity_type = $1"
		args = append(args, entityType)
		argIdx++
	}
	if entityID != "" {
		query += " AND entity_id = $" + string(rune('0'+argIdx))
		args = append(args, entityID)
		argIdx++
	}

	query += " ORDER BY created_at DESC LIMIT $" + string(rune('0'+argIdx))
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]interface{}
	for rows.Next() {
		var actorID int64
		var action, entType, entID string
		var oldVal, newVal sql.NullString
		var ipAddr sql.NullString
		var createdAt string

		if err := rows.Scan(&actorID, &action, &entType, &entID, &oldVal, &newVal, &ipAddr, &createdAt); err != nil {
			return nil, err
		}

		entry := map[string]interface{}{
			"actor_id":    actorID,
			"action":      action,
			"entity_type": entType,
			"entity_id":   entID,
			"created_at":  createdAt,
		}
		if ipAddr.Valid {
			entry["ip_address"] = ipAddr.String
		}
		if oldVal.Valid {
			entry["old_value"] = json.RawMessage(oldVal.String)
		}
		if newVal.Valid {
			entry["new_value"] = json.RawMessage(newVal.String)
		}

		results = append(results, entry)
	}
	return results, rows.Err()
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
