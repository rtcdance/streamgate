-- Reverse of 035: restore UUID + FK. Fails if VARCHAR rows hold non-UUID
-- content IDs (e.g. "demo") — such rows must be removed first, which is
-- exactly why 035 exists.
ALTER TABLE content_gating_rules
    ALTER COLUMN content_id TYPE UUID USING content_id::uuid;

ALTER TABLE content_gating_rules
    ADD CONSTRAINT fk_gating_rules_content
    FOREIGN KEY (content_id) REFERENCES contents(id) ON DELETE CASCADE;
