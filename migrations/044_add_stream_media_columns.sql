-- TRANSITION_PLAN review 2026-10-27: StreamingService reads and writes
-- streams.url, streams.playlist and streams.duration (GetStream,
-- GetStreamByID, UpdateStreamPlaylist), but 009 only created manifest_url.
-- Every read failed with `column "url" does not exist` once the integration
-- tests actually reached PostgreSQL. Add the columns the service contract
-- needs; manifest_url stays for callers that already populate it.
ALTER TABLE streams ADD COLUMN IF NOT EXISTS url VARCHAR(500) DEFAULT '';
ALTER TABLE streams ADD COLUMN IF NOT EXISTS playlist TEXT DEFAULT '';
ALTER TABLE streams ADD COLUMN IF NOT EXISTS duration INTEGER DEFAULT 0;
