-- Add market_type and display_side to bets for fancy/session differentiation
ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS market_type TEXT DEFAULT 'match_odds';
ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS display_side TEXT;

-- Update display_side for existing bets based on market conventions
-- (back -> back, lay -> lay for match_odds; back -> yes, lay -> no for fancy/session)

-- Index for filtering bets by market_type
CREATE INDEX IF NOT EXISTS idx_bets_market_type ON betting.bets(market_type);

-- Add composite index for user bets filtered by market_type
CREATE INDEX IF NOT EXISTS idx_bets_user_market_type ON betting.bets(user_id, market_type, created_at DESC);
