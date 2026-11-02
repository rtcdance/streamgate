-- 035 later drops this constraint and retypes content_id to VARCHAR(255):
-- gating-rule subjects are string content IDs ("demo", "demo-content-1", ...),
-- not contents.id UUIDs. Applying the set a second time against an
-- already-migrated database therefore used to fail with
-- 'foreign key constraint "fk_gating_rules_content" cannot be implemented'
-- (Postgres 42804: the column is VARCHAR by then). Only express the FK while
-- the column is still UUID, so a replay is a no-op instead of an error.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'content_gating_rules'
          AND column_name = 'content_id'
          AND data_type = 'uuid'
    ) THEN
        ALTER TABLE content_gating_rules
            ADD CONSTRAINT fk_gating_rules_content
            FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
    END IF;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_gating_rules_content already exists, skipping';
END $$;

CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_owner ON transcoding_tasks(owner_wallet);
CREATE INDEX IF NOT EXISTS idx_nfts_owner_chain ON nfts(owner_address, chain_id);
