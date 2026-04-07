-- Demo Seed Data for Lotus Exchange
-- Run after migrations: psql bettingdb -f scripts/seed_demo.sql

SET search_path TO betting, auth, public;

-- ══════════════════════════════════════════════════════════════
-- 1. USER HIERARCHY (5 levels)
-- ══════════════════════════════════════════════════════════════

-- SuperAdmin already seeded in migration (id=1)
-- Password for ALL demo users: "demo1234"
-- Hash: demo salt + argon2id (pre-computed for demo)

-- Admin (level 2)
INSERT INTO auth.users (username, email, password_hash, path, role, parent_id, balance, credit_limit, commission_rate, status)
VALUES
('admin1', 'admin1@lotus.com',
 'deadbeef00000001deadbeef00000001:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2', 'admin', 1, 500000.00, 5000000.00, 5.0, 'active');

-- Master (level 3)
INSERT INTO auth.users (username, email, password_hash, path, role, parent_id, balance, credit_limit, commission_rate, status)
VALUES
('master1', 'master1@lotus.com',
 'deadbeef00000002deadbeef00000002:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2.3', 'master', 2, 200000.00, 1000000.00, 3.0, 'active');

-- Agent (level 4)
INSERT INTO auth.users (username, email, password_hash, path, role, parent_id, balance, credit_limit, commission_rate, status)
VALUES
('agent1', 'agent1@lotus.com',
 'deadbeef00000003deadbeef00000003:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2.3.4', 'agent', 3, 100000.00, 500000.00, 2.0, 'active');

-- Clients (level 5) — the bettors
INSERT INTO auth.users (username, email, password_hash, path, role, parent_id, balance, credit_limit, commission_rate, status)
VALUES
('player1', 'player1@lotus.com',
 'deadbeef00000004deadbeef00000004:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2.3.4.5', 'client', 4, 50000.00, 100000.00, 2.0, 'active'),
('player2', 'player2@lotus.com',
 'deadbeef00000005deadbeef00000005:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2.3.4.6', 'client', 4, 25000.00, 50000.00, 2.0, 'active'),
('player3', 'player3@lotus.com',
 'deadbeef00000006deadbeef00000006:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855',
 '1.2.3.4.7', 'client', 4, 75000.00, 200000.00, 1.5, 'active');

-- ══════════════════════════════════════════════════════════════
-- 2. MARKETS — IPL 2026 Cricket
-- ══════════════════════════════════════════════════════════════

-- Match 1: MI vs CSK (LIVE in-play)
INSERT INTO betting.markets (id, event_id, sport, name, market_type, status, in_play, start_time, total_matched)
VALUES
('ipl-mi-csk-mo', 'ipl-mi-csk', 'cricket', 'Mumbai Indians v Chennai Super Kings - Match Odds', 'match_odds', 'open', true, NOW() - INTERVAL '45 minutes', 2500000.00),
('ipl-mi-csk-fancy1', 'ipl-mi-csk', 'cricket', 'MI Innings Runs - Over 150.5', 'fancy', 'open', true, NOW() - INTERVAL '45 minutes', 800000.00),
('ipl-mi-csk-fancy2', 'ipl-mi-csk', 'cricket', 'MI Innings Runs - Over 170.5', 'fancy', 'open', true, NOW() - INTERVAL '45 minutes', 450000.00),
('ipl-mi-csk-bm', 'ipl-mi-csk', 'cricket', 'Mumbai Indians v Chennai Super Kings - Bookmaker', 'bookmaker', 'open', true, NOW() - INTERVAL '45 minutes', 1200000.00);

-- Match 2: RCB vs KKR (Upcoming)
INSERT INTO betting.markets (id, event_id, sport, name, market_type, status, in_play, start_time, total_matched)
VALUES
('ipl-rcb-kkr-mo', 'ipl-rcb-kkr', 'cricket', 'Royal Challengers v Kolkata Knight Riders - Match Odds', 'match_odds', 'open', false, NOW() + INTERVAL '3 hours', 500000.00),
('ipl-rcb-kkr-fancy1', 'ipl-rcb-kkr', 'cricket', 'RCB Innings Runs - Over 160.5', 'fancy', 'open', false, NOW() + INTERVAL '3 hours', 120000.00);

-- Match 3: DC vs SRH (Upcoming tomorrow)
INSERT INTO betting.markets (id, event_id, sport, name, market_type, status, in_play, start_time, total_matched)
VALUES
('ipl-dc-srh-mo', 'ipl-dc-srh', 'cricket', 'Delhi Capitals v Sunrisers Hyderabad - Match Odds', 'match_odds', 'open', false, NOW() + INTERVAL '27 hours', 150000.00);

