-- fk_uploads_owner intentionally omitted: uploads.owner_id is a wallet address
-- (VARCHAR(128) after migration 017), not a users.id (UUID), so the FK cannot
-- be expressed as a typed constraint. Application-level integrity is enforced
-- in service code.

DO $$ BEGIN
    ALTER TABLE streams ADD CONSTRAINT fk_streams_content FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
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
