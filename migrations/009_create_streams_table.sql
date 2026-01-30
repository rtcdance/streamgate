-- Create streams table
CREATE TABLE IF NOT EXISTS streams (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_id UUID REFERENCES content(id) ON DELETE CASCADE,
    stream_key VARCHAR(255) UNIQUE NOT NULL,
    status VARCHAR(50) DEFAULT 'idle',
    format VARCHAR(50),
    hls_path TEXT,
    dash_path TEXT,
    adaptive_bitrate BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_streams_content ON streams(content_id);
CREATE INDEX IF NOT EXISTS idx_streams_status ON streams(status);
CREATE INDEX IF NOT EXISTS idx_streams_key ON streams(stream_key);

GRANT ALL PRIVILEGES ON TABLE streams TO streamgate;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO streamgate;
