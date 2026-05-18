CREATE TABLE IF NOT EXISTS playback_events (
    id VARCHAR(36) PRIMARY KEY,
    content_id VARCHAR(36) NOT NULL,
    wallet_address VARCHAR(66) NOT NULL,
    event_type VARCHAR(32) NOT NULL,
    duration_seconds INT NOT NULL DEFAULT 0,
    playback_token_jti VARCHAR(36) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    ip_address VARCHAR(45) NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_playback_events_content_id ON playback_events (content_id);
CREATE INDEX IF NOT EXISTS idx_playback_events_wallet ON playback_events (wallet_address);
CREATE INDEX IF NOT EXISTS idx_playback_events_created_at ON playback_events (created_at);

CREATE TABLE IF NOT EXISTS content_stats (
    content_id VARCHAR(36) PRIMARY KEY REFERENCES contents(id) ON DELETE CASCADE,
    total_plays INT NOT NULL DEFAULT 0,
    unique_viewers INT NOT NULL DEFAULT 0,
    total_watch_seconds BIGINT NOT NULL DEFAULT 0,
    avg_watch_seconds INT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP NOT NULL DEFAULT NOW()
);
