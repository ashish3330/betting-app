-- Migration 004: Production Hardening
-- Adds KYC, responsible gambling, cashout, session tracking,
-- notifications, promotions, auto-partitioning, and account statement view.

SET search_path TO betting, auth, public;

-- ============================================================
-- 1. KYC Verification columns on auth.users
-- ============================================================

ALTER TABLE auth.users
    ADD COLUMN IF NOT EXISTS kyc_status TEXT DEFAULT 'pending'
        CHECK (kyc_status IN ('pending', 'verified', 'rejected', 'under_review')),
    ADD COLUMN IF NOT EXISTS kyc_document_type TEXT,
    ADD COLUMN IF NOT EXISTS kyc_document_id TEXT,
    ADD COLUMN IF NOT EXISTS kyc_verified_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS kyc_rejection_reason TEXT,
    ADD COLUMN IF NOT EXISTS phone TEXT,
    ADD COLUMN IF NOT EXISTS date_of_birth DATE,
    ADD COLUMN IF NOT EXISTS full_name TEXT;

COMMENT ON COLUMN auth.users.kyc_status IS 'Know Your Customer verification status';
COMMENT ON COLUMN auth.users.kyc_document_type IS 'Type of ID document submitted (e.g. aadhaar, passport, pan)';
COMMENT ON COLUMN auth.users.kyc_document_id IS 'Encrypted document identifier';

