DROP TRIGGER IF EXISTS trg_tasks_updated_at ON tasks;
DROP TRIGGER IF EXISTS trg_transcoding_tasks_updated_at ON transcoding_tasks;
DROP TRIGGER IF EXISTS trg_streams_updated_at ON streams;
DROP TRIGGER IF EXISTS trg_nfts_updated_at ON nfts;
DROP TRIGGER IF EXISTS trg_uploads_updated_at ON uploads;
DROP TRIGGER IF EXISTS trg_users_updated_at ON users;
DROP TRIGGER IF EXISTS trg_contents_updated_at ON contents;
DROP FUNCTION IF EXISTS update_updated_at_column();
