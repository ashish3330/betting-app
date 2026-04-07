-- ══════════════════════════════════════════════════════════════════
-- Migration 006: Deposit Payment Module
-- Tracks bank accounts per Master/Agent with 90K INR daily limit
-- Timezone: Asia/Kolkata (IST)
-- ══════════════════════════════════════════════════════════════════

SET search_path TO betting, auth, public;
SET timezone TO 'Asia/Kolkata';

-- ── Bank Accounts (owned by Master or Agent) ────────────────────

CREATE TABLE IF NOT EXISTS betting.bank_accounts (
    id              BIGSERIAL PRIMARY KEY,
    owner_id        BIGINT NOT NULL REFERENCES auth.users(id),
    owner_role      VARCHAR(10) NOT NULL CHECK (owner_role IN ('master', 'agent')),

    -- Bank details
    bank_name       VARCHAR(200) NOT NULL,
    account_holder  VARCHAR(200) NOT NULL,
    account_number  VARCHAR(30) NOT NULL,
    ifsc_code       VARCHAR(11) NOT NULL,
    upi_id          VARCHAR(100),
    qr_image_url    VARCHAR(500),

    -- Limits & status
    daily_limit     NUMERIC(12,2) NOT NULL DEFAULT 90000.00,
    status          VARCHAR(10) NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),

    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_bank_accounts_owner ON betting.bank_accounts(owner_id, owner_role);
CREATE INDEX idx_bank_accounts_status ON betting.bank_accounts(status) WHERE status = 'active';

COMMENT ON TABLE betting.bank_accounts IS 'Bank accounts managed by Masters/Agents for player deposits. Each has a 90K INR daily limit.';

-- ── Daily Account Usage (tracks deposits per account per day IST) ──

CREATE TABLE IF NOT EXISTS betting.daily_account_usage (
    id              BIGSERIAL PRIMARY KEY,
    bank_account_id BIGINT NOT NULL REFERENCES betting.bank_accounts(id),
    usage_date      DATE NOT NULL,  -- Calendar date in IST
    total_used      NUMERIC(12,2) NOT NULL DEFAULT 0.00,
    deposit_count   INT NOT NULL DEFAULT 0,
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    UNIQUE(bank_account_id, usage_date)
);

CREATE INDEX idx_daily_usage_date ON betting.daily_account_usage(usage_date);
CREATE INDEX idx_daily_usage_account ON betting.daily_account_usage(bank_account_id, usage_date);

COMMENT ON TABLE betting.daily_account_usage IS 'Tracks total deposits received per bank account per calendar day (IST). Used to enforce 90K daily limit.';

-- ── Deposit Requests ────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS betting.deposit_requests (
    id              BIGSERIAL PRIMARY KEY,
    player_id       BIGINT NOT NULL REFERENCES auth.users(id),
    agent_id        BIGINT NOT NULL REFERENCES auth.users(id),
    master_id       BIGINT NOT NULL REFERENCES auth.users(id),
    bank_account_id BIGINT NOT NULL REFERENCES betting.bank_accounts(id),

    -- Amount requested
    amount          NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    currency        VARCHAR(3) NOT NULL DEFAULT 'INR',

    -- Status flow: pending → confirmed / rejected / expired
    status          VARCHAR(15) NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'confirmed', 'rejected', 'expired')),

    -- Confirmation details (filled by Agent/Master)
    confirmed_by    BIGINT REFERENCES auth.users(id),
    confirmed_at    TIMESTAMP WITH TIME ZONE,
    txn_reference   VARCHAR(100),  -- UTR / transaction reference
    rejection_reason VARCHAR(500),

    -- Metadata
    ip_address      VARCHAR(45),
    device_info     VARCHAR(200),
    created_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

CREATE INDEX idx_deposit_requests_player ON betting.deposit_requests(player_id, status);
CREATE INDEX idx_deposit_requests_agent ON betting.deposit_requests(agent_id, status);
CREATE INDEX idx_deposit_requests_master ON betting.deposit_requests(master_id, status);
CREATE INDEX idx_deposit_requests_bank ON betting.deposit_requests(bank_account_id, created_at);
CREATE INDEX idx_deposit_requests_pending ON betting.deposit_requests(status) WHERE status = 'pending';

COMMENT ON TABLE betting.deposit_requests IS 'Player deposit requests. Agent/Master manually confirms after verifying payment.';

-- ── Daily Reset Job (run at 00:00 IST via cron) ────────────────
-- No need to delete rows — the usage_date column naturally partitions by day.
-- Queries always filter by usage_date = CURRENT_DATE AT TIME ZONE 'Asia/Kolkata'.
-- Old rows serve as audit trail. Optionally archive rows older than 90 days.

-- ══════════════════════════════════════════════════════════════════
-- SEED: Insert no data — Masters/Agents create accounts via UI
-- ══════════════════════════════════════════════════════════════════
