-- Migration 016: betting.kyc_documents
--
-- Storage table for uploaded KYC document metadata. The actual files live
-- in object storage (S3 / R2) and this row records the type, size, MIME,
-- storage URL, and review status. Created by the deleted cmd/server
-- runMigrations loop and never ported to a numbered migration.
--
-- Idempotent: safe to re-run.

SET search_path TO betting, auth, public;

CREATE TABLE IF NOT EXISTS betting.kyc_documents (
    id           BIGSERIAL PRIMARY KEY,
    user_id      BIGINT NOT NULL REFERENCES auth.users(id),
    doc_type     TEXT NOT NULL DEFAULT '',
    filename     TEXT NOT NULL DEFAULT '',
    size_bytes   BIGINT DEFAULT 0,
    content_type TEXT DEFAULT '',
    storage_url  TEXT DEFAULT '',
    status       TEXT DEFAULT 'pending_review',
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kyc_user ON betting.kyc_documents (user_id);

COMMENT ON TABLE betting.kyc_documents IS 'Per-user KYC document metadata; file payloads live in object storage.';
