-- SG-12 step 8: generation-private staging with an atomic publish switch.
-- Additive only: contents gains the published artifact generation (0 = legacy
-- flat layout, playable as before) and the profile that produced it;
-- transcoding_tasks gains the artifact generation allocated for the task's
-- staging prefix. Distinct from the lease `generation` column (migration 038).

ALTER TABLE contents ADD COLUMN IF NOT EXISTS published_generation BIGINT NOT NULL DEFAULT 0;
ALTER TABLE contents ADD COLUMN IF NOT EXISTS published_profile VARCHAR(64) NOT NULL DEFAULT '';

ALTER TABLE transcoding_tasks ADD COLUMN IF NOT EXISTS output_generation BIGINT NOT NULL DEFAULT 0;