-- Match 4: GT vs LSG (Settled — for history)
INSERT INTO betting.markets (id, event_id, sport, name, market_type, status, in_play, start_time, total_matched)
VALUES
('ipl-gt-lsg-mo', 'ipl-gt-lsg', 'cricket', 'Gujarat Titans v Lucknow Super Giants - Match Odds', 'match_odds', 'settled', false, NOW() - INTERVAL '1 day', 3200000.00);

-- ══════════════════════════════════════════════════════════════
-- 3. RUNNERS
-- ══════════════════════════════════════════════════════════════

-- MI vs CSK Match Odds
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-mi-csk-mo', 101, 'Mumbai Indians', 'active'),
('ipl-mi-csk-mo', 102, 'Chennai Super Kings', 'active'),
('ipl-mi-csk-mo', 103, 'The Draw', 'active');

-- MI vs CSK Fancy
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-mi-csk-fancy1', 201, 'Over 150.5 Runs', 'active'),
('ipl-mi-csk-fancy2', 202, 'Over 170.5 Runs', 'active');

-- MI vs CSK Bookmaker
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-mi-csk-bm', 301, 'Mumbai Indians', 'active'),
('ipl-mi-csk-bm', 302, 'Chennai Super Kings', 'active');

-- RCB vs KKR
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-rcb-kkr-mo', 401, 'Royal Challengers Bengaluru', 'active'),
('ipl-rcb-kkr-mo', 402, 'Kolkata Knight Riders', 'active'),
('ipl-rcb-kkr-mo', 403, 'The Draw', 'active'),
('ipl-rcb-kkr-fancy1', 501, 'Over 160.5 Runs', 'active');

-- DC vs SRH
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-dc-srh-mo', 601, 'Delhi Capitals', 'active'),
('ipl-dc-srh-mo', 602, 'Sunrisers Hyderabad', 'active'),
('ipl-dc-srh-mo', 603, 'The Draw', 'active');

-- GT vs LSG (settled)
INSERT INTO betting.runners (market_id, selection_id, name, status) VALUES
('ipl-gt-lsg-mo', 701, 'Gujarat Titans', 'winner'),
('ipl-gt-lsg-mo', 702, 'Lucknow Super Giants', 'loser'),
('ipl-gt-lsg-mo', 703, 'The Draw', 'loser');

-- ══════════════════════════════════════════════════════════════
-- 4. SAMPLE BETS (historical + active)
-- ══════════════════════════════════════════════════════════════

-- Active bets on MI vs CSK
INSERT INTO betting.bets (id, market_id, selection_id, user_id, side, price, stake, matched_stake, unmatched_stake, profit, status, client_ref, created_at)
VALUES
('bet-001', 'ipl-mi-csk-mo', 101, 5, 'back', 1.85, 5000, 5000, 0, 0, 'matched', 'demo-001', NOW() - INTERVAL '30 minutes'),
('bet-002', 'ipl-mi-csk-mo', 102, 6, 'back', 2.10, 3000, 3000, 0, 0, 'matched', 'demo-002', NOW() - INTERVAL '28 minutes'),
('bet-003', 'ipl-mi-csk-mo', 101, 5, 'lay', 1.90, 2000, 0, 2000, 0, 'unmatched', 'demo-003', NOW() - INTERVAL '15 minutes'),
('bet-004', 'ipl-mi-csk-mo', 102, 7, 'back', 2.05, 10000, 8000, 2000, 0, 'partial', 'demo-004', NOW() - INTERVAL '10 minutes'),
('bet-005', 'ipl-mi-csk-fancy1', 201, 5, 'back', 1.75, 2000, 2000, 0, 0, 'matched', 'demo-005', NOW() - INTERVAL '20 minutes');

-- Settled bets on GT vs LSG (GT won)
INSERT INTO betting.bets (id, market_id, selection_id, user_id, side, price, stake, matched_stake, unmatched_stake, profit, status, client_ref, created_at, matched_at, settled_at)
VALUES
('bet-100', 'ipl-gt-lsg-mo', 701, 5, 'back', 1.95, 10000, 10000, 0, 9500, 'settled', 'demo-100', NOW() - INTERVAL '25 hours', NOW() - INTERVAL '24 hours', NOW() - INTERVAL '1 hour'),
('bet-101', 'ipl-gt-lsg-mo', 702, 6, 'back', 2.00, 5000, 5000, 0, -5000, 'settled', 'demo-101', NOW() - INTERVAL '25 hours', NOW() - INTERVAL '24 hours', NOW() - INTERVAL '1 hour'),
('bet-102', 'ipl-gt-lsg-mo', 701, 7, 'lay', 1.95, 3000, 3000, 0, -2850, 'settled', 'demo-102', NOW() - INTERVAL '24 hours', NOW() - INTERVAL '23 hours', NOW() - INTERVAL '1 hour');

