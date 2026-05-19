CREATE TABLE IF NOT EXISTS content_categories (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(255) NOT NULL UNIQUE,
    description TEXT DEFAULT '',
    parent_id UUID REFERENCES content_categories(id),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_categories_slug ON content_categories(slug);
CREATE INDEX IF NOT EXISTS idx_categories_parent ON content_categories(parent_id);

CREATE TABLE IF NOT EXISTS content_category_bindings (
    content_id UUID NOT NULL,
    category_id UUID NOT NULL REFERENCES content_categories(id) ON DELETE CASCADE,
    PRIMARY KEY (content_id, category_id)
);

CREATE INDEX IF NOT EXISTS idx_category_bindings_category ON content_category_bindings(category_id);
