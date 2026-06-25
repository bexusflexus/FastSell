CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_trgm;

CREATE TABLE inventory_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    code text NOT NULL UNIQUE,
    name text NOT NULL,
    description text NOT NULL DEFAULT '',
    archived boolean NOT NULL DEFAULT false,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE item_dispositions (
    code text PRIMARY KEY,
    label text NOT NULL,
    sort_order integer NOT NULL DEFAULT 0,
    is_active boolean NOT NULL DEFAULT true
);

CREATE TABLE locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    description text,
    archived boolean NOT NULL DEFAULT false,
    archived_datetime timestamptz,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE container_types (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    description text,
    archived boolean NOT NULL DEFAULT false,
    archived_datetime timestamptz,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE containers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    type text,
    container_type_id uuid REFERENCES container_types(id) ON DELETE SET NULL,
    location_id uuid REFERENCES locations(id) ON DELETE SET NULL,
    location_description text,
    notes text,
    archived boolean NOT NULL DEFAULT false,
    archived_datetime timestamptz,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    container_id uuid REFERENCES containers(id) ON DELETE SET NULL,
    title text,
    description text,
    approx_value numeric(12,2),
    sold_price numeric(12,2),
    sold_date date,
    notes text NOT NULL DEFAULT '',
    disposition_code text REFERENCES item_dispositions(code) ON DELETE SET NULL,
    current_inventory boolean NOT NULL DEFAULT true,
    ai_enriched boolean NOT NULL DEFAULT false,
    archived boolean NOT NULL DEFAULT false,
    archived_datetime timestamptz,
    inventory_group_id uuid REFERENCES inventory_groups(id) ON DELETE SET NULL,
    location_id uuid REFERENCES locations(id) ON DELETE SET NULL,
    location_detail text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE item_disposition_history (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    previous_disposition_code text REFERENCES item_dispositions(code) ON DELETE SET NULL,
    new_disposition_code text NOT NULL REFERENCES item_dispositions(code) ON DELETE RESTRICT,
    previous_current_inventory boolean NOT NULL DEFAULT true,
    new_current_inventory boolean NOT NULL DEFAULT true,
    changed_datetime timestamptz NOT NULL DEFAULT now(),
    changed_by text
);

CREATE TABLE upload_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    container_id uuid REFERENCES containers(id) ON DELETE SET NULL,
    location_id uuid REFERENCES locations(id) ON DELETE SET NULL,
    location_detail text,
    source text NOT NULL DEFAULT 'web_upload',
    notes text,
    status text NOT NULL DEFAULT 'pending',
    completed_at timestamptz,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE upload_groups (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    container_id uuid REFERENCES containers(id) ON DELETE SET NULL,
    inventory_group_id uuid REFERENCES inventory_groups(id) ON DELETE SET NULL,
    client_group_id text,
    title text,
    notes text,
    sort_order integer NOT NULL DEFAULT 0,
    status text NOT NULL DEFAULT 'pending',
    ai_assist_status text NOT NULL DEFAULT 'idle',
    ai_assist_error_message text,
    ai_assist_requested_datetime timestamptz,
    ai_assist_started_datetime timestamptz,
    ai_assist_completed_datetime timestamptz,
    ai_assist_provider_config_id uuid,
    ai_suggested_title text,
    ai_suggested_description text,
    ai_suggested_approx_value numeric(12,2),
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE image_assets (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id uuid REFERENCES upload_sessions(id) ON DELETE CASCADE,
    upload_group_id uuid REFERENCES upload_groups(id) ON DELETE CASCADE,
    item_id uuid REFERENCES items(id) ON DELETE SET NULL,
    client_file_id text,
    original_filename text,
    stored_filename text,
    file_path text,
    thumbnail_path text,
    normalized_path text,
    file_hash text,
    mime_type text,
    file_size_bytes bigint,
    upload_order integer NOT NULL DEFAULT 0,
    is_original boolean NOT NULL DEFAULT true,
    status text NOT NULL DEFAULT 'pending',
    error_message text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE ai_provider_configs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name text NOT NULL,
    provider_type text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    active boolean NOT NULL DEFAULT false,
    base_url text,
    api_key_value text,
    api_key_env_var text,
    model_name text NOT NULL,
    vision_enabled boolean NOT NULL DEFAULT true,
    timeout_seconds integer NOT NULL DEFAULT 60,
    max_output_tokens integer,
    temperature numeric,
    last_test_datetime timestamptz,
    last_test_status text,
    last_error_message text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz,
    CONSTRAINT ai_provider_configs_active_enabled CHECK (active = false OR enabled = true)
);

ALTER TABLE upload_groups
    ADD CONSTRAINT upload_groups_ai_provider_fk
    FOREIGN KEY (ai_assist_provider_config_id)
    REFERENCES ai_provider_configs(id)
    ON DELETE SET NULL;

CREATE UNIQUE INDEX ai_provider_configs_one_active_idx
    ON ai_provider_configs (active)
    WHERE active = true;

CREATE TABLE sell_provider_configs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_type text NOT NULL UNIQUE,
    display_name text NOT NULL,
    enabled boolean NOT NULL DEFAULT true,
    sort_order integer NOT NULL DEFAULT 0,
    icon_key text NOT NULL DEFAULT 'store',
    base_url text,
    seller_profile_url text,
    notes text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE sale_listing_drafts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id uuid NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    sell_provider_config_id uuid REFERENCES sell_provider_configs(id) ON DELETE SET NULL,
    provider_type text NOT NULL,
    status text NOT NULL DEFAULT 'draft',
    title text NOT NULL,
    description text,
    asking_price numeric(12,2),
    currency text NOT NULL DEFAULT 'USD',
    listing_url text,
    notes text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

CREATE TABLE whole_scene_scans (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    upload_session_id uuid NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    container_id uuid REFERENCES containers(id) ON DELETE SET NULL,
    location_id uuid REFERENCES locations(id) ON DELETE SET NULL,
    location_detail text,
    inventory_group_id uuid NOT NULL REFERENCES inventory_groups(id) ON DELETE RESTRICT,
    hint text,
    status text NOT NULL DEFAULT 'created',
    created_by text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_by text,
    updated_datetime timestamptz
);

CREATE TABLE whole_scene_scan_images (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id uuid NOT NULL REFERENCES whole_scene_scans(id) ON DELETE CASCADE,
    image_asset_id uuid NOT NULL REFERENCES image_assets(id) ON DELETE CASCADE,
    sort_order integer NOT NULL DEFAULT 0,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    UNIQUE (scan_id, image_asset_id)
);

CREATE TABLE whole_scene_analysis_runs (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id uuid NOT NULL REFERENCES whole_scene_scans(id) ON DELETE CASCADE,
    run_number integer NOT NULL,
    status text NOT NULL DEFAULT 'queued',
    ai_provider_config_id uuid REFERENCES ai_provider_configs(id) ON DELETE SET NULL,
    provider_type text NOT NULL,
    model_name text NOT NULL,
    prompt_version text NOT NULL,
    prompt_text text,
    request_payload jsonb,
    raw_response_text text,
    error_message text,
    queued_datetime timestamptz NOT NULL DEFAULT now(),
    started_datetime timestamptz,
    completed_datetime timestamptz,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz,
    UNIQUE (scan_id, run_number)
);

CREATE TABLE whole_scene_candidates (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    scan_id uuid NOT NULL REFERENCES whole_scene_scans(id) ON DELETE CASCADE,
    analysis_run_id uuid REFERENCES whole_scene_analysis_runs(id) ON DELETE SET NULL,
    source text NOT NULL DEFAULT 'manual',
    status text NOT NULL DEFAULT 'proposed',
    title text,
    description text,
    approx_value numeric(12,2),
    confidence_label text,
    uncertainty_notes text,
    raw_candidate jsonb,
    parse_warnings text,
    ai_assist_status text NOT NULL DEFAULT 'idle',
    ai_assist_error_message text NOT NULL DEFAULT '',
    ai_assist_requested_at timestamptz,
    ai_assist_started_at timestamptz,
    ai_assist_completed_at timestamptz,
    ai_assist_provider_config_id uuid REFERENCES ai_provider_configs(id) ON DELETE SET NULL,
    ai_assist_provider text NOT NULL DEFAULT '',
    ai_assist_model text NOT NULL DEFAULT '',
    approved_item_id uuid REFERENCES items(id) ON DELETE SET NULL,
    approved_datetime timestamptz,
    rejected_datetime timestamptz,
    created_by text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_by text,
    updated_datetime timestamptz
);

CREATE TABLE whole_scene_candidate_appearances (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id uuid NOT NULL REFERENCES whole_scene_candidates(id) ON DELETE CASCADE,
    scan_image_id uuid NOT NULL REFERENCES whole_scene_scan_images(id) ON DELETE CASCADE,
    source_image_index integer,
    bounding_box_x numeric,
    bounding_box_y numeric,
    bounding_box_width numeric,
    bounding_box_height numeric,
    localization_data jsonb,
    confidence_label text,
    notes text,
    created_datetime timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE whole_scene_candidate_crops (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    candidate_id uuid NOT NULL REFERENCES whole_scene_candidates(id) ON DELETE CASCADE,
    appearance_id uuid REFERENCES whole_scene_candidate_appearances(id) ON DELETE SET NULL,
    scan_image_id uuid REFERENCES whole_scene_scan_images(id) ON DELETE SET NULL,
    crop_image_asset_id uuid REFERENCES image_assets(id) ON DELETE SET NULL,
    status text NOT NULL DEFAULT 'generated',
    is_preferred boolean NOT NULL DEFAULT false,
    bounding_box_x numeric,
    bounding_box_y numeric,
    bounding_box_width numeric,
    bounding_box_height numeric,
    crop_metadata jsonb,
    error_message text,
    created_datetime timestamptz NOT NULL DEFAULT now(),
    updated_datetime timestamptz
);

INSERT INTO inventory_groups (code, name, description)
VALUES ('household_items', 'Household Items', 'Default inventory group');

INSERT INTO item_dispositions (code, label, sort_order, is_active)
VALUES
    ('stored', 'Stored', 10, true),
    ('for_sale', 'For Sale', 20, true),
    ('in_use', 'In Use', 30, true),
    ('sale_pending', 'Sale Pending', 40, true),
    ('sold', 'Sold', 50, true),
    ('donated', 'Donated', 60, true),
    ('disposed', 'Disposed', 70, true);

CREATE INDEX containers_container_type_id_idx ON containers (container_type_id);
CREATE INDEX containers_location_id_idx ON containers (location_id);
CREATE INDEX containers_archived_idx ON containers (archived);

CREATE INDEX items_container_id_idx ON items (container_id);
CREATE INDEX items_inventory_group_id_idx ON items (inventory_group_id);
CREATE INDEX items_location_id_idx ON items (location_id);
CREATE INDEX items_disposition_code_idx ON items (disposition_code);
CREATE INDEX items_current_inventory_idx ON items (current_inventory);
CREATE INDEX items_archived_idx ON items (archived);
CREATE INDEX items_title_trgm_idx ON items USING gin (title gin_trgm_ops);
CREATE INDEX items_description_trgm_idx ON items USING gin (description gin_trgm_ops);
CREATE INDEX items_notes_trgm_idx ON items USING gin (notes gin_trgm_ops);

CREATE INDEX item_disposition_history_item_id_idx ON item_disposition_history (item_id);

CREATE INDEX upload_sessions_container_id_idx ON upload_sessions (container_id);
CREATE INDEX upload_sessions_location_id_idx ON upload_sessions (location_id);
CREATE INDEX upload_sessions_status_idx ON upload_sessions (status);

CREATE INDEX upload_groups_session_id_idx ON upload_groups (session_id);
CREATE INDEX upload_groups_container_id_idx ON upload_groups (container_id);
CREATE INDEX upload_groups_inventory_group_id_idx ON upload_groups (inventory_group_id);
CREATE INDEX upload_groups_status_idx ON upload_groups (status);
CREATE INDEX upload_groups_ai_assist_status_idx ON upload_groups (ai_assist_status);

CREATE INDEX image_assets_session_id_idx ON image_assets (session_id);
CREATE INDEX image_assets_upload_group_id_idx ON image_assets (upload_group_id);
CREATE INDEX image_assets_item_id_idx ON image_assets (item_id);
CREATE INDEX image_assets_status_idx ON image_assets (status);

CREATE INDEX sale_listing_drafts_item_id_idx ON sale_listing_drafts (item_id);
CREATE INDEX sale_listing_drafts_provider_idx ON sale_listing_drafts (sell_provider_config_id);
CREATE INDEX sale_listing_drafts_status_idx ON sale_listing_drafts (status);

CREATE INDEX whole_scene_scans_upload_session_id_idx ON whole_scene_scans (upload_session_id);
CREATE INDEX whole_scene_scans_container_id_idx ON whole_scene_scans (container_id);
CREATE INDEX whole_scene_scans_inventory_group_id_idx ON whole_scene_scans (inventory_group_id);
CREATE INDEX whole_scene_scans_status_idx ON whole_scene_scans (status);

CREATE INDEX whole_scene_scan_images_scan_id_idx ON whole_scene_scan_images (scan_id);
CREATE INDEX whole_scene_scan_images_image_asset_id_idx ON whole_scene_scan_images (image_asset_id);

CREATE INDEX whole_scene_analysis_runs_scan_id_idx ON whole_scene_analysis_runs (scan_id);
CREATE INDEX whole_scene_analysis_runs_status_idx ON whole_scene_analysis_runs (status);

CREATE INDEX whole_scene_candidates_scan_id_idx ON whole_scene_candidates (scan_id);
CREATE INDEX whole_scene_candidates_analysis_run_id_idx ON whole_scene_candidates (analysis_run_id);
CREATE INDEX whole_scene_candidates_status_idx ON whole_scene_candidates (status);
CREATE INDEX whole_scene_candidates_ai_assist_status_idx ON whole_scene_candidates (ai_assist_status);
CREATE INDEX whole_scene_candidates_approved_item_id_idx ON whole_scene_candidates (approved_item_id);

CREATE INDEX whole_scene_candidate_appearances_candidate_id_idx ON whole_scene_candidate_appearances (candidate_id);
CREATE INDEX whole_scene_candidate_appearances_scan_image_id_idx ON whole_scene_candidate_appearances (scan_image_id);

CREATE INDEX whole_scene_candidate_crops_candidate_id_idx ON whole_scene_candidate_crops (candidate_id);
CREATE INDEX whole_scene_candidate_crops_crop_image_asset_id_idx ON whole_scene_candidate_crops (crop_image_asset_id);
