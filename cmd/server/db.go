package main

// PostgreSQL-backed store. When DATABASE_URL is set, all user/bet/ledger/notification
// data is persisted to PostgreSQL. Markets, runners, odds, order books remain in-memory
// (populated from seed data + Odds API) since they're ephemeral real-time data.
//
// This hybrid approach gives us:
// - Persistent users, bets, ledger, notifications, audit across restarts
// - Fast in-memory matching engine and order books
// - Mock odds fluctuation (or real via ODDS_API_KEY)

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

func initDB(databaseURL string, log *slog.Logger) error {
	var err error
	db, err = sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err = db.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	log.Info("connected to PostgreSQL", "url", sanitizeURL(databaseURL))

	// Run migrations
	if err := runMigrations(log); err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

func sanitizeURL(url string) string {
	// Hide password in logs
	if i := strings.Index(url, "@"); i > 0 {
		if j := strings.Index(url, "://"); j > 0 {
			return url[:j+3] + "***@" + url[i+1:]
		}
	}
	return url
}

func runMigrations(log *slog.Logger) error {
	// Create schemas and extensions
	migrations := []string{
		`CREATE SCHEMA IF NOT EXISTS betting`,
		`CREATE SCHEMA IF NOT EXISTS auth`,
		`CREATE EXTENSION IF NOT EXISTS ltree`,

		// ── Users ──
		`CREATE TABLE IF NOT EXISTS auth.users (
			id BIGSERIAL PRIMARY KEY,
			username VARCHAR(50) UNIQUE NOT NULL,
			email VARCHAR(255) NOT NULL,
			password_hash TEXT NOT NULL DEFAULT '',
			path TEXT NOT NULL DEFAULT '',
			role TEXT NOT NULL DEFAULT 'client',
			parent_id BIGINT REFERENCES auth.users(id),
			balance NUMERIC(20,2) DEFAULT 0,
			exposure NUMERIC(20,2) DEFAULT 0,
			credit_limit NUMERIC(20,2) DEFAULT 0,
			commission_rate NUMERIC(5,2) DEFAULT 2,
			status TEXT DEFAULT 'active',
			referral_code TEXT DEFAULT '',
			referred_by BIGINT DEFAULT 0,
			otp_enabled BOOLEAN DEFAULT FALSE,
			is_demo BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW(),
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Bets (non-partitioned for simplicity) ──
		`CREATE TABLE IF NOT EXISTS betting.bets (
			id TEXT PRIMARY KEY,
			market_id TEXT NOT NULL,
			selection_id BIGINT NOT NULL,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			side TEXT NOT NULL,
			price NUMERIC(10,2) NOT NULL,
			stake NUMERIC(20,2) NOT NULL,
			matched_stake NUMERIC(20,2) DEFAULT 0,
			unmatched_stake NUMERIC(20,2) DEFAULT 0,
			profit NUMERIC(20,2) DEFAULT 0,
			status TEXT DEFAULT 'unmatched',
			client_ref TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Ledger ──
		`CREATE TABLE IF NOT EXISTS betting.ledger (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			amount NUMERIC(20,2) NOT NULL,
			type TEXT NOT NULL,
			reference TEXT NOT NULL,
			bet_id TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Notifications ──
		`CREATE TABLE IF NOT EXISTS betting.notifications (
			id TEXT PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			type TEXT NOT NULL,
			title TEXT NOT NULL,
			message TEXT NOT NULL,
			read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Audit Log ──
		`CREATE TABLE IF NOT EXISTS betting.audit_log (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT DEFAULT 0,
			username TEXT DEFAULT '',
			action TEXT NOT NULL,
			details TEXT DEFAULT '',
			ip TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Login History ──
		`CREATE TABLE IF NOT EXISTS auth.login_history (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			ip TEXT DEFAULT '',
			user_agent TEXT DEFAULT '',
			success BOOLEAN DEFAULT TRUE,
			login_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Payment Transactions ──
		`CREATE TABLE IF NOT EXISTS betting.payment_transactions (
			id TEXT PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			direction TEXT NOT NULL,
			method TEXT NOT NULL,
			amount NUMERIC(20,2) NOT NULL,
			currency TEXT NOT NULL DEFAULT 'INR',
			status TEXT DEFAULT 'pending',
			upi_id TEXT DEFAULT '',
			wallet_address TEXT DEFAULT '',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Responsible Gambling ──
		`CREATE TABLE IF NOT EXISTS betting.responsible_gambling (
			user_id BIGINT PRIMARY KEY REFERENCES auth.users(id),
			daily_deposit_limit NUMERIC(20,2) DEFAULT 0,
			daily_loss_limit NUMERIC(20,2) DEFAULT 0,
			max_stake_per_bet NUMERIC(20,2) DEFAULT 0,
			session_limit_minutes INT DEFAULT 0,
			self_excluded BOOLEAN DEFAULT FALSE,
			excluded_until TIMESTAMPTZ,
			updated_at TIMESTAMPTZ DEFAULT NOW()
		)`,

		// ── Indexes ──
		`CREATE INDEX IF NOT EXISTS idx_bets_user ON betting.bets(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bets_market ON betting.bets(market_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bets_status ON betting.bets(status)`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_user ON betting.ledger(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_notif_user ON betting.notifications(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_notif_read ON betting.notifications(user_id, read)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_user ON betting.audit_log(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_login_user ON auth.login_history(user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_payment_user ON betting.payment_transactions(user_id)`,

		// ── Compatibility patches for legacy schemas ──
		// audit_log may have older schema with actor_id; ensure new columns exist
		`ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS user_id BIGINT DEFAULT 0`,
		`ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS username TEXT DEFAULT ''`,
		`ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS details TEXT DEFAULT ''`,
		`ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS ip TEXT DEFAULT ''`,
		// Age verification — required for any regulated market.
		`ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS date_of_birth DATE`,
		// KYC status — gates withdrawals.
		`ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_status TEXT DEFAULT 'pending'`,
		`ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_verified_at TIMESTAMPTZ`,
		`ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_rejection_reason TEXT`,
		// bets table may be missing market_type from legacy schema
		`ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS market_type TEXT DEFAULT ''`,
		// display_side stores "yes"/"no" for fancy/session, "back"/"lay" otherwise.
		// Without this column dbSaveBet silently fails on INSERT and bets vanish
		// after the in-memory store cycles.
		`ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS display_side TEXT DEFAULT ''`,
		// notifications check constraint may be too restrictive
		`ALTER TABLE betting.notifications DROP CONSTRAINT IF EXISTS notifications_type_check`,
		`ALTER TABLE betting.notifications ADD CONSTRAINT notifications_type_check CHECK (type IN ('bet_matched','bet_placed','bet_settled','bet_won','bet_lost','deposit_complete','deposit','withdrawal_complete','withdrawal','credit','cashout_available','kyc_update','promotion','system','responsible_gambling','login','info','warning','success','error'))`,

		// ── KYC documents ──
		`CREATE TABLE IF NOT EXISTS betting.kyc_documents (
			id BIGSERIAL PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES auth.users(id),
			doc_type TEXT NOT NULL DEFAULT '',
			filename TEXT NOT NULL DEFAULT '',
			size_bytes BIGINT DEFAULT 0,
			content_type TEXT DEFAULT '',
			storage_url TEXT DEFAULT '',
			status TEXT DEFAULT 'pending_review',
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_kyc_user ON betting.kyc_documents(user_id)`,

		// ── bet_fills (matching engine per-fill rows) ──
		`CREATE TABLE IF NOT EXISTS betting.bet_fills (
			id BIGSERIAL PRIMARY KEY,
			bet_id TEXT NOT NULL,
			counter_bet_id TEXT NOT NULL,
			price NUMERIC(10,2) NOT NULL,
			size NUMERIC(20,2) NOT NULL,
			created_at TIMESTAMPTZ DEFAULT NOW()
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bet_fills_bet_id  ON betting.bet_fills(bet_id)`,
		`CREATE INDEX IF NOT EXISTS idx_bet_fills_counter ON betting.bet_fills(counter_bet_id)`,

		// ── Phase 10: Performance indexes (mirror of migration 010) ──
		// Make sure the columns the indexes cover actually exist on the
		// monolith's simpler schema. These ALTERs are idempotent.
		`ALTER TABLE betting.bets ADD COLUMN IF NOT EXISTS settled_at TIMESTAMPTZ`,

		// Bets: cover settled_at queries (responsible gambling daily-loss check).
		`CREATE INDEX IF NOT EXISTS idx_bets_user_settled ON betting.bets(user_id, settled_at) WHERE status = 'settled'`,
		// Bets: cover ClickHouse ingestion (ORDER BY settled_at).
		`CREATE INDEX IF NOT EXISTS idx_bets_settled_at ON betting.bets(settled_at) WHERE status = 'settled'`,
		// Users: partial index on active status for the dashboard filter.
		`CREATE INDEX IF NOT EXISTS idx_users_status_active ON auth.users(status) WHERE status = 'active'`,
		// Markets / events: ORDER BY start_time default listings (tables may not
		// exist on the monolith; migration statement failures are logged and skipped).
		`CREATE INDEX IF NOT EXISTS idx_markets_start_time ON betting.markets(start_time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_events_start_time ON betting.events(start_time)`,
		// Settlement events outbox: pending rows in id order.
		`CREATE INDEX IF NOT EXISTS idx_settlement_events_pending_id ON betting.settlement_events(id) WHERE status = 'pending'`,
		// Casino sessions: active session lookup per user.
		`CREATE INDEX IF NOT EXISTS idx_casino_sessions_user_active ON betting.casino_sessions(user_id) WHERE status = 'active'`,
		// Notifications: unread badge count.
		`CREATE INDEX IF NOT EXISTS idx_notifications_user_unread ON betting.notifications(user_id, created_at DESC) WHERE read = false`,
		// Audit log: latest-N-for-user queries.
		`CREATE INDEX IF NOT EXISTS idx_audit_user_id_desc ON betting.audit_log(user_id, id DESC)`,
		// Fraud alerts: created_at ordering for the ops dashboard.
		`CREATE INDEX IF NOT EXISTS idx_fraud_alerts_created ON betting.fraud_alerts(created_at DESC)`,

		// ── Extend partition rollover horizon ──
		// Migration 004 originally created partitions only 3 months ahead.
		// Bump to 12 months so a weekly server restart comfortably keeps the
		// betting.bets partition tree from running out. CREATE OR REPLACE is
		// idempotent and only runs when the function exists (partitioned
		// schema). On the monolith's non-partitioned schema this fails with
		// "schema betting does not exist" and is silently logged.
		`CREATE OR REPLACE FUNCTION betting.create_monthly_partition()
		RETURNS void
		LANGUAGE plpgsql
		AS $$
		DECLARE
		    partition_date  DATE;
		    partition_name  TEXT;
		    start_date      DATE;
		    end_date        DATE;
		BEGIN
		    FOR i IN 0..11 LOOP
		        partition_date := date_trunc('month', NOW() + (i || ' months')::interval)::date;
		        start_date     := partition_date;
		        end_date       := (partition_date + interval '1 month')::date;
		        partition_name := 'bets_' || to_char(partition_date, 'YYYY_MM');
		        IF NOT EXISTS (
		            SELECT 1
		            FROM pg_catalog.pg_class c
		            JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		            WHERE n.nspname = 'betting' AND c.relname = partition_name
		        ) THEN
		            EXECUTE format(
		                'CREATE TABLE betting.%I PARTITION OF betting.bets FOR VALUES FROM (%L) TO (%L)',
		                partition_name, start_date, end_date
		            );
		        END IF;
		    END LOOP;
		END;
		$$`,
	}

	for _, m := range migrations {
		if _, err := db.Exec(m); err != nil {
			// Skip "already exists" type errors
			if !strings.Contains(err.Error(), "already exists") &&
				!strings.Contains(err.Error(), "duplicate key") {
				log.Warn("migration statement failed", "error", err, "sql", m[:minInt(80, len(m))])
			}
		}
	}

	// If betting.bets is partitioned (migration 001 schema), backfill the next
	// few months of partitions on every startup. The function only exists when
	// migration 004 has been applied; if it isn't there we silently skip — the
	// monolith's CREATE TABLE IF NOT EXISTS path uses a non-partitioned table
	// where this is a no-op anyway.
	//
	// Without this, the static partitions in migration 001 run out at the end
	// of 2026 and bet INSERTs start failing with "no partition of relation
	// betting.bets found for row" — a hard production outage.
	if _, err := db.Exec(`SELECT betting.create_monthly_partition()`); err != nil {
		if !strings.Contains(err.Error(), "does not exist") {
			log.Warn("partition backfill failed", "error", err)
		}
	}

	log.Info("database migrations complete")
	return nil
}

func useDB() bool {
	return db != nil
}

// ── DB-backed User operations ─────────────────────────────────────────────────

func dbCreateUser(username, email, passwordHash, role, path string, parentID *int64, balance, creditLimit, commRate float64, isDemo bool) (*User, error) {
	var id int64
	now := time.Now().Format(time.RFC3339)
	refCode := fmt.Sprintf("REF-%s-%s", strings.ToUpper(username), randHex(2))

	err := db.QueryRow(`
		INSERT INTO auth.users (username, email, password_hash, role, path, parent_id, balance, credit_limit, commission_rate, referral_code, is_demo, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NOW(),NOW()) RETURNING id`,
		username, email, passwordHash, role, path, parentID, balance, creditLimit, commRate, refCode, isDemo,
	).Scan(&id)
	if err != nil {
		return nil, err
	}

	// Update path to include own ID if no parent
	if parentID == nil {
		if _, err := db.Exec(`UPDATE auth.users SET path=$1 WHERE id=$2`, fmt.Sprintf("%d", id), id); err != nil {
			logger.Error("dbCreateUser: failed to update path", "error", err)
		}
	}

	return &User{
		ID: id, Username: username, Email: email, PasswordHash: passwordHash,
		Role: role, Path: path, ParentID: parentID,
		Balance: balance, Exposure: 0, CreditLimit: creditLimit,
		CommissionRate: commRate, Status: "active", ReferralCode: refCode,
		IsDemo: isDemo, CreatedAt: now, UpdatedAt: now,
	}, nil
}

func dbGetUserByUsername(username string) *User {
	u := &User{}
	var parentID sql.NullInt64
	err := db.QueryRow(`
		SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		       credit_limit, commission_rate, status, referral_code, otp_enabled, is_demo, created_at, updated_at
		FROM auth.users WHERE username=$1`, username).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Path, &parentID,
		&u.Balance, &u.Exposure, &u.CreditLimit, &u.CommissionRate, &u.Status,
		&u.ReferralCode, &u.OTPEnabled, &u.IsDemo, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	if parentID.Valid {
		pid := parentID.Int64
		u.ParentID = &pid
	}
	return u
}

func dbGetUser(id int64) *User {
	u := &User{}
	var parentID sql.NullInt64
	err := db.QueryRow(`
		SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		       credit_limit, commission_rate, status, referral_code, otp_enabled, is_demo, created_at, updated_at
		FROM auth.users WHERE id=$1`, id).Scan(
		&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Path, &parentID,
		&u.Balance, &u.Exposure, &u.CreditLimit, &u.CommissionRate, &u.Status,
		&u.ReferralCode, &u.OTPEnabled, &u.IsDemo, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil
	}
	if parentID.Valid {
		pid := parentID.Int64
		u.ParentID = &pid
	}
	return u
}

func dbUpdateUserStatus(userID int64, status string) {
	if _, err := db.Exec(`UPDATE auth.users SET status=$1, updated_at=NOW() WHERE id=$2`, status, userID); err != nil {
		logger.Error("dbUpdateUserStatus failed", "error", err)
	}
}

func dbUpdateBalance(userID int64, balance, exposure float64) {
	if _, err := db.Exec(`UPDATE auth.users SET balance=$1, exposure=$2, updated_at=NOW() WHERE id=$3`, balance, exposure, userID); err != nil {
		logger.Error("dbUpdateBalance failed", "error", err)
	}
}

func dbAllUsers() []*User {
	rows, err := db.Query(`
		SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		       credit_limit, commission_rate, status, referral_code, otp_enabled, is_demo, created_at
		FROM auth.users ORDER BY id`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		var parentID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Path, &parentID,
			&u.Balance, &u.Exposure, &u.CreditLimit, &u.CommissionRate,
			&u.Status, &u.ReferralCode, &u.OTPEnabled, &u.IsDemo, &u.CreatedAt); err != nil {
			logger.Error("dbAllUsers scan error, skipping row", "error", err)
			continue
		}
		if parentID.Valid {
			pid := parentID.Int64
			u.ParentID = &pid
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbAllUsers rows iteration error", "error", err)
	}
	return users
}

// ── DB-backed Bet operations ──────────────────────────────────────────────────

func dbSaveBet(b *Bet) {
	// Plain INSERT — no ON CONFLICT clause. The bets table may be partitioned
	// (migration 001) with composite PK (id, created_at), in which case
	// ON CONFLICT (id) fails at runtime because there's no unique index on
	// id alone. Bet IDs are random hex so duplicates are essentially
	// impossible; if one ever happens the unique violation surfaces as a
	// real error which dbUpdateBet's UPDATE handles separately.
	_, err := db.Exec(`
		INSERT INTO betting.bets (id, market_id, selection_id, user_id, side, price, stake, matched_stake, unmatched_stake, profit, status, client_ref, market_type, display_side, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		b.ID, b.MarketID, b.SelectionID, b.UserID, b.Side, b.Price, b.Stake,
		b.MatchedStake, b.UnmatchedStake, b.Profit, b.Status, b.ClientRef, b.MarketType, b.DisplaySide, b.CreatedAt)
	if err != nil {
		logger.Error("dbSaveBet failed", "bet_id", b.ID, "error", err)
	}
}

func dbGetUserBets(userID int64) []*Bet {
	rows, err := db.Query(`
		SELECT id, market_id, selection_id, user_id, side, price, stake, matched_stake, unmatched_stake, profit, status, client_ref, market_type, display_side, created_at
		FROM betting.bets WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var bets []*Bet
	for rows.Next() {
		b := &Bet{}
		var marketType, displaySide sql.NullString
		if err := rows.Scan(&b.ID, &b.MarketID, &b.SelectionID, &b.UserID, &b.Side, &b.Price, &b.Stake,
			&b.MatchedStake, &b.UnmatchedStake, &b.Profit, &b.Status, &b.ClientRef, &marketType, &displaySide, &b.CreatedAt); err != nil {
			logger.Error("dbGetUserBets scan error, skipping row", "error", err)
			continue
		}
		if marketType.Valid {
			b.MarketType = marketType.String
		}
		if displaySide.Valid {
			b.DisplaySide = displaySide.String
		}
		bets = append(bets, b)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetUserBets rows iteration error", "error", err)
	}
	return bets
}

func dbUpdateBet(b *Bet) {
	_, err := db.Exec(`UPDATE betting.bets SET matched_stake=$1, unmatched_stake=$2, profit=$3, status=$4 WHERE id=$5`,
		b.MatchedStake, b.UnmatchedStake, b.Profit, b.Status, b.ID)
	if err != nil {
		logger.Error("dbUpdateBet failed", "bet_id", b.ID, "error", err)
	}
}

// ── DB-backed Ledger ──────────────────────────────────────────────────────────

func dbAddLedger(userID int64, amount float64, typ, ref, betID string) {
	if _, err := db.Exec(`INSERT INTO betting.ledger (user_id, amount, type, reference, bet_id) VALUES ($1,$2,$3,$4,$5)`,
		userID, amount, typ, ref, betID); err != nil {
		logger.Error("dbAddLedger failed", "error", err)
	}
}

func dbGetLedger(userID int64, limit, offset int) []*LedgerEntry {
	rows, err := db.Query(`
		SELECT id, user_id, amount, type, reference, bet_id, created_at
		FROM betting.ledger WHERE user_id=$1 ORDER BY id DESC LIMIT $2 OFFSET $3`,
		userID, limit, offset)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var entries []*LedgerEntry
	for rows.Next() {
		e := &LedgerEntry{}
		var betID sql.NullString
		if err := rows.Scan(&e.ID, &e.UserID, &e.Amount, &e.Type, &e.Reference, &betID, &e.CreatedAt); err != nil {
			logger.Error("dbGetLedger scan error, skipping row", "error", err)
			continue
		}
		e.BetID = betID.String
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetLedger rows iteration error", "error", err)
	}
	return entries
}

// ── DB-backed Notifications ───────────────────────────────────────────────────

func dbAddNotification(id string, userID int64, typ, title, message string) {
	if _, err := db.Exec(`INSERT INTO betting.notifications (user_id, type, title, message) VALUES ($1,$2,$3,$4)`,
		userID, typ, title, message); err != nil {
		logger.Error("dbAddNotification failed", "error", err)
	}
}

func dbGetNotifications(userID int64, unreadOnly bool, limit, offset int) []*Notification {
	var query string
	if unreadOnly {
		query = `SELECT id, user_id, type, title, message, read, created_at FROM betting.notifications WHERE user_id=$1 AND read=FALSE ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	} else {
		query = `SELECT id, user_id, type, title, message, read, created_at FROM betting.notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`
	}
	rows, err := db.Query(query, userID, limit, offset)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var notifs []*Notification
	for rows.Next() {
		n := &Notification{}
		if err := rows.Scan(&n.ID, &n.UserID, &n.Type, &n.Title, &n.Message, &n.Read, &n.Created); err != nil {
			logger.Error("dbGetNotifications scan error, skipping row", "error", err)
			continue
		}
		notifs = append(notifs, n)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetNotifications rows iteration error", "error", err)
	}
	return notifs
}

func dbGetUnreadCount(userID int64) int {
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM betting.notifications WHERE user_id=$1 AND read=FALSE`, userID).Scan(&count)
	return count
}

func dbMarkNotificationRead(userID int64, notifID string) bool {
	res, err := db.Exec(`UPDATE betting.notifications SET read=TRUE WHERE id=$1 AND user_id=$2`, notifID, userID)
	if err != nil {
		return false
	}
	n, _ := res.RowsAffected()
	return n > 0
}

func dbMarkAllNotificationsRead(userID int64) int {
	res, _ := db.Exec(`UPDATE betting.notifications SET read=TRUE WHERE user_id=$1 AND read=FALSE`, userID)
	n, _ := res.RowsAffected()
	return int(n)
}

// ── DB-backed Audit Log ───────────────────────────────────────────────────────

func dbAddAudit(userID int64, username, action, details, ip string) {
	// audit_log has both legacy schema (actor_id, entity_type, entity_id) and new schema (user_id, username, details, ip)
	// Use new columns added by setup migration; entity fields are optional with safe defaults.
	if _, err := db.Exec(`INSERT INTO betting.audit_log (user_id, username, action, details, ip, entity_type, entity_id) VALUES ($1,$2,$3,$4,$5,'system','-')`,
		userID, username, action, details, ip); err != nil {
		logger.Error("dbAddAudit failed", "error", err)
	}
}

func dbRecordLogin(userID int64, ip, userAgent string, success bool) {
	if _, err := db.Exec(`INSERT INTO auth.login_history (user_id, ip, user_agent, success) VALUES ($1,$2,$3,$4)`,
		userID, ip, userAgent, success); err != nil {
		logger.Error("dbRecordLogin failed", "error", err)
	}
}

func dbGetLoginHistory(userID int64, limit int) []*LoginRecord {
	rows, err := db.Query(`
		SELECT user_id, ip, user_agent, login_at, success
		FROM auth.login_history WHERE user_id=$1 ORDER BY login_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var records []*LoginRecord
	for rows.Next() {
		r := &LoginRecord{}
		if err := rows.Scan(&r.UserID, &r.IP, &r.UserAgent, &r.LoginAt, &r.Success); err != nil {
			logger.Error("dbGetLoginHistory scan error, skipping row", "error", err)
			continue
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetLoginHistory rows iteration error", "error", err)
	}
	return records
}

// ── DB-backed Bet reads ───────────────────────────────────────────────────────

func dbAllBets() []*Bet {
	rows, err := db.Query(`
		SELECT id, market_id, selection_id, user_id, side, price, stake, matched_stake, unmatched_stake, profit, status, client_ref, created_at
		FROM betting.bets ORDER BY created_at DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var bets []*Bet
	for rows.Next() {
		b := &Bet{}
		if err := rows.Scan(&b.ID, &b.MarketID, &b.SelectionID, &b.UserID, &b.Side, &b.Price, &b.Stake,
			&b.MatchedStake, &b.UnmatchedStake, &b.Profit, &b.Status, &b.ClientRef, &b.CreatedAt); err != nil {
			logger.Error("dbAllBets scan error, skipping row", "error", err)
			continue
		}
		bets = append(bets, b)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbAllBets rows iteration error", "error", err)
	}
	return bets
}

// ── DB-backed Audit reads ─────────────────────────────────────────────────────

func dbGetAuditLog(limit int) []*AuditEntry {
	rows, err := db.Query(`
		SELECT id, user_id, username, action, details, ip, created_at
		FROM betting.audit_log ORDER BY id DESC LIMIT $1`, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.Details, &e.IP, &e.Timestamp); err != nil {
			logger.Error("dbGetAuditLog scan error, skipping row", "error", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetAuditLog rows iteration error", "error", err)
	}
	return entries
}

func dbGetAuditLogForUser(userID int64, limit int) []*AuditEntry {
	rows, err := db.Query(`
		SELECT id, user_id, username, action, details, ip, created_at
		FROM betting.audit_log WHERE user_id=$1 ORDER BY id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.UserID, &e.Username, &e.Action, &e.Details, &e.IP, &e.Timestamp); err != nil {
			logger.Error("dbGetAuditLogForUser scan error, skipping row", "error", err)
			continue
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetAuditLogForUser rows iteration error", "error", err)
	}
	return entries
}

// ── DB-backed User balance sync ───────────────────────────────────────────────

func dbSyncUserBalance(userID int64) (float64, float64) {
	var balance, exposure float64
	db.QueryRow(`SELECT balance, exposure FROM auth.users WHERE id=$1`, userID).Scan(&balance, &exposure)
	return balance, exposure
}

// ── DB-backed Payment Transactions ────────────────────────────────────────────

func dbSavePaymentTx(tx *PaymentTx) {
	if _, err := db.Exec(`
		INSERT INTO betting.payment_transactions (id, user_id, direction, method, amount, currency, status, upi_id, wallet_address, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (id) DO UPDATE SET status=$7`,
		tx.ID, tx.UserID, tx.Direction, tx.Method, tx.Amount, tx.Currency, tx.Status, tx.UPIID, tx.Wallet, tx.CreatedAt); err != nil {
		logger.Error("dbSavePaymentTx failed", "tx_id", tx.ID, "error", err)
	}
}

func dbGetUserPayments(userID int64) []*PaymentTx {
	rows, err := db.Query(`
		SELECT id, user_id, direction, method, amount, currency, status, upi_id, wallet_address, created_at
		FROM betting.payment_transactions WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var txns []*PaymentTx
	for rows.Next() {
		tx := &PaymentTx{}
		var upiID, wallet sql.NullString
		if err := rows.Scan(&tx.ID, &tx.UserID, &tx.Direction, &tx.Method, &tx.Amount, &tx.Currency, &tx.Status, &upiID, &wallet, &tx.CreatedAt); err != nil {
			logger.Error("dbGetUserPayments scan error, skipping row", "error", err)
			continue
		}
		tx.UPIID = upiID.String
		tx.Wallet = wallet.String
		txns = append(txns, tx)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetUserPayments rows iteration error", "error", err)
	}
	return txns
}

// ── DB-backed Responsible Gambling ────────────────────────────────────────────

func dbSaveResponsibleLimits(userID int64, limits *ResponsibleGamblingLimits) {
	if _, err := db.Exec(`
		INSERT INTO betting.responsible_gambling (user_id, daily_deposit_limit, daily_loss_limit, max_stake_per_bet, session_limit_minutes, self_excluded, excluded_until, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,NOW())
		ON CONFLICT (user_id) DO UPDATE SET
			daily_deposit_limit=$2, daily_loss_limit=$3, max_stake_per_bet=$4,
			session_limit_minutes=$5, self_excluded=$6, excluded_until=$7, updated_at=NOW()`,
		userID, limits.DailyDeposit, limits.DailyLoss, limits.MaxStake, limits.SessionMinutes,
		limits.SelfExcluded, nullableTime(limits.ExcludedUntil)); err != nil {
		logger.Error("dbSaveResponsibleLimits failed", "error", err)
	}
}

func dbGetResponsibleLimits(userID int64) *ResponsibleGamblingLimits {
	l := &ResponsibleGamblingLimits{}
	var excludedUntil sql.NullTime
	err := db.QueryRow(`
		SELECT daily_deposit_limit, daily_loss_limit, max_stake_per_bet, session_limit_minutes, self_excluded, excluded_until
		FROM betting.responsible_gambling WHERE user_id=$1`, userID).Scan(
		&l.DailyDeposit, &l.DailyLoss, &l.MaxStake, &l.SessionMinutes, &l.SelfExcluded, &excludedUntil)
	if err != nil {
		return nil
	}
	if excludedUntil.Valid {
		l.ExcludedUntil = excludedUntil.Time.Format(time.RFC3339)
	}
	return l
}

func nullableTime(s string) interface{} {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	return t
}

// ── DB-backed Hierarchy queries ────────────────────────────────────────────

func dbGetChildren(userID int64) []*User {
	rows, err := db.Query(`
		SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		       credit_limit, commission_rate, status, referral_code, otp_enabled, is_demo, created_at
		FROM auth.users
		WHERE path LIKE (SELECT path FROM auth.users WHERE id=$1) || '.%'
		  AND id != $1
		ORDER BY id`, userID)
	if err != nil {
		logger.Error("dbGetChildren failed", "error", err)
		return nil
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		var parentID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Path, &parentID,
			&u.Balance, &u.Exposure, &u.CreditLimit, &u.CommissionRate,
			&u.Status, &u.ReferralCode, &u.OTPEnabled, &u.IsDemo, &u.CreatedAt); err != nil {
			logger.Error("dbGetChildren scan error, skipping row", "error", err)
			continue
		}
		if parentID.Valid {
			pid := parentID.Int64
			u.ParentID = &pid
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetChildren rows iteration error", "error", err)
	}
	return users
}

func dbGetDirectChildren(userID int64) []*User {
	rows, err := db.Query(`
		SELECT id, username, email, password_hash, role, path, parent_id, balance, exposure,
		       credit_limit, commission_rate, status, referral_code, otp_enabled, is_demo, created_at
		FROM auth.users WHERE parent_id=$1 ORDER BY id`, userID)
	if err != nil {
		logger.Error("dbGetDirectChildren failed", "error", err)
		return nil
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		u := &User{}
		var parentID sql.NullInt64
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.PasswordHash, &u.Role, &u.Path, &parentID,
			&u.Balance, &u.Exposure, &u.CreditLimit, &u.CommissionRate,
			&u.Status, &u.ReferralCode, &u.OTPEnabled, &u.IsDemo, &u.CreatedAt); err != nil {
			logger.Error("dbGetDirectChildren scan error, skipping row", "error", err)
			continue
		}
		if parentID.Valid {
			pid := parentID.Int64
			u.ParentID = &pid
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		logger.Error("dbGetDirectChildren rows iteration error", "error", err)
	}
	return users
}

// ── DB-backed Referral update ─────────────────────────────────────────────

func dbUpdateReferredBy(userID, referrerID int64) {
	if _, err := db.Exec(`UPDATE auth.users SET referred_by=$1, updated_at=NOW() WHERE id=$2`, referrerID, userID); err != nil {
		logger.Error("dbUpdateReferredBy failed", "error", err)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
