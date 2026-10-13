-- SG-13 B (NFT 事件加速失效): durable event pipeline storage.
-- Additive only, compatible with the previous app version.
--
-- event_store_events: every consumed log, identity
--   (chainID, blockHash, txHash, logIndex) — duplicate delivery is a no-op.
-- event_checkpoints: per (chain, filter_key) cursor. filter_key embeds the
--   filter version and the sorted contract whitelist, so a filter change
--   never reuses an address-only checkpoint written under another filter.
-- event_block_journal: bounded recent block hashes (64 deep by policy) used
--   to find the common ancestor on reorg and to mark orphaned ranges.

CREATE TABLE IF NOT EXISTS event_store_events (
    event_id        TEXT PRIMARY KEY,
    chain_id        BIGINT NOT NULL,
    contract_address TEXT NOT NULL,
    block_number    BIGINT NOT NULL,
    block_hash      TEXT NOT NULL,
    tx_hash         TEXT NOT NULL,
    log_index       INTEGER NOT NULL,
    event_type      TEXT NOT NULL DEFAULT 'ContractEvent',
    payload         JSONB NOT NULL DEFAULT '{}',
    reorged         BOOLEAN NOT NULL DEFAULT FALSE,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_event_store_events_block
    ON event_store_events(chain_id, contract_address, block_number);

CREATE TABLE IF NOT EXISTS event_checkpoints (
    chain_id     BIGINT NOT NULL,
    filter_key   TEXT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   TEXT NOT NULL DEFAULT '',
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, filter_key)
);

CREATE TABLE IF NOT EXISTS event_block_journal (
    chain_id     BIGINT NOT NULL,
    block_number BIGINT NOT NULL,
    block_hash   TEXT NOT NULL,
    orphaned     BOOLEAN NOT NULL DEFAULT FALSE,
    recorded_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, block_number)
);
