-- Add timestamp columns to transcoding_tasks table
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS started_at TIMESTAMP;
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS completed_at TIMESTAMP;
