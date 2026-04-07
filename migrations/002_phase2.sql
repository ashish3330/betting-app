-- Phase 2: Casino, Payments, Fraud tables

-- Set search path
SET search_path TO betting, auth, public;

-- Casino sessions (betting schema)
CREATE TABLE betting.casino_sessions (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    game_type TEXT NOT NULL,
    provider_id TEXT NOT NULL,
    status TEXT DEFAULT 'active' CHECK (status IN ('active', 'expired', 'closed')),
    stream_url TEXT,
    token TEXT NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_casino_sessions_user ON betting.casino_sessions (user_id);
CREATE INDEX idx_casino_sessions_status ON betting.casino_sessions (status) WHERE status = 'active';

-- Casino bets (betting schema)
CREATE TABLE betting.casino_bets (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES betting.casino_sessions(id),
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    game_type TEXT NOT NULL,
    round_id TEXT NOT NULL,
    stake NUMERIC(20,2) NOT NULL,
    payout NUMERIC(20,2) DEFAULT 0,
    status TEXT DEFAULT 'pending',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(session_id, round_id)
);

CREATE INDEX idx_casino_bets_user ON betting.casino_bets (user_id, created_at);
CREATE INDEX idx_casino_bets_session ON betting.casino_bets (session_id);

-- Payment transactions (betting schema)
CREATE TABLE betting.payment_transactions (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    direction TEXT NOT NULL CHECK (direction IN ('deposit', 'withdrawal')),
    method TEXT NOT NULL CHECK (method IN ('upi', 'crypto', 'bank')),
    amount NUMERIC(20,2) NOT NULL CHECK (amount > 0),
    currency TEXT NOT NULL DEFAULT 'INR',
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'failed', 'refunded')),
    provider_ref TEXT,
    upi_id TEXT,
    wallet_address TEXT,
    tx_hash TEXT,
    metadata JSONB,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_payment_user ON betting.payment_transactions (user_id, created_at);
CREATE INDEX idx_payment_status ON betting.payment_transactions (status) WHERE status = 'pending';
CREATE INDEX idx_payment_provider_ref ON betting.payment_transactions (provider_ref) WHERE provider_ref IS NOT NULL;

-- Fraud alerts (betting schema)
CREATE TABLE betting.fraud_alerts (
    id TEXT PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    type TEXT NOT NULL,
    risk_level TEXT NOT NULL CHECK (risk_level IN ('low', 'medium', 'high', 'critical')),
    details TEXT,
    score NUMERIC(5,2) DEFAULT 0,
    resolved BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_fraud_alerts_user ON betting.fraud_alerts (user_id);
CREATE INDEX idx_fraud_alerts_unresolved ON betting.fraud_alerts (resolved, created_at) WHERE resolved = FALSE;
CREATE INDEX idx_fraud_alerts_risk ON betting.fraud_alerts (risk_level);

-- Audit log (betting schema, immutable)
CREATE TABLE betting.audit_log (
    id BIGSERIAL PRIMARY KEY,
    actor_id BIGINT REFERENCES auth.users(id),
    action TEXT NOT NULL,
    entity_type TEXT NOT NULL,
    entity_id TEXT NOT NULL,
    old_value JSONB,
    new_value JSONB,
    ip_address INET,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_audit_actor ON betting.audit_log (actor_id, created_at);
CREATE INDEX idx_audit_entity ON betting.audit_log (entity_type, entity_id);
