-- Migration 015: betting.casino_sessions safety net
--
-- Migration 002_phase2.sql creates betting.casino_sessions with the full
-- (id, user_id, game_type, provider_id, status, stream_url, token,
-- created_at, expires_at) shape used by the casino launch flow. This file
-- is a self-contained idempotent re-declaration of the same table plus
-- the partial-index used by the active-session lookup, included so that:
--
--   1. A fresh install run against only migrations 012+ (e.g. for a
--      microservice-specific dev DB) still ends up with a valid table.
--   2. A legacy install that somehow dropped the partial index picks it
--      back up.
--
-- Every statement uses CREATE ... IF NOT EXISTS and is safe to re-run.

SET search_path TO betting, auth, public;

CREATE TABLE IF NOT EXISTS betting.casino_sessions (
    id          TEXT PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES auth.users(id),
    game_type   TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    stream_url  TEXT DEFAULT '',
    token       TEXT DEFAULT '',
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);

-- Indexes used by casino.ListActiveSessions and responsible-gambling
-- session-time enforcement.
CREATE INDEX IF NOT EXISTS idx_casino_sessions_user
    ON betting.casino_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_casino_sessions_user_active
    ON betting.casino_sessions (user_id) WHERE status = 'active';