-- ============================================================
-- 2. Responsible Gambling
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.responsible_gambling (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES auth.users(id) UNIQUE,
    daily_deposit_limit   NUMERIC(20,2) DEFAULT 100000,
    weekly_deposit_limit  NUMERIC(20,2) DEFAULT 500000,
    monthly_deposit_limit NUMERIC(20,2) DEFAULT 2000000,
    daily_loss_limit      NUMERIC(20,2),
    max_stake_per_bet     NUMERIC(20,2) DEFAULT 500000,
    session_time_limit_mins INT DEFAULT 480,
    self_excluded_until   TIMESTAMPTZ,
    cooling_off_until     TIMESTAMPTZ,
    reality_check_interval_mins INT DEFAULT 60,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_responsible_gambling_user_id
    ON betting.responsible_gambling (user_id);

COMMENT ON TABLE betting.responsible_gambling IS 'Per-user responsible gambling limits and self-exclusion settings';

-- ============================================================
-- 3. Cashout Offers
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.cashout_offers (
    id              TEXT PRIMARY KEY,
    bet_id          TEXT NOT NULL,
    user_id         BIGINT NOT NULL REFERENCES auth.users(id),
    original_stake  NUMERIC(20,2) NOT NULL,
    original_price  NUMERIC(10,2) NOT NULL,
    cashout_amount  NUMERIC(20,2) NOT NULL,
    status          TEXT DEFAULT 'offered'
        CHECK (status IN ('offered', 'accepted', 'expired', 'rejected')),
    offered_at      TIMESTAMPTZ DEFAULT NOW(),
    accepted_at     TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cashout_offers_bet_id
    ON betting.cashout_offers (bet_id);
CREATE INDEX IF NOT EXISTS idx_cashout_offers_user_id
    ON betting.cashout_offers (user_id);
CREATE INDEX IF NOT EXISTS idx_cashout_offers_status
    ON betting.cashout_offers (status);

COMMENT ON TABLE betting.cashout_offers IS 'Real-time cashout offers for in-play and pre-match bets';

-- ============================================================
-- 4. Session Tracking
-- ============================================================

CREATE TABLE IF NOT EXISTS auth.user_sessions (
    id              TEXT PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES auth.users(id),
    ip_address      INET NOT NULL,
    user_agent      TEXT,
    device_fingerprint TEXT,
    login_at        TIMESTAMPTZ DEFAULT NOW(),
    last_active_at  TIMESTAMPTZ DEFAULT NOW(),
    logout_at       TIMESTAMPTZ,
    is_active       BOOLEAN DEFAULT TRUE
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id
    ON auth.user_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_user_sessions_is_active
    ON auth.user_sessions (is_active) WHERE is_active = TRUE;

COMMENT ON TABLE auth.user_sessions IS 'Tracks user login sessions for security and responsible gambling session limits';

-- ============================================================
-- 5. Notification System
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.notifications (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES auth.users(id),
    type            TEXT NOT NULL
        CHECK (type IN (
            'bet_matched', 'bet_settled', 'deposit_complete',
            'withdrawal_complete', 'cashout_available', 'kyc_update',
            'promotion', 'system', 'responsible_gambling'
        )),
    title           TEXT NOT NULL,
    message         TEXT NOT NULL,
    data            JSONB,
    read            BOOLEAN DEFAULT FALSE,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_notifications_user_created
    ON betting.notifications (user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_notifications_read
    ON betting.notifications (read) WHERE read = FALSE;

COMMENT ON TABLE betting.notifications IS 'In-app notification inbox for users';

-- ============================================================
-- 6. Promotions / Bonuses
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.promotions (
    id              TEXT PRIMARY KEY,
    code            TEXT UNIQUE NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    type            TEXT NOT NULL
        CHECK (type IN ('welcome_bonus', 'deposit_bonus', 'cashback', 'free_bet', 'referral')),
    value           NUMERIC(10,2) NOT NULL,
    value_type      TEXT NOT NULL
        CHECK (value_type IN ('percentage', 'fixed')),
    min_deposit     NUMERIC(20,2) DEFAULT 0,
    max_bonus       NUMERIC(20,2),
    wagering_requirement NUMERIC(5,2) DEFAULT 1.0,
    max_uses        INT,
    used_count      INT DEFAULT 0,
    starts_at       TIMESTAMPTZ DEFAULT NOW(),
    expires_at      TIMESTAMPTZ,
    is_active       BOOLEAN DEFAULT TRUE,
    created_at      TIMESTAMPTZ DEFAULT NOW()
);

COMMENT ON TABLE betting.promotions IS 'Available promotional offers and bonus codes';

-- ============================================================
-- 7. User Promotions (redemptions)
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.user_promotions (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES auth.users(id),
    promotion_id    TEXT NOT NULL REFERENCES betting.promotions(id),
    bonus_amount    NUMERIC(20,2) NOT NULL,
    wagering_remaining NUMERIC(20,2) NOT NULL,
    status          TEXT DEFAULT 'active'
        CHECK (status IN ('active', 'completed', 'expired', 'forfeited')),
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    UNIQUE(user_id, promotion_id)
);

COMMENT ON TABLE betting.user_promotions IS 'Tracks individual user promotion redemptions and wagering progress';

-- ============================================================
-- 8. Auto-partition function for betting.bets
-- ============================================================

CREATE OR REPLACE FUNCTION betting.create_monthly_partition()
RETURNS void
LANGUAGE plpgsql
AS $$
DECLARE
    partition_date  DATE;
    partition_name  TEXT;
    start_date      DATE;
    end_date        DATE;
BEGIN
    -- Create partitions for the next 3 months from today
    FOR i IN 0..2 LOOP
        partition_date := date_trunc('month', NOW() + (i || ' months')::interval)::date;
        start_date     := partition_date;
        end_date       := (partition_date + interval '1 month')::date;
        partition_name := 'bets_' || to_char(partition_date, 'YYYY_MM');

        -- Skip if the partition already exists
        IF NOT EXISTS (
            SELECT 1
            FROM pg_catalog.pg_class c
            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
            WHERE n.nspname = 'betting'
              AND c.relname = partition_name
        ) THEN
            EXECUTE format(
                'CREATE TABLE betting.%I PARTITION OF betting.bets FOR VALUES FROM (%L) TO (%L)',
                partition_name,
                start_date,
                end_date
            );
            RAISE NOTICE 'Created partition betting.%', partition_name;
        END IF;
    END LOOP;
END;
$$;

COMMENT ON FUNCTION betting.create_monthly_partition()
    IS 'Auto-creates monthly partitions for betting.bets. Schedule via pg_cron or application timer.';

-- Create any immediately needed partitions
SELECT betting.create_monthly_partition();

-- ============================================================
-- 9. Materialized view: account statement
-- ============================================================

CREATE MATERIALIZED VIEW IF NOT EXISTS betting.mv_account_statement AS
SELECT
    l.user_id,
    l.txn_date                                          AS date,
    COALESCE(opening.balance, 0)                        AS opening_balance,
    COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'deposit'), 0)     AS deposits,
    COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'withdrawal'), 0) AS withdrawals,
    COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'hold'), 0)   AS stakes,
    COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'settlement' AND l.amount > 0), 0) AS winnings,
    COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'commission'), 0) AS commission,
    COALESCE(opening.balance, 0)
        + COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'deposit'), 0)
        - COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'withdrawal'), 0)
        - COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'hold'), 0)
        + COALESCE(SUM(l.amount) FILTER (WHERE l.type = 'settlement'), 0)
        - COALESCE(SUM(ABS(l.amount)) FILTER (WHERE l.type = 'commission'), 0)
                                                        AS closing_balance
FROM (
    SELECT user_id, amount, type, balance,
           created_at::date AS txn_date
    FROM betting.ledger
) l
LEFT JOIN LATERAL (
    -- Opening balance: last ledger entry balance before this day
    SELECT sub.balance
    FROM betting.ledger sub
    WHERE sub.user_id = l.user_id
      AND sub.created_at::date < l.txn_date
    ORDER BY sub.created_at DESC
    LIMIT 1
) opening ON TRUE
GROUP BY l.user_id, l.txn_date, opening.balance
ORDER BY l.user_id, l.txn_date;

CREATE UNIQUE INDEX IF NOT EXISTS idx_mv_account_statement_pk
    ON betting.mv_account_statement (user_id, date);

COMMENT ON MATERIALIZED VIEW betting.mv_account_statement
    IS 'Daily account statement per user. Refresh with: REFRESH MATERIALIZED VIEW CONCURRENTLY betting.mv_account_statement;';
