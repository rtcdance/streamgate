-- FK to contents(id) removed (2026-10-11): gating rule subjects are string
-- content IDs that may not have a contents row (e.g. the h5 demo "demo").

CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at);
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_owner ON transcoding_tasks(owner_wallet);
CREATE INDEX IF NOT EXISTS idx_nfts_owner_chain ON nfts(owner_address, chain_id);
