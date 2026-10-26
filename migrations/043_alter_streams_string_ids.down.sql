-- Reverse of 043: restore UUID columns + the FK to contents. Fails if rows
-- hold non-UUID stream/content ids — such rows must be removed first, which is
-- exactly why 043 exists.
ALTER TABLE stream_qualities DROP CONSTRAINT IF EXISTS stream_qualities_stream_id_fkey;

ALTER TABLE streams
    ALTER COLUMN id TYPE UUID USING id::uuid;
ALTER TABLE streams
    ALTER COLUMN content_id TYPE UUID USING content_id::uuid;
ALTER TABLE streams ALTER COLUMN id SET DEFAULT uuid_generate_v4();

ALTER TABLE stream_qualities
    ALTER COLUMN stream_id TYPE UUID USING stream_id::uuid;

ALTER TABLE stream_qualities
    ADD CONSTRAINT stream_qualities_stream_id_fkey
    FOREIGN KEY (stream_id) REFERENCES streams(id) ON DELETE CASCADE;

ALTER TABLE streams
    ADD CONSTRAINT fk_streams_content
    FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
