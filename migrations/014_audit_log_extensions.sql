-- Migration 014: betting.audit_log legacy-compat columns + tamper-evident chain
--
-- Migration 002_phase2.sql created betting.audit_log with the "new" shape
-- (actor_id, entity_type, entity_id, old_value, new_value, ip_address).
-- Over time dbAddAudit in the deleted monolith grew a second, denormalised
-- write path that also wrote user_id, username, details (human text), ip
-- (string), plus a SHA-256 hash_chain column for tamper-evidence. Those
-- ALTERs only lived inside cmd/server/db.go's runMigrations — with the
-- monolith gone, a fresh DB rebuilt from the numbered migrations is missing
-- every one of them and the audit trail silently vanishes on insert (42703).
--
-- This migration materialises the full set. Every statement is idempotent.

SET search_path TO betting, auth, public;

-- Denormalised identity columns mirrored from auth.users at write time.
-- We keep actor_id (the FK) for referential integrity, but having the
-- flattened user_id + username pair avoids a join in every audit query.
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS user_id  BIGINT DEFAULT 0;
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS username TEXT   DEFAULT '';

-- Human-readable description used by the admin audit viewer instead of
-- old_value/new_value JSON diffs.
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS details TEXT DEFAULT '';

-- String (non-INET) IP column. audit_log already has ip_address INET from
-- migration 002, but dbAddAudit writes a free-form string (including the
-- sentinel value "system" used for server-initiated events), which does not
-- round-trip through INET. A separate text column keeps both writers happy.
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS ip TEXT DEFAULT '';

-- Even though 002 declares entity_type / entity_id NOT NULL, add IF NOT
-- EXISTS guards for any legacy databases whose audit_log was created before
-- those columns were added.
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS entity_type TEXT DEFAULT '';
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS entity_id   TEXT DEFAULT '';

-- Tamper-evident hash chain. Each new row's hash links to the previous row's
-- hash (SHA-256 of canonical row data + prev hash), verified by
-- /api/v1/admin/audit/verify. Detects post-hoc edits or deletions of any
-- row in the chain.
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS hash_chain TEXT DEFAULT '';

-- Index that backs the latest-N-for-user audit query in the admin panel.
CREATE INDEX IF NOT EXISTS idx_audit_user         ON betting.audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_audit_user_id_desc ON betting.audit_log(user_id, id DESC);
