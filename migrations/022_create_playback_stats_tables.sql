CREATE TABLE IF NOT EXISTS playback_events (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_id UUID NOT NULL,
    wallet_address VARCHAR(255) NOT NULL,
    event_type VARCHAR(50) NOT NULL,
    duration_seconds INTEGER NOT NULL DEFAULT 0,
    playback_token_jti VARCHAR(255) DEFAULT '',
    user_agent TEXT DEFAULT '',
    ip_address VARCHAR(45) DEFAULT '',
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_playback_events_content ON playback_events(content_id);
CREATE INDEX IF NOT EXISTS idx_playback_events_wallet ON playback_events(wallet_address);
CREATE INDEX IF NOT EXISTS idx_playback_events_type ON playback_events(event_type);
CREATE INDEX IF NOT EXISTS idx_playback_events_created ON playback_events(created_at DESC);

CREATE TABLE IF NOT EXISTS content_stats (
    content_id UUID PRIMARY KEY,
    total_plays INTEGER NOT NULL DEFAULT 0,
    unique_viewers INTEGER NOT NULL DEFAULT 0,
    total_watch_seconds BIGINT NOT NULL DEFAULT 0,
    avg_watch_seconds INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
