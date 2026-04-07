-- Fix schema mismatches between migrations and mockserver

-- audit_log: add columns mockserver expects
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS user_id BIGINT;
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS username TEXT DEFAULT '';
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS details TEXT DEFAULT '';
ALTER TABLE betting.audit_log ADD COLUMN IF NOT EXISTS ip TEXT DEFAULT '';
ALTER TABLE betting.audit_log ALTER COLUMN entity_type SET DEFAULT 'system';
ALTER TABLE betting.audit_log ALTER COLUMN entity_id SET DEFAULT '';

-- notifications: drop restrictive type check (mockserver uses dynamic types)
ALTER TABLE betting.notifications DROP CONSTRAINT IF EXISTS notifications_type_check;
