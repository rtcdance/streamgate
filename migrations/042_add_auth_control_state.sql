-- SG-06 (恢复凭据失效): the authoritative restore epoch.
--
-- Exactly one row, forever (the CHECK pins the primary key to 1). It is a random
-- UUID that the restore drill regenerates BEFORE a restored stack takes traffic,
-- so every credential minted against the pre-restore state stops verifying: an
-- access token, a refresh, and a playback capability alike.
--
-- Layering, not duplication: auth_epoch (Redis) versions ONE login session and a
-- logout moves it; restore_epoch (here, Postgres) versions the WHOLE credential
-- universe and only a restore moves it. A session cannot outlive a restore.
--
-- The column deliberately has NO database-side default: the single row is
-- created by the application with the UUID library it already uses, so this
-- migration adds no pgcrypto/uuid-ossp extension.
--
-- Additive only, compatible with the previous app version.
CREATE TABLE IF NOT EXISTS auth_control_state (
    id            SMALLINT    PRIMARY KEY CHECK (id = 1),
    restore_epoch UUID        NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
