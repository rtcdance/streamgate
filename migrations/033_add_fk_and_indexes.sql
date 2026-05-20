ALTER TABLE content_gating_rules
    ADD CONSTRAINT fk_gating_rules_content
    FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_owner ON transcoding_tasks(owner_wallet);
CREATE INDEX IF NOT EXISTS idx_nfts_owner_chain ON nfts(owner_address, chain_id);
