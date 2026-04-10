package main

// Bundled services wiring for the Lotus Exchange monolith.
//
// This file is the incremental-migration bridge between the legacy in-memory
// `Store` and the per-domain services in `internal/*`. The monolith still
// owns most request handlers, but any handler listed in the migration status
// block at the bottom of main.go now delegates to the bundled service handler
// below.
//
// Design:
//   - One `bundledServices` struct wires up the services we want to share
//     with the per-service binaries (cmd/*-service). They reuse the same
//     `*sql.DB` that `cmd/server` already opens in db.go.
//   - Services that need dependencies we do not yet instantiate in the
//     monolith (Redis, ClickHouse, etc.) are left nil and are opt-in: only
//     wire them up when a handler that needs them is migrated.
//   - The bundle is created once in main() after initDB() and held in the
//     `bundled` package variable.

import (
	"log/slog"

	"github.com/lotus-exchange/lotus-exchange/internal/notification"
)

// bundledServices holds long-lived service instances shared across bundled
// HTTP handlers. Fields are added incrementally as handlers are migrated from
// the in-memory Store to internal/*.
type bundledServices struct {
	notif        *notification.Service
	notifHandler *notification.Handler
}

// bundled is the monolith-wide bundle. It is nil until initBundledServices
// has been called (after initDB).
var bundled *bundledServices

// initBundledServices constructs all internal services that the monolith
// currently wires up. It is a no-op when the DB connection is unavailable —
// the legacy in-memory handlers continue to serve requests in that case.
func initBundledServices(log *slog.Logger) {
	if !useDB() || db == nil {
		log.Info("bundled services skipped — no database connection")
		return
	}

	b := &bundledServices{}

	// ── Notification ──
	// Only needs *sql.DB + logger. Reads/writes the betting.notifications
	// table that already exists in the schema.
	b.notif = notification.NewService(db, log)
	b.notifHandler = notification.NewHandler(b.notif)

	bundled = b
	log.Info("bundled services initialised", "services", []string{"notification"})
}
