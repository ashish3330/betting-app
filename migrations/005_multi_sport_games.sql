-- Migration 005: Multi-Sport, Casino Games & Production Hardening
-- Adds sports reference data, competitions, events hierarchy,
-- casino games/providers, and settlement outbox pattern.

SET search_path TO betting, auth, public;

-- ============================================================
-- 1. Sports Reference Table
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.sports (
    id          VARCHAR(50) PRIMARY KEY,
    name        VARCHAR(100) NOT NULL,
    slug        VARCHAR(50) NOT NULL UNIQUE,
    active      BOOLEAN DEFAULT true,
    sort_order  INT DEFAULT 0,
    icon_url    VARCHAR(500),
    created_at  TIMESTAMP DEFAULT NOW()
);

COMMENT ON TABLE betting.sports IS 'Master list of supported sports for the exchange';

INSERT INTO betting.sports (id, name, slug, sort_order) VALUES
    ('cricket',       'Cricket',        'cricket',        1),
    ('football',      'Football',       'football',       2),
    ('tennis',        'Tennis',         'tennis',         3),
    ('horse_racing',  'Horse Racing',   'horse-racing',   4),
    ('kabaddi',       'Kabaddi',        'kabaddi',        5),
    ('basketball',    'Basketball',     'basketball',     6),
    ('table_tennis',  'Table Tennis',   'table-tennis',   7),
    ('esports',       'Esports',        'esports',        8),
    ('volleyball',    'Volleyball',     'volleyball',     9),
    ('ice_hockey',    'Ice Hockey',     'ice-hockey',    10),
    ('boxing',        'Boxing',         'boxing',        11),
    ('mma',           'MMA',            'mma',           12),
    ('badminton',     'Badminton',      'badminton',     13),
    ('golf',          'Golf',           'golf',          14)
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- 2. Competitions Table
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.competitions (
    id          VARCHAR(100) PRIMARY KEY,
    sport_id    VARCHAR(50) REFERENCES betting.sports(id),
    name        VARCHAR(200) NOT NULL,
    region      VARCHAR(100),
    start_date  DATE,
    end_date    DATE,
    status      VARCHAR(20) DEFAULT 'upcoming',
    match_count INT DEFAULT 0,
    created_at  TIMESTAMP DEFAULT NOW(),
    updated_at  TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_competitions_sport
    ON betting.competitions(sport_id, status);

COMMENT ON TABLE betting.competitions IS 'Tournaments, leagues, and series within a sport';

-- ============================================================
-- 3. Events Table
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.events (
    id              VARCHAR(100) PRIMARY KEY,
    competition_id  VARCHAR(100) REFERENCES betting.competitions(id),
    sport_id        VARCHAR(50) REFERENCES betting.sports(id),
    name            VARCHAR(300) NOT NULL,
    home_team       VARCHAR(200),
    away_team       VARCHAR(200),
    start_time      TIMESTAMP NOT NULL,
    status          VARCHAR(20) DEFAULT 'upcoming',
    in_play         BOOLEAN DEFAULT false,
    score           JSONB,
    created_at      TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_events_competition
    ON betting.events(competition_id, start_time);

CREATE INDEX idx_events_sport_status
    ON betting.events(sport_id, status, start_time);

CREATE INDEX idx_events_inplay
    ON betting.events(in_play) WHERE in_play = true;

COMMENT ON TABLE betting.events IS 'Individual matches/fixtures linked to a competition and sport';

-- ============================================================
-- 4. Extend Markets Table with Foreign Keys to Events & Sports
-- ============================================================
-- The original markets table uses event_id TEXT and sport TEXT.
-- Add proper FK columns that reference the new normalised tables.

ALTER TABLE betting.markets
    ADD COLUMN IF NOT EXISTS event_id VARCHAR(100) REFERENCES betting.events(id);

ALTER TABLE betting.markets
    ADD COLUMN IF NOT EXISTS sport_id VARCHAR(50) REFERENCES betting.sports(id);

CREATE INDEX IF NOT EXISTS idx_markets_event
    ON betting.markets(event_id);

CREATE INDEX IF NOT EXISTS idx_markets_sport_status
    ON betting.markets(sport_id, status);

-- ============================================================
-- 5. Casino Providers Table (created before games for FK)
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.casino_providers (
    id              VARCHAR(50) PRIMARY KEY,
    name            VARCHAR(200) NOT NULL,
    api_base_url    VARCHAR(500),
    stream_base_url VARCHAR(500),
    webhook_secret  VARCHAR(200),
    active          BOOLEAN DEFAULT true,
    created_at      TIMESTAMP DEFAULT NOW()
);

COMMENT ON TABLE betting.casino_providers IS 'Third-party casino game providers (Evolution, Ezugi, etc.)';

-- Seed casino providers
INSERT INTO betting.casino_providers (id, name) VALUES
    ('evolution',      'Evolution Gaming'),
    ('ezugi',          'Ezugi Live'),
    ('betgames',       'BetGames TV'),
    ('superspade',     'Super Spade Games'),
    ('pragmatic_play', 'Pragmatic Play')
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- 6. Casino Games Reference Table
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.casino_games (
    id          VARCHAR(50) PRIMARY KEY,
    name        VARCHAR(200) NOT NULL,
    category    VARCHAR(50) NOT NULL,  -- live_casino, virtual_sports, slots, crash_games, card_games, table_games
    provider_id VARCHAR(50) NOT NULL REFERENCES betting.casino_providers(id),
    min_bet     NUMERIC(20,2) DEFAULT 10,
    max_bet     NUMERIC(20,2) DEFAULT 500000,
    active      BOOLEAN DEFAULT true,
    thumbnail   VARCHAR(500),
    sort_order  INT DEFAULT 0,
    rtp         NUMERIC(5,2),  -- return to player percentage
    created_at  TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_casino_games_category
    ON betting.casino_games(category, active);

CREATE INDEX idx_casino_games_provider
    ON betting.casino_games(provider_id, active);

COMMENT ON TABLE betting.casino_games IS 'Catalogue of all casino games available on the platform';
COMMENT ON COLUMN betting.casino_games.rtp IS 'Return-to-Player percentage';

-- ============================================================
-- 7. Seed Casino Games
-- ============================================================

-- 7a. Live Casino Games
INSERT INTO betting.casino_games (id, name, category, provider_id, min_bet, max_bet, sort_order, rtp) VALUES
    -- Evolution Gaming
    ('evo_lightning_roulette',  'Lightning Roulette',       'live_casino', 'evolution',  20,   500000,  1, 97.30),
    ('evo_crazy_time',          'Crazy Time',               'live_casino', 'evolution',  10,   250000,  2, 96.08),
    ('evo_dream_catcher',       'Dream Catcher',            'live_casino', 'evolution',  10,   250000,  3, 96.58),
    ('evo_monopoly_live',       'Monopoly Live',            'live_casino', 'evolution',  10,   250000,  4, 96.23),
    ('evo_lightning_baccarat',  'Lightning Baccarat',       'live_casino', 'evolution',  20,   500000,  5, 98.76),
    ('evo_speed_baccarat',      'Speed Baccarat',           'live_casino', 'evolution',  10,   500000,  6, 98.94),
    ('evo_blackjack_vip',       'Blackjack VIP',            'live_casino', 'evolution',  500,  500000,  7, 99.29),
    ('evo_auto_roulette',       'Auto Roulette',            'live_casino', 'evolution',  10,   500000,  8, 97.30),
    ('evo_dragon_tiger',        'Dragon Tiger',             'live_casino', 'evolution',  10,   250000,  9, 96.27),
    ('evo_football_studio',     'Football Studio',          'live_casino', 'evolution',  10,   250000, 10, 96.27),
    ('evo_cash_or_crash',       'Cash or Crash',            'live_casino', 'evolution',  10,   250000, 11, 99.59),
    ('evo_funky_time',          'Funky Time',               'live_casino', 'evolution',  10,   250000, 12, 95.92),

    -- Ezugi Live
    ('ez_teen_patti',           'Teen Patti Live',          'live_casino', 'ezugi',      10,   100000,  1, 96.63),
    ('ez_andar_bahar',          'Andar Bahar Live',         'live_casino', 'ezugi',      10,   100000,  2, 97.85),
    ('ez_32_cards',             '32 Cards',                 'live_casino', 'ezugi',      10,   100000,  3, 97.44),
    ('ez_lucky7',               'Lucky 7',                  'live_casino', 'ezugi',      10,   100000,  4, 96.20),
    ('ez_baccarat',             'Baccarat',                 'live_casino', 'ezugi',      10,   250000,  5, 98.94),
    ('ez_roulette',             'Roulette',                 'live_casino', 'ezugi',      10,   250000,  6, 97.30),
    ('ez_blackjack',            'Blackjack',                'live_casino', 'ezugi',      10,   100000,  7, 99.28),
    ('ez_sic_bo',               'Sic Bo',                   'live_casino', 'ezugi',      10,   100000,  8, 97.22),

    -- BetGames TV
    ('bg_war_of_bets',          'War of Bets',              'live_casino', 'betgames',   10,   100000,  1, 96.40),
    ('bg_6plus_poker',          '6+ Poker',                 'live_casino', 'betgames',   10,   100000,  2, 96.30),
    ('bg_dice',                 'Dice',                     'live_casino', 'betgames',   10,   100000,  3, 95.50),
    ('bg_wheel',                'Wheel of Fortune',         'live_casino', 'betgames',   10,   100000,  4, 95.00),
    ('bg_lucky5',               'Lucky 5',                  'live_casino', 'betgames',   10,   100000,  5, 95.80),
    ('bg_lucky7',               'Lucky 7',                  'live_casino', 'betgames',   10,   100000,  6, 96.00),

    -- Super Spade Games
    ('ss_teen_patti',           'Teen Patti',               'live_casino', 'superspade', 10,   100000,  1, 96.63),
    ('ss_andar_bahar',          'Andar Bahar',              'live_casino', 'superspade', 10,   100000,  2, 97.85),
    ('ss_7up_7down',            '7 Up 7 Down',              'live_casino', 'superspade', 10,   100000,  3, 97.14),
    ('ss_dragon_tiger',         'Dragon Tiger',             'live_casino', 'superspade', 10,   100000,  4, 96.27),
    ('ss_roulette',             'Roulette',                 'live_casino', 'superspade', 10,   100000,  5, 97.30),
    ('ss_muflis_tp',            'Muflis Teen Patti',        'live_casino', 'superspade', 10,   100000,  6, 96.50),
    ('ss_baccarat',             'Baccarat',                 'live_casino', 'superspade', 10,   100000,  7, 98.94)
ON CONFLICT (id) DO NOTHING;

-- 7b. Virtual Sports
INSERT INTO betting.casino_games (id, name, category, provider_id, min_bet, max_bet, sort_order, rtp) VALUES
    ('bg_virt_football',        'Virtual Football',         'virtual_sports', 'betgames',   10,  50000,  1, 94.50),
    ('bg_virt_horse_racing',    'Virtual Horse Racing',     'virtual_sports', 'betgames',   10,  50000,  2, 94.80),
    ('bg_virt_greyhound',       'Virtual Greyhound Racing', 'virtual_sports', 'betgames',   10,  50000,  3, 94.60),
    ('bg_virt_tennis',          'Virtual Tennis',           'virtual_sports', 'betgames',   10,  50000,  4, 95.00),
    ('bg_virt_cricket',         'Virtual Cricket',          'virtual_sports', 'betgames',   10,  50000,  5, 94.70),
    ('bg_virt_basketball',      'Virtual Basketball',       'virtual_sports', 'betgames',   10,  50000,  6, 94.90)
ON CONFLICT (id) DO NOTHING;

-- 7c. Slots
INSERT INTO betting.casino_games (id, name, category, provider_id, min_bet, max_bet, sort_order, rtp) VALUES
    ('pp_gates_olympus',        'Gates of Olympus',         'slots', 'pragmatic_play', 10, 250000,  1, 96.50),
    ('pp_sweet_bonanza',        'Sweet Bonanza',            'slots', 'pragmatic_play', 10, 250000,  2, 96.48),
    ('pp_sugar_rush',           'Sugar Rush',               'slots', 'pragmatic_play', 10, 250000,  3, 96.50),
    ('pp_starlight_princess',   'Starlight Princess',       'slots', 'pragmatic_play', 10, 250000,  4, 96.50),
    ('pp_big_bass_bonanza',     'Big Bass Bonanza',         'slots', 'pragmatic_play', 10, 250000,  5, 96.71),
    ('pp_wolf_gold',            'Wolf Gold',                'slots', 'pragmatic_play', 10, 250000,  6, 96.01),
    ('pp_dog_house',            'The Dog House',            'slots', 'pragmatic_play', 10, 250000,  7, 96.51),
    ('pp_john_hunter',          'John Hunter',              'slots', 'pragmatic_play', 10, 250000,  8, 96.50),
    ('pp_fruit_party',          'Fruit Party',              'slots', 'pragmatic_play', 10, 250000,  9, 96.50),
    ('pp_wild_west_gold',       'Wild West Gold',           'slots', 'pragmatic_play', 10, 250000, 10, 96.51)
ON CONFLICT (id) DO NOTHING;

-- 7d. Crash Games
INSERT INTO betting.casino_games (id, name, category, provider_id, min_bet, max_bet, sort_order, rtp) VALUES
    ('pp_spaceman',             'Spaceman',                 'crash_games', 'pragmatic_play', 10, 100000,  1, 96.50),
    ('pp_mines',                'Mines',                    'crash_games', 'pragmatic_play', 10, 100000,  2, 97.00),
    ('pp_plinko',               'Plinko',                   'crash_games', 'pragmatic_play', 10, 100000,  3, 97.00),
    ('pp_mini_roulette',        'Mini Roulette',            'crash_games', 'pragmatic_play', 10, 100000,  4, 96.15),
    ('pp_penalty_shootout',     'Penalty Shootout',         'crash_games', 'pragmatic_play', 10, 100000,  5, 96.50),
    ('pp_hot_pepper',           'Hot Pepper',               'crash_games', 'pragmatic_play', 10, 100000,  6, 96.50)
ON CONFLICT (id) DO NOTHING;

-- 7e. Card Games
INSERT INTO betting.casino_games (id, name, category, provider_id, min_bet, max_bet, sort_order, rtp) VALUES
    ('ez_teen_patti_20',        'Teen Patti 20-20',         'card_games', 'ezugi',      10, 100000,  1, 96.63),
    ('ss_three_card_poker',     'Three Card Poker',         'card_games', 'superspade', 10, 100000,  2, 96.63),
    ('ez_one_day_tp',           'One Day Teen Patti',       'card_games', 'ezugi',      10, 100000,  3, 96.50),
    ('ss_hi_lo',                'Hi Lo',                    'card_games', 'superspade', 10, 100000,  4, 97.00),
    ('evo_casino_holdem',       'Casino Hold''em',          'card_games', 'evolution',  50, 250000,  5, 97.84),
    ('evo_three_card_poker',    'Three Card Poker',         'card_games', 'evolution',  50, 250000,  6, 96.63),
    ('ss_bollywood_casino',     'Bollywood Casino',         'card_games', 'superspade', 10, 100000,  7, 96.00),
    ('ez_bet_on_tp',            'Bet On Teen Patti',        'card_games', 'ezugi',      10, 100000,  8, 96.50)
ON CONFLICT (id) DO NOTHING;

-- ============================================================
-- 8. Settlement Events Outbox Table
-- ============================================================

CREATE TABLE IF NOT EXISTS betting.settlement_events (
    id              BIGSERIAL PRIMARY KEY,
    market_id       VARCHAR(100) NOT NULL,
    bet_id          VARCHAR(100) NOT NULL,
    user_id         BIGINT NOT NULL,
    event_type      VARCHAR(50) NOT NULL,  -- 'release', 'settle', 'void_release'
    amount          NUMERIC(20,2) NOT NULL,
    commission      NUMERIC(20,2) DEFAULT 0,
    status          VARCHAR(20) DEFAULT 'pending',  -- pending, processed, failed
    created_at      TIMESTAMP DEFAULT NOW(),
    processed_at    TIMESTAMP,
    error_message   TEXT
);

CREATE INDEX idx_settlement_events_pending
    ON betting.settlement_events(status) WHERE status = 'pending';

COMMENT ON TABLE betting.settlement_events IS 'Transactional outbox for reliable settlement event processing';
COMMENT ON COLUMN betting.settlement_events.event_type IS 'release = free exposure, settle = credit winnings, void_release = refund voided bet';

-- ============================================================
-- 9. User Sessions Table (safe to run; 004 already created it)
-- ============================================================

CREATE TABLE IF NOT EXISTS auth.user_sessions (
    id                  BIGSERIAL PRIMARY KEY,
    user_id             BIGINT NOT NULL,
    ip_address          VARCHAR(45),
    user_agent          TEXT,
    device_fingerprint  VARCHAR(200),
    login_at            TIMESTAMP DEFAULT NOW(),
    logout_at           TIMESTAMP,
    active              BOOLEAN DEFAULT true
);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user
    ON auth.user_sessions(user_id, active);

-- ============================================================
-- Done
-- ============================================================
