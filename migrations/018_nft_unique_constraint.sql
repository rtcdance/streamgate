-- Add unique constraint on (contract_address, token_id) to prevent duplicate NFT records
DO $$ BEGIN
    ALTER TABLE nfts ADD CONSTRAINT nfts_contract_token_unique UNIQUE (contract_address, token_id);
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint nfts_contract_token_unique already exists';
END $$;
