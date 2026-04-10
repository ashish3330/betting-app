// Package service contains shared boilerplate for Lotus Exchange micro-services:
// database/redis/nats setup, health endpoints, middleware chains, and the HTTP
// runtime with graceful shutdown. Its goal is to keep each cmd/*-service/main.go
// small and focused on service-specific wiring only.
package service

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"strings"
	"time"

	// Register the postgres driver once here so callers don't need to do so.
	_ "github.com/lib/pq"
)

// defaultSearchPath is the schema search order used by every Lotus Exchange
// service that talks to Postgres. It mirrors the layout produced by the
// migrations in migrations/.
const defaultSearchPath = "betting,auth,public"

// OpenPostgres opens a pooled Postgres connection using the standard
// lotus-exchange defaults:
//
//  1. Ensures the DSN includes search_path=betting,auth,public (existing
//     search_path values are respected).
//  2. Applies the supplied pool sizing and connection lifetime.
//  3. Pings the database with a 5s timeout derived from ctx so a broken
//     deployment fails fast at startup rather than on first request.
//
// Callers retain ownership of the returned *sql.DB and must Close it.
func OpenPostgres(ctx context.Context, dsn string, maxOpen, maxIdle int, lifetime time.Duration) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("service: postgres DSN is empty")
	}

	dsn = appendSearchPath(dsn)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("service: sql.Open postgres: %w", err)
	}

	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(lifetime)

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("service: postgres ping: %w", err)
	}

	return db, nil
}

// appendSearchPath injects search_path=betting,auth,public into the DSN if the
// caller hasn't already specified one. It supports both URL-style DSNs
// (postgres://user:pass@host/db?...) and keyword-style DSNs
// (host=... dbname=... search_path=...).
func appendSearchPath(dsn string) string {
	// Keyword-style DSN detection: treat as keyword form if it doesn't start
	// with a scheme. A keyword DSN like "host=foo dbname=bar" will never parse
	// as a valid URL with a scheme.
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		if strings.Contains(dsn, "search_path") {
			return dsn
		}
		// Keyword-style values are space separated.
		return strings.TrimSpace(dsn) + " search_path=" + defaultSearchPath
	}

	u, err := url.Parse(dsn)
	if err != nil {
		// Fall back to naive appending when url.Parse fails so we don't break
		// unusual but still-valid DSNs.
		if strings.Contains(dsn, "search_path") {
			return dsn
		}
		sep := "?"
		if strings.Contains(dsn, "?") {
			sep = "&"
		}
		return dsn + sep + "search_path=" + defaultSearchPath
	}

	q := u.Query()
	if q.Get("search_path") != "" {
		return dsn
	}
	q.Set("search_path", defaultSearchPath)
	u.RawQuery = q.Encode()
	return u.String()
}
