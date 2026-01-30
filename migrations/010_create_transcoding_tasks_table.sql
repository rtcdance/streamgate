-- Create transcoding_tasks table
CREATE TABLE IF NOT EXISTS transcoding_tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_id UUID REFERENCES content(id) ON DELETE CASCADE,
    status VARCHAR(50) DEFAULT 'pending',
    input_path TEXT,
    output_path TEXT,
    format VARCHAR(50),
    profile VARCHAR(50),
    progress INTEGER DEFAULT 0,
    error_message TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_content ON transcoding_tasks(content_id);
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_status ON transcoding_tasks(status);

GRANT ALL PRIVILEGES ON TABLE transcoding_tasks TO streamgate;
GRANT ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public TO streamgate;
