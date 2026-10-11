-- TRANSITION_PLAN review 2026-10-11 (P0): gating rule subjects are STRING
-- content IDs ("demo", "demo-content-1", ...). The original UUID column +
-- FK to contents(id) made every non-UUID content impossible to rule (insert
-- failed with `invalid input syntax for type uuid`), and post-A8 the gate
-- resolver error turns that into a 500 on the manifest route.
--
-- Converges BOTH fresh databases (021 -> 035) and pre-existing databases
-- (021 applied with UUID, 035 alters in place) to the same schema.
ALTER TABLE content_gating_rules
    DROP CONSTRAINT IF EXISTS fk_gating_rules_content;

ALTER TABLE content_gating_rules
    ALTER COLUMN content_id TYPE VARCHAR(255) USING content_id::text;
