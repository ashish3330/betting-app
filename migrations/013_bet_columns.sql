-- Migration 013: betting.bets missing columns
--
-- Adds market_type and display_side, which the deleted cmd/server runMigrations
-- injected at boot. Neither column exists in migration 001_initial.sql and they
-- were never ported to a numbered migration, so on a brand-new database created
-- from this directory the bet INSERT in the matching engine silently fails with
-- 42703 ("column does not exist") and bets vanish after the in-memory store
-- cycles.
--
-- Idempotent: safe to re-run.

SET search_path TO betting, auth, public;

-- bets may be missing market_type from the legacy (pre-fancy) schema.
-- Matching engine writes it on every INSERT.
ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS market_type TEXT DEFAULT '';

-- display_side stores "yes"/"no" for fancy/session markets, "back"/"lay"
-- otherwise. Without this column dbSaveBet silently drops every bet.
ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS display_side TEXT DEFAULT '';

-- settled_at already exists in 001_initial.sql as part of the partitioned
-- CREATE TABLE, but make the safety net explicit so a fresh install never
-- ends up missing it.
ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ;
