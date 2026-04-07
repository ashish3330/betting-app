-- Lotus Exchange: Initial Schema
-- PostgreSQL 16 with ltree extension

-- Create schemas
CREATE SCHEMA IF NOT EXISTS betting;
CREATE SCHEMA IF NOT EXISTS auth;

-- Set search path
SET search_path TO betting, auth, public;

CREATE EXTENSION IF NOT EXISTS ltree;

-- Users table with ltree hierarchy (auth schema)
CREATE TABLE auth.users (
    id BIGSERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    path LTREE NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('superadmin', 'admin', 'master', 'agent', 'client')),
    parent_id BIGINT REFERENCES auth.users(id),
    balance NUMERIC(20,2) DEFAULT 0 CHECK (balance >= 0),
    exposure NUMERIC(20,2) DEFAULT 0 CHECK (exposure >= 0),
    credit_limit NUMERIC(20,2) DEFAULT 0,
    commission_rate NUMERIC(5,2) DEFAULT 0,
    status TEXT DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'blocked', 'inactive')),
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_users_path ON auth.users USING GIST (path);
CREATE INDEX idx_users_parent_id ON auth.users (parent_id);
CREATE INDEX idx_users_role ON auth.users (role);
CREATE INDEX idx_users_username ON auth.users (username);

-- Markets table (betting schema)
CREATE TABLE betting.markets (
    id TEXT PRIMARY KEY,
    event_id TEXT NOT NULL,
    sport TEXT NOT NULL,
    name TEXT NOT NULL,
    market_type TEXT NOT NULL CHECK (market_type IN ('match_odds', 'fancy', 'player_prop', 'bookmaker')),
    status TEXT DEFAULT 'open' CHECK (status IN ('open', 'suspended', 'closed', 'settled', 'void')),
    in_play BOOLEAN DEFAULT FALSE,
    start_time TIMESTAMPTZ,
    total_matched NUMERIC(20,2) DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_markets_sport ON betting.markets (sport);
CREATE INDEX idx_markets_status ON betting.markets (status);
CREATE INDEX idx_markets_event_id ON betting.markets (event_id);
CREATE INDEX idx_markets_in_play ON betting.markets (in_play) WHERE in_play = TRUE;

-- Runners table (betting schema)
CREATE TABLE betting.runners (
    id BIGSERIAL PRIMARY KEY,
    market_id TEXT NOT NULL REFERENCES betting.markets(id),
    selection_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    status TEXT DEFAULT 'active',
    UNIQUE(market_id, selection_id)
);

-- Bets table (betting schema, partitioned by month)
CREATE TABLE betting.bets (
    id TEXT NOT NULL,
    market_id TEXT NOT NULL,
    selection_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    side TEXT NOT NULL CHECK (side IN ('back', 'lay')),
    price NUMERIC(10,2) NOT NULL CHECK (price > 1),
    stake NUMERIC(20,2) NOT NULL CHECK (stake > 0),
    matched_stake NUMERIC(20,2) DEFAULT 0,
    unmatched_stake NUMERIC(20,2) DEFAULT 0,
    profit NUMERIC(20,2) DEFAULT 0,
    status TEXT DEFAULT 'pending' CHECK (status IN ('pending', 'matched', 'partial', 'unmatched', 'cancelled', 'settled', 'void')),
    client_ref TEXT,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    matched_at TIMESTAMPTZ,
    settled_at TIMESTAMPTZ,
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- Create partitions for current and next few months
CREATE TABLE betting.bets_2026_01 PARTITION OF betting.bets FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE betting.bets_2026_02 PARTITION OF betting.bets FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE betting.bets_2026_03 PARTITION OF betting.bets FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE betting.bets_2026_04 PARTITION OF betting.bets FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE betting.bets_2026_05 PARTITION OF betting.bets FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE betting.bets_2026_06 PARTITION OF betting.bets FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE betting.bets_2026_07 PARTITION OF betting.bets FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE betting.bets_2026_08 PARTITION OF betting.bets FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE betting.bets_2026_09 PARTITION OF betting.bets FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE betting.bets_2026_10 PARTITION OF betting.bets FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE betting.bets_2026_11 PARTITION OF betting.bets FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE betting.bets_2026_12 PARTITION OF betting.bets FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

CREATE INDEX idx_bets_market_id ON betting.bets (market_id, created_at);
CREATE INDEX idx_bets_user_id ON betting.bets (user_id, created_at);
CREATE INDEX idx_bets_status ON betting.bets (status, created_at);
CREATE INDEX idx_bets_client_ref ON betting.bets (client_ref, created_at);

-- Ledger table (betting schema, immutable audit trail)
CREATE TABLE betting.ledger (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES auth.users(id),
    amount NUMERIC(20,2) NOT NULL,
    type TEXT NOT NULL CHECK (type IN ('hold', 'release', 'settlement', 'commission', 'deposit', 'withdrawal', 'transfer')),
    reference TEXT UNIQUE NOT NULL,
    bet_id TEXT,
    market_id TEXT,
    balance NUMERIC(20,2) DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_ledger_user_id ON betting.ledger (user_id, created_at);
CREATE INDEX idx_ledger_type ON betting.ledger (type, created_at);
CREATE INDEX idx_ledger_bet_id ON betting.ledger (bet_id) WHERE bet_id IS NOT NULL;

-- Seed SuperAdmin
INSERT INTO auth.users (username, email, password_hash, path, role, balance, credit_limit, status)
VALUES (
    'superadmin',
    'admin@lotusexchange.com',
    -- Default password: "admin123" (change in production)
    'a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4:abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789',
    '1',
    'superadmin',
    1000000.00,
    10000000.00,
    'active'
);

-- Update path after insert
UPDATE auth.users SET path = id::text::ltree WHERE id = 1;
