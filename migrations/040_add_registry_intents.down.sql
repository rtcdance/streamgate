DROP TABLE IF EXISTS http_idempotency;

DROP INDEX IF EXISTS idx_tx_signed_attempts_reservation;
DROP TABLE IF EXISTS tx_signed_attempts;

DROP TABLE IF EXISTS tx_nonce_reservations;

DROP INDEX IF EXISTS idx_content_registry_intents_status;
DROP TABLE IF EXISTS content_registry_intents;
