-- Migration 017: betting.settlement_idempotency
--
-- Settlement idempotency guard. SettleMarket is gated by an INSERT into
-- this table on the unique primary-key "key". A duplicate settle (network
-- retry, double admin click, NATS at-least-once redelivery) hits the
-- unique constraint, the second settle becomes a no-op, and the stored
-- (bets_settled, payout) pair is returned to the caller so retries see
-- the same result. Without this table the settlement handler would
-- re-walk the bet list and double-pay every settled bet on the second
-- call. Created by the deleted cmd/server runMigrations loop and never
-- ported to a numbered migration.
--
-- Also adds betting.bet_fills, which the matching engine writes one row
-- per fill into (buyer+seller bet_ids, price, size). Lives next to the
-- settlement table because both are written by the matching-engine side
-- of the bet lifecycle.
--
-- Idempotent: safe to re-run.

SET search_path TO betting, auth, public;

-- ── Settlement idempotency ───────────────────────────────────────
CREATE TABLE IF NOT EXISTS betting.settlement_idempotency (
    key                 TEXT PRIMARY KEY,
    market_id           TEXT NOT NULL,
    winner_selection_id BIGINT NOT NULL,
    bets_settled        INT,
    payout              NUMERIC(20,2),
    settled_at          TIMESTAMPTZ DEFAULT NOW(),
    settled_by          BIGINT
);

CREATE INDEX IF NOT EXISTS idx_settlement_idem_market
    ON betting.settlement_idempotency (market_id);

COMMENT ON TABLE betting.settlement_idempotency
    IS 'Primary-key insert guards SettleMarket against double-payout on retry.';

-- ── Per-fill rows produced by the matching engine ────────────────
CREATE TABLE IF NOT EXISTS betting.bet_fills (
    id             BIGSERIAL PRIMARY KEY,
    bet_id         TEXT NOT NULL,
    counter_bet_id TEXT NOT NULL,
    price          NUMERIC(10,2) NOT NULL,
    size           NUMERIC(20,2) NOT NULL,
    created_at     TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_bet_fills_bet_id  ON betting.bet_fills (bet_id);
CREATE INDEX IF NOT EXISTS idx_bet_fills_counter ON betting.bet_fills (counter_bet_id);

COMMENT ON TABLE betting.bet_fills
    IS 'Per-fill rows written by the matching engine: one row per price-size trade between two bets.';
