-- fk_uploads_owner intentionally omitted: uploads.owner_id is a wallet address
-- (VARCHAR(128) after migration 017), not a users.id (UUID), so the FK cannot
-- be expressed as a typed constraint. Application-level integrity is enforced
-- in service code.

-- Replay-safe: migration 043 retypes streams.content_id to VARCHAR(255) and
-- drops this constraint, so a database that is already at 043 (a CI service
-- container migrated up front, a reused test database) cannot express it any
-- more. Adding it unconditionally made the whole migration fail with
-- "fk_streams_content cannot be implemented", which the test helper turned
-- into a skip. The other four foreign keys below stay typed: no later
-- migration changes those columns.
DO $$ BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema()
          AND table_name = 'streams' AND column_name = 'content_id'
          AND data_type = 'uuid'
    ) THEN
        ALTER TABLE streams ADD CONSTRAINT fk_streams_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
    ELSE
        RAISE NOTICE 'streams.content_id is no longer UUID (migration 043); skipping fk_streams_content';
    END IF;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_streams_content already exists, skipping';
END $$;

DO $$ BEGIN
    ALTER TABLE transcoding_tasks ADD CONSTRAINT fk_transcoding_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_transcoding_content already exists, skipping';
END $$;

DO $$ BEGIN
    ALTER TABLE nfts ADD CONSTRAINT fk_nfts_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE SET NULL;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_nfts_content already exists, skipping';
END $$;

DO $$ BEGIN
    ALTER TABLE playback_events ADD CONSTRAINT fk_playback_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_playback_content already exists, skipping';
END $$;

DO $$ BEGIN
    ALTER TABLE content_stats ADD CONSTRAINT fk_stats_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_stats_content already exists, skipping';
END $$;
