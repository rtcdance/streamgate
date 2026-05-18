CREATE TABLE IF NOT EXISTS content_categories (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(128) NOT NULL UNIQUE,
    slug VARCHAR(128) NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    parent_id VARCHAR(36) REFERENCES content_categories(id) ON DELETE SET NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS content_category_bindings (
    content_id VARCHAR(36) NOT NULL REFERENCES contents(id) ON DELETE CASCADE,
    category_id VARCHAR(36) NOT NULL REFERENCES content_categories(id) ON DELETE CASCADE,
    PRIMARY KEY (content_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_category_bindings_category ON content_category_bindings (category_id);
