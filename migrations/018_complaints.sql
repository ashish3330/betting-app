-- Migration 018: betting.complaints
--
-- Player complaint workflow used by the compliance module. Lifecycle:
--
--     open  →  investigating  →  resolved | rejected
--
-- The table was originally created lazily from the compliance handler
-- (EnsureComplaintsSchema, called from main() after runMigrations).
-- With the monolith gone and the handler moved into the compliance
-- service, the canonical DDL belongs in a numbered migration.
--
-- Idempotent: safe to re-run.

SET search_path TO betting, auth, public;

CREATE TABLE IF NOT EXISTS betting.complaints (
    id               BIGSERIAL PRIMARY KEY,
    user_id          BIGINT NOT NULL,
    subject          TEXT NOT NULL,
    body             TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'open',
    priority         TEXT NOT NULL DEFAULT 'normal',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at      TIMESTAMPTZ,
    resolver_id      BIGINT,
    resolution_notes TEXT
);

CREATE INDEX IF NOT EXISTS idx_complaints_user   ON betting.complaints (user_id);
CREATE INDEX IF NOT EXISTS idx_complaints_status ON betting.complaints (status);

COMMENT ON TABLE betting.complaints IS 'Player complaint tickets with open/investigating/resolved/rejected lifecycle.';
