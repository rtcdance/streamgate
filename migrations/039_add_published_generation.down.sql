ALTER TABLE transcoding_tasks DROP COLUMN IF EXISTS output_generation;

ALTER TABLE contents DROP COLUMN IF EXISTS published_profile;
ALTER TABLE contents DROP COLUMN IF EXISTS published_generation;
