-- Add columns to content table
ALTER TABLE contents ADD COLUMN IF NOT EXISTS thumbnail_url VARCHAR(500);
ALTER TABLE contents ADD COLUMN IF NOT EXISTS duration INTEGER;
ALTER TABLE contents ADD COLUMN IF NOT EXISTS size BIGINT;
ALTER TABLE contents ADD COLUMN IF NOT EXISTS owner_id UUID;
ALTER TABLE contents ADD COLUMN IF NOT EXISTS status VARCHAR(50) DEFAULT 'pending';

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_contents_owner ON contents(owner_id);
CREATE INDEX IF NOT EXISTS idx_contents_status ON contents(status);