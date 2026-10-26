-- TRANSITION_PLAN review 2026-10-27: StreamingService generates STRING stream
-- ids ("stream_<contentID>_<unix>") and is called with string content ids
-- ("demo", "content-123"), but 009 declared streams.id and streams.content_id
-- as UUID (with an FK to contents(id)). Every CreateStream therefore failed
-- with `invalid input syntax for type uuid`, which the integration tests only
-- hid while the migration set failed to apply. The API-facing type is the
-- string one, so converge on VARCHAR(255) — the same treatment 035 gave
-- content_gating_rules.content_id.
ALTER TABLE streams DROP CONSTRAINT IF EXISTS fk_streams_content;

ALTER TABLE stream_qualities DROP CONSTRAINT IF EXISTS stream_qualities_stream_id_fkey;

ALTER TABLE streams ALTER COLUMN id DROP DEFAULT;
ALTER TABLE streams ALTER COLUMN id TYPE VARCHAR(255) USING id::text;
ALTER TABLE streams ALTER COLUMN content_id TYPE VARCHAR(255) USING content_id::text;

ALTER TABLE stream_qualities
    ALTER COLUMN stream_id TYPE VARCHAR(255) USING stream_id::text;

ALTER TABLE stream_qualities
    ADD CONSTRAINT stream_qualities_stream_id_fkey
    FOREIGN KEY (stream_id) REFERENCES streams(id) ON DELETE CASCADE;
