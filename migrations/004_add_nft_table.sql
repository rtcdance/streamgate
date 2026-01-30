-- Create nfts table
CREATE TABLE IF NOT EXISTS nfts (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    contract_address VARCHAR(255) NOT NULL,
    token_id VARCHAR(255) NOT NULL,
    owner_address VARCHAR(255) NOT NULL,
    metadata_url VARCHAR(500),
    content_id UUID,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_nfts_contract ON nfts(contract_address);
CREATE INDEX IF NOT EXISTS idx_nfts_owner ON nfts(owner_address);
CREATE INDEX IF NOT EXISTS idx_nfts_content ON nfts(content_id);