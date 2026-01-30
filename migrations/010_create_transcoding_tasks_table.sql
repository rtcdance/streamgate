-- Create transcoding_tasks table
CREATE TABLE IF NOT EXISTS transcoding_tasks (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    content_id UUID NOT NULL,
    profile VARCHAR(50) NOT NULL,
    status VARCHAR(50) DEFAULT 'pending',
    progress INTEGER DEFAULT 0,
    input_url VARCHAR(500),
    output_url VARCHAR(500),
    error TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_content ON transcoding_tasks(content_id);
CREATE INDEX IF NOT EXISTS idx_transcoding_tasks_status ON transcoding_tasks(status);