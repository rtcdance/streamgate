CREATE TABLE IF NOT EXISTS content_gating_rules (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_id UUID NOT NULL,
    contract_address VARCHAR(255) NOT NULL,
    token_id VARCHAR(255) DEFAULT '',
    chain_id BIGINT NOT NULL DEFAULT 1,
    standard VARCHAR(50) NOT NULL DEFAULT 'erc721',
    min_balance INTEGER NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_gating_rules_content ON content_gating_rules(content_id);
CREATE INDEX IF NOT EXISTS idx_gating_rules_contract ON content_gating_rules(contract_address);
CREATE INDEX IF NOT EXISTS idx_gating_rules_active ON content_gating_rules(content_id, is_active);
