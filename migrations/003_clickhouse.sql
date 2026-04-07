-- ClickHouse Analytics Schema
-- Run against ClickHouse, NOT PostgreSQL

CREATE TABLE IF NOT EXISTS raw_bets (
    timestamp DateTime64(3),
    bet_id String,
    market_id String,
    user_id UInt64,
    side String,
    price Float64,
    stake Float64,
    matched_stake Float64,
    profit Float64,
    status String,
    matched_at Nullable(DateTime64(3)),
    settled_at Nullable(DateTime64(3))
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS market_snapshots (
    timestamp DateTime64(3),
    market_id String,
    total_matched Float64,
    back_volume Float64,
    lay_volume Float64,
    best_back_price Float64,
    best_lay_price Float64,
    spread Float64,
    active_orders UInt32
) ENGINE = MergeTree()
PARTITION BY toYYYYMM(timestamp)
ORDER BY (market_id, timestamp);

CREATE TABLE IF NOT EXISTS audit_events (
    timestamp DateTime64(3),
    actor_id UInt64,
    action String,
    entity_type String,
    entity_id String,
    details String,
    ip_address String,
    user_agent String
) ENGINE = MergeTree()
PARTITION BY toYYYYMMDD(timestamp)
ORDER BY (actor_id, timestamp);

-- Materialized views for dashboards

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_daily_pnl
ENGINE = SummingMergeTree()
ORDER BY (user_id, day)
AS SELECT
    user_id,
    toDate(timestamp) AS day,
    count() AS total_bets,
    sum(stake) AS total_stake,
    sum(profit) AS total_profit
FROM raw_bets
WHERE status = 'settled'
GROUP BY user_id, day;

CREATE MATERIALIZED VIEW IF NOT EXISTS mv_hourly_volume
ENGINE = SummingMergeTree()
ORDER BY (market_id, hour)
AS SELECT
    market_id,
    toStartOfHour(timestamp) AS hour,
    count() AS bet_count,
    sum(stake) AS volume,
    sum(matched_stake) AS matched_volume
FROM raw_bets
GROUP BY market_id, hour;
