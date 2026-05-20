ALTER TABLE content_gating_rules DROP CONSTRAINT IF EXISTS fk_gating_rules_content;
DROP INDEX IF EXISTS idx_tasks_created_at;
DROP INDEX IF EXISTS idx_transcoding_tasks_owner;
DROP INDEX IF EXISTS idx_nfts_owner_chain;
