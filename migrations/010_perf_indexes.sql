-- Migration 010: Performance indexes identified by audit
--
-- These indexes cover hot-path queries that were previously doing
-- sequential scans or were missing partial-index optimizations. Each
-- CREATE is IF NOT EXISTS so the migration is safe to re-run.

-- ---------------------------------------------------------------------------
-- bet_fills table
-- ---------------------------------------------------------------------------
-- The matching engine persists per-match fill rows to this table. It existed
-- implicitly in code but never had a migration; create it here so both the
-- microservice and monolith have a consistent schema.
CREATE TABLE IF NOT EXISTS betting.bet_fills (
    id             BIGSERIAL PRIMARY KEY,
    bet_id         TEXT NOT NULL,
    counter_bet_id TEXT NOT NULL,
    price          NUMERIC(10,2) NOT NULL,
    size           NUMERIC(20,2) NOT NULL,
    created_at     TIMESTAMPTZ DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_bet_fills_bet_id  ON betting.bet_fills(bet_id);
CREATE INDEX IF NOT EXISTS idx_bet_fills_counter ON betting.bet_fills(counter_bet_id);

-- ---------------------------------------------------------------------------
-- Hot-path indexes
-- ---------------------------------------------------------------------------

-- Bets: cover settled_at queries (responsible gambling daily-loss check).
CREATE INDEX IF NOT EXISTS idx_bets_user_settled
    ON betting.bets(user_id, settled_at) WHERE status = 'settled';

-- Bets: cover ClickHouse ingestion (ORDER BY settled_at).
CREATE INDEX IF NOT EXISTS idx_bets_settled_at
    ON betting.bets(settled_at) WHERE status = 'settled';

-- Users: status filter on dashboard (most rows are 'active', partial index
-- keeps the tree tiny and ensures it's actually used).
CREATE INDEX IF NOT EXISTS idx_users_status_active
    ON auth.users(status) WHERE status = 'active';

-- Markets: ORDER BY start_time is the default listing order.
CREATE INDEX IF NOT EXISTS idx_markets_start_time
    ON betting.markets(start_time DESC);

-- Events: ORDER BY start_time when no other filter is applied.
CREATE INDEX IF NOT EXISTS idx_events_start_time
    ON betting.events(start_time);

-- Settlement events outbox: pending rows, scanned in id order.
CREATE INDEX IF NOT EXISTS idx_settlement_events_pending_id
    ON betting.settlement_events(id) WHERE status = 'pending';

-- Casino sessions: active session lookup per user (responsible gambling).
CREATE INDEX IF NOT EXISTS idx_casino_sessions_user_active
    ON betting.casino_sessions(user_id) WHERE status = 'active';

-- Notifications: user + unread for navbar badge counts.
CREATE INDEX IF NOT EXISTS idx_notifications_user_unread
    ON betting.notifications(user_id, created_at DESC) WHERE read = false;

-- Audit log: user_id with created_at/id ordering (most queries are
-- "latest N entries for user X").
CREATE INDEX IF NOT EXISTS idx_audit_user_id_desc
    ON betting.audit_log(user_id, id DESC);

-- Fraud alerts: created_at for the resolved/unresolved dashboard queries.
CREATE INDEX IF NOT EXISTS idx_fraud_alerts_created
    ON betting.fraud_alerts(created_at DESC);
