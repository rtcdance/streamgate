CREATE UNIQUE INDEX IF NOT EXISTS contents_owner_url_unique ON contents (owner_id, url) WHERE url IS NOT NULL AND url != '';

CREATE UNIQUE INDEX IF NOT EXISTS transcoding_tasks_content_profile_active ON transcoding_tasks (content_id, profile) WHERE status IN ('pending', 'processing');
