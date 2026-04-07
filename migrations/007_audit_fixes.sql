-- Performance indexes for 10k bets/2sec (non-concurrent for partitioned tables)
CREATE INDEX IF NOT EXISTS idx_bets_user_created ON betting.bets(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_bets_market_status ON betting.bets(market_id, status);
CREATE INDEX IF NOT EXISTS idx_bets_status_created ON betting.bets(status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_user_created ON betting.ledger(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_user_created ON betting.notifications(user_id, created_at DESC);

-- Add missing columns
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS referral_code TEXT DEFAULT '';
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS otp_enabled BOOLEAN DEFAULT false;
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS is_demo BOOLEAN DEFAULT false;
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS force_password_change BOOLEAN DEFAULT false;

-- Unique partial index for client_ref idempotency (must include partition key)
CREATE UNIQUE INDEX IF NOT EXISTS idx_bets_client_ref_unique ON betting.bets(client_ref, created_at) WHERE client_ref IS NOT NULL AND client_ref != '';
