ALTER TABLE content_stats DROP CONSTRAINT IF EXISTS fk_stats_content;
ALTER TABLE playback_events DROP CONSTRAINT IF EXISTS fk_playback_content;
ALTER TABLE nfts DROP CONSTRAINT IF EXISTS fk_nfts_content;
ALTER TABLE transcoding_tasks DROP CONSTRAINT IF EXISTS fk_transcoding_content;
ALTER TABLE streams DROP CONSTRAINT IF EXISTS fk_streams_content;
