DO $$ BEGIN
    ALTER TABLE nfts DROP CONSTRAINT IF EXISTS nfts_contract_token_chain_unique;
    ALTER TABLE nfts ADD CONSTRAINT nfts_contract_token_unique UNIQUE (contract_address, token_id);
EXCEPTION WHEN duplicate_object THEN
    RAISE NOTICE 'constraint nfts_contract_token_unique already exists';
END $$;
