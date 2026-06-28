ALTER TABLE whole_scene_candidates
    DROP COLUMN IF EXISTS ai_assist_user_hint;

ALTER TABLE upload_groups
    ALTER COLUMN ai_assist_status SET DEFAULT 'idle'::text;

ALTER TABLE items
    ALTER COLUMN disposition_code DROP DEFAULT;
