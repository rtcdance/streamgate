DO $$ BEGIN
    ALTER TABLE uploads ADD CONSTRAINT fk_uploads_owner FOREIGN KEY (owner_id) REFERENCES users(id) ON DELETE CASCADE;
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint fk_uploads_owner already exists, skipping';
END $$;

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
