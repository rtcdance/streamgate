-- Stream quality variants table
-- Used by StreamingService.getStreamQualities() for HLS manifest generation
CREATE TABLE IF NOT EXISTS stream_qualities (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    stream_id UUID NOT NULL REFERENCES streams(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    resolution VARCHAR(20) NOT NULL,
    bitrate INTEGER NOT NULL,
    url TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_stream_qualities_stream_id ON stream_qualities(stream_id);
