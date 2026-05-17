-- Add unique constraint on (contract_address, token_id) to prevent duplicate NFT records
ALTER TABLE nfts ADD CONSTRAINT IF NOT EXISTS nfts_contract_token_unique UNIQUE (contract_address, token_id);
