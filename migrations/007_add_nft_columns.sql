-- Add columns to nfts table
ALTER TABLE nfts ADD COLUMN IF NOT EXISTS metadata_url VARCHAR(500);
ALTER TABLE nfts ADD COLUMN IF NOT EXISTS content_id UUID;