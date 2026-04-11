-- Migration 012: auth.users missing columns (safety net)
--
-- The deleted cmd/server monolith used to run a boot-time migration loop
-- that ALTER TABLE ... ADD COLUMN IF NOT EXISTS'd these onto auth.users.
-- With the monolith gone the canonical schema lives entirely in this
-- directory, so we materialise every one of those ALTERs as a proper
-- numbered migration. Most of these columns are ALREADY created by
-- migration 004_production_hardening.sql (KYC block) and 007_audit_fixes.sql
-- (referral_code / otp_enabled / is_demo). This file is a self-contained
-- idempotent safety net: it succeeds as a no-op on any DB where those
-- earlier migrations already ran, and creates the missing columns on any
-- fresh DB that somehow skipped them.
--
-- Applying this migration twice is a no-op (every statement uses
-- ADD COLUMN IF NOT EXISTS).

SET search_path TO betting, auth, public;

-- ── Registration / login columns ─────────────────────────────────
-- dbCreateUser inserts referral_code; dbGetUser/dbAllUsers select
-- referral_code, otp_enabled, is_demo. Without these columns every
-- auth path silently fails with 42703 and the user cache stays empty.
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS referral_code TEXT DEFAULT '';
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS otp_enabled   BOOLEAN DEFAULT FALSE;
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS is_demo       BOOLEAN DEFAULT FALSE;

-- ── KYC / age verification ───────────────────────────────────────
-- Gates withdrawals and any regulated market entry.
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS date_of_birth        DATE;
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_status           TEXT DEFAULT 'pending';
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_verified_at      TIMESTAMPTZ;
ALTER TABLE auth.users ADD COLUMN IF NOT EXISTS kyc_rejection_reason TEXT;
