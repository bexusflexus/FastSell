ALTER TABLE whole_scene_candidates
    ADD COLUMN IF NOT EXISTS ai_assist_user_hint text NOT NULL DEFAULT '';

ALTER TABLE upload_groups
    ALTER COLUMN ai_assist_status SET DEFAULT 'not_requested'::text;

UPDATE upload_groups
SET ai_assist_status = 'not_requested'
WHERE ai_assist_status = 'idle';

ALTER TABLE items
    ALTER COLUMN disposition_code SET DEFAULT 'stored'::text;

UPDATE items
SET disposition_code = 'stored'
WHERE disposition_code IS NULL;

INSERT INTO item_dispositions (code, label, sort_order, is_active)
VALUES
    ('stored', 'Stored', 10, true),
    ('for_sale', 'For Sale', 20, true),
    ('in_use', 'In Use', 30, true),
    ('sale_pending', 'Sale Pending', 40, true),
    ('sold', 'Sold', 50, true),
    ('donated', 'Donated', 60, true),
    ('disposed', 'Disposed', 70, true)
ON CONFLICT (code) DO UPDATE
SET
    label = EXCLUDED.label,
    sort_order = EXCLUDED.sort_order,
    is_active = EXCLUDED.is_active;
