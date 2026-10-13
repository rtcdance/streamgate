-- SG-12 steps 6/7: Postgres as the only status authority.
-- Additive only: lease columns for cross-instance task claiming, attempt
-- bookkeeping for the 3-attempt business cap, and the narrow outbox used for
-- task enqueue intents and DLQ intents. Compatible with the previous app
-- version (all new columns have defaults).

ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS lease_owner VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS lease_expires_at TIMESTAMPTZ;
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS generation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS attempt INTEGER NOT NULL DEFAULT 0;
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS next_attempt_at TIMESTAMPTZ;
ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS last_error TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_lease ON transcoding_tasks(status, lease_expires_at);

CREATE TABLE IF NOT EXISTS outbox (
    id TEXT NOT NULL,
    kind VARCHAR(32) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'pending',
    attempts INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    delivered_at TIMESTAMPTZ,
    PRIMARY KEY (kind, id)
);

CREATE INDEX IF NOT EXISTS idx_outbox_status ON outbox(status, created_at);
