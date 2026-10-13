DROP INDEX IF EXISTS idx_outbox_status;
DROP TABLE IF EXISTS outbox;

DROP INDEX IF EXISTS idx_transcoding_tasks_lease;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS last_error;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS next_attempt_at;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS attempt;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS generation;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS lease_expires_at;
ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS lease_owner;
