-- TRANSITION_PLAN SG-06 step 3: the authoritative rule version.
--
-- Rule changes (create / update / delete) and the monotonic version bump must
-- happen in the SAME transaction, so a reader can never observe the new rules
-- under the old version or the reverse. The version lives in its own table so a
-- bump never rewrites the content row.
--
-- content_id is TEXT, matching content_gating_rules.content_id after 035
-- (string content IDs such as "demo" are legal).
CREATE TABLE IF NOT EXISTS content_rule_versions (
    content_id TEXT PRIMARY KEY,
    version    BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- No index beyond the primary key: every read is a point lookup by content_id.
