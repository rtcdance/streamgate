-- SG-13 A (登记与交易): durable off-chain consistency for on-chain content
-- registration. Additive only, compatible with the previous app version.
--
-- content_registry_intents: the single register intent per (chain, registry,
-- content id), carrying the six-state lifecycle (pending / submitted /
-- confirmed / failed / broadcast_unknown / disabled) plus the recorded block
-- evidence. Written in the SAME transaction as the contents row.
-- tx_nonce_reservations: UNIQUE(chain_id, signer, nonce) nonce ledger; the
-- advisory-locked allocator appends here so two processes never return the
-- same first nonce.
-- tx_signed_attempts: every signed raw tx, persisted BEFORE broadcast,
-- unique by tx_hash and linked to its reservation via reservation_key; an
-- explicit same-nonce replacement adds a row here, never a reservation.
-- http_idempotency: HTTP retry records; the unique key is
-- (actor, operation, scope, idempotency_key) and the canonical payload
-- digest is the comparison column, NOT part of the key.

CREATE TABLE IF NOT EXISTS content_registry_intents (
    content_id       TEXT NOT NULL,
    chain_id         BIGINT NOT NULL,
    registry_address TEXT NOT NULL,
    creator          TEXT NOT NULL,
    content_digest   TEXT NOT NULL,
    metadata_uri     TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'pending',
    tx_hash          TEXT NOT NULL DEFAULT '',
    nonce            BIGINT,
    block_number     BIGINT,
    block_hash       TEXT NOT NULL DEFAULT '',
    confirmed_at     TIMESTAMPTZ,
    last_error       TEXT NOT NULL DEFAULT '',
    lease_owner      TEXT NOT NULL DEFAULT '',
    lease_expires_at TIMESTAMPTZ,
    attempt          INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, registry_address, content_id)
);

CREATE INDEX IF NOT EXISTS idx_content_registry_intents_status
    ON content_registry_intents(status, lease_expires_at);

CREATE TABLE IF NOT EXISTS tx_nonce_reservations (
    chain_id    BIGINT NOT NULL,
    signer      TEXT NOT NULL,
    nonce       BIGINT NOT NULL,
    reserved_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (chain_id, signer, nonce)
);

CREATE TABLE IF NOT EXISTS tx_signed_attempts (
    reservation_key TEXT NOT NULL,
    chain_id        BIGINT NOT NULL,
    signer          TEXT NOT NULL,
    nonce           BIGINT NOT NULL,
    tx_hash         TEXT NOT NULL,
    raw_tx          TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'signed',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tx_hash)
);

CREATE INDEX IF NOT EXISTS idx_tx_signed_attempts_reservation
    ON tx_signed_attempts(reservation_key);

CREATE TABLE IF NOT EXISTS http_idempotency (
    actor           TEXT NOT NULL,
    operation       TEXT NOT NULL,
    scope           TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload_digest  TEXT NOT NULL,
    response        JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (actor, operation, scope, idempotency_key)
);