-- ══════════════════════════════════════════════════════════════
-- 5. LEDGER ENTRIES
-- ══════════════════════════════════════════════════════════════

INSERT INTO betting.ledger (user_id, amount, type, reference, bet_id, created_at) VALUES
(5, 50000.00, 'deposit', 'deposit:initial:5', NULL, NOW() - INTERVAL '2 days'),
(6, 25000.00, 'deposit', 'deposit:initial:6', NULL, NOW() - INTERVAL '2 days'),
(7, 75000.00, 'deposit', 'deposit:initial:7', NULL, NOW() - INTERVAL '2 days'),
(5, -5000.00, 'hold', 'hold:bet-001', 'bet-001', NOW() - INTERVAL '30 minutes'),
(6, -3000.00, 'hold', 'hold:bet-002', 'bet-002', NOW() - INTERVAL '28 minutes'),
(5, -2000.00, 'hold', 'hold:bet-003', 'bet-003', NOW() - INTERVAL '15 minutes'),
(7, -10000.00, 'hold', 'hold:bet-004', 'bet-004', NOW() - INTERVAL '10 minutes'),
(5, 9500.00, 'settlement', 'settlement:bet-100', 'bet-100', NOW() - INTERVAL '1 hour'),
(5, -190.00, 'commission', 'commission:bet-100', 'bet-100', NOW() - INTERVAL '1 hour'),
(6, -5000.00, 'settlement', 'settlement:bet-101', 'bet-101', NOW() - INTERVAL '1 hour'),
(7, -2850.00, 'settlement', 'settlement:bet-102', 'bet-102', NOW() - INTERVAL '1 hour');

-- ══════════════════════════════════════════════════════════════
-- 6. PAYMENT TRANSACTIONS
-- ══════════════════════════════════════════════════════════════

INSERT INTO betting.payment_transactions (id, user_id, direction, method, amount, currency, status, upi_id, created_at, completed_at)
VALUES
('tx_demo_001', 5, 'deposit', 'upi', 50000.00, 'INR', 'completed', 'player1@paytm', NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),
('tx_demo_002', 6, 'deposit', 'upi', 25000.00, 'INR', 'completed', 'player2@gpay', NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),
('tx_demo_003', 7, 'deposit', 'crypto', 75000.00, 'USDT', 'completed', NULL, NOW() - INTERVAL '2 days', NOW() - INTERVAL '2 days'),
('tx_demo_004', 5, 'withdrawal', 'upi', 5000.00, 'INR', 'pending', 'player1@paytm', NOW() - INTERVAL '2 hours', NULL);

-- ══════════════════════════════════════════════════════════════
-- 7. CASINO SESSIONS
-- ══════════════════════════════════════════════════════════════

INSERT INTO betting.casino_sessions (id, user_id, game_type, provider_id, status, stream_url, token, created_at, expires_at)
VALUES
('casino-demo-001', 5, 'teen_patti', 'evolution', 'closed', 'https://stream.evolution.com/hls/teen_patti/casino-demo-001/stream.m3u8', 'token123', NOW() - INTERVAL '3 hours', NOW() - INTERVAL '1 hour'),
('casino-demo-002', 6, 'andar_bahar', 'ezugi', 'active', 'https://stream.ezugi.com/hls/andar_bahar/casino-demo-002/stream.m3u8', 'token456', NOW() - INTERVAL '30 minutes', NOW() + INTERVAL '3 hours');

-- ══════════════════════════════════════════════════════════════
-- 8. UPDATE EXPOSURES on users
-- ══════════════════════════════════════════════════════════════

UPDATE auth.users SET exposure = 7000.00 WHERE id = 5;   -- player1: bet-001 + bet-003
UPDATE auth.users SET exposure = 3000.00 WHERE id = 6;   -- player2: bet-002
UPDATE auth.users SET exposure = 10000.00 WHERE id = 7;  -- player3: bet-004

-- Recalculate balances after settlements
UPDATE auth.users SET balance = 50000.00 + 9500.00 - 190.00 WHERE id = 5;  -- 59310
UPDATE auth.users SET balance = 25000.00 - 5000.00 WHERE id = 6;           -- 20000
UPDATE auth.users SET balance = 75000.00 - 2850.00 WHERE id = 7;           -- 72150

SELECT '=== DEMO SEED COMPLETE ===' AS status;
SELECT id, username, role, balance, exposure, balance - exposure AS available FROM auth.users ORDER BY id;
SELECT id, name, status, in_play, total_matched FROM betting.markets ORDER BY start_time;
SELECT COUNT(*) AS total_bets FROM betting.bets;
