CREATE TABLE IF NOT EXISTS content_gating_rules (
    id VARCHAR(36) PRIMARY KEY,
    content_id VARCHAR(36) NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    contract_address VARCHAR(66) NOT NULL,
    token_id VARCHAR(128) NOT NULL DEFAULT '',
    chain_id BIGINT NOT NULL DEFAULT 1,
    standard VARCHAR(16) NOT NULL DEFAULT 'erc721',
    min_balance INT NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_gating_rules_content_id ON content_gating_rules (content_id);
CREATE INDEX IF NOT EXISTS idx_gating_rules_contract ON content_gating_rules (contract_address, chain_id);
