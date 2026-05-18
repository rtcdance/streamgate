CREATE INDEX IF NOT EXISTS idx_contents_created_at ON contents(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_streams_content_status ON streams(content_id, status);
CREATE INDEX IF NOT EXISTS idx_uploads_owner_status ON uploads(owner_id, status);
CREATE INDEX IF NOT EXISTS idx_nfts_owner_contract ON nfts(owner_address, contract_address);
CREATE INDEX IF NOT EXISTS idx_transactions_user_type ON transactions(user_id, type);
