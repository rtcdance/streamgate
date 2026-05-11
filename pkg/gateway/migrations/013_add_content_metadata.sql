-- Add metadata column to contents table
ALTER TABLE contents ADD COLUMN IF NOT EXISTS metadata JSONB;
