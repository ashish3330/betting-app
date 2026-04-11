-- Migration 011: Notification idempotency unique index
--
-- internal/notification/service.go:Send issues:
--
--   INSERT INTO notifications (..., idempotency_key, ...)
--   VALUES (..., $6, ...)
--   ON CONFLICT (idempotency_key) DO NOTHING
--
-- but earlier migrations only created the notifications table without the
-- idempotency_key column or the supporting unique index. The ON CONFLICT
-- clause therefore raises `there is no unique or exclusion constraint
-- matching the ON CONFLICT specification` and the entire INSERT fails -- so
-- duplicate notifications were never actually deduped.
--
-- This migration:
--   1. Adds the idempotency_key column to betting.notifications if it does
--      not yet exist (idempotent ALTER for re-runs).
--   2. Creates a partial UNIQUE index on idempotency_key, ignoring NULLs so
--      legacy rows and notifications without an idempotency key are still
--      allowed.
--
-- The column / table names match internal/notification/service.go exactly:
--   table  = betting.notifications
--   column = idempotency_key

ALTER TABLE betting.notifications
    ADD COLUMN IF NOT EXISTS idempotency_key TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS notifications_idempotency_key_unique
    ON betting.notifications (idempotency_key)
    WHERE idempotency_key IS NOT NULL;
