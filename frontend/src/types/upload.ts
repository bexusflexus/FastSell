export interface ContainerOption {
  id: string;
  name: string;
  type: string | null;
  containerTypeId: string | null;
  containerTypeName: string | null;
  locationId: string | null;
  locationName: string | null;
  locationDescription: string | null;
  notes: string | null;
  createdDatetime?: string;
  updatedDatetime?: string | null;
  archived: boolean;
  archivedDatetime: string | null;
}

export interface ImageDraft {
  clientFileId: string;
  originalFilename: string;
  sizeBytes: number;
  mimeType: string;
  objectUrl: string;
  file: File;
  role?: string;
}

export interface ItemGroupDraft {
  clientGroupId: string;
  title: string;
  notes: string;
  images: ImageDraft[];
  autoTitle: boolean;
}

export interface UploadSessionDraft {
  selectedContainerId: string | null;
  noContainer: boolean;
  inventoryGroupId: string | null;
  locationId: string | null;
  locationDetail: string;
  sessionNotes: string;
  groups: ItemGroupDraft[];
}

export interface IntakeContextPayload {
  container_id: string;
  container_name: string;
  no_container: boolean;
  location_id: string;
  location_detail: string;
}

export interface UploadFilePayload {
  client_file_id: string;
  original_filename: string;
  mime_type: string;
  size_bytes: number;
}

export interface UploadGroupPayload {
  client_group_id: string;
  inventory_group_id?: string;
  title: string;
  notes: string;
  files: UploadFilePayload[];
}

export interface UploadSessionPayload {
  intake_context: IntakeContextPayload;
  session_notes: string;
  groups: UploadGroupPayload[];
}

export interface SimulatedUploadResult {
  upload_session_id: string;
  accepted_at: string;
  status: 'simulated';
  file_count: number;
  group_count: number;
}

export interface CreateContainerInput {
  name: string;
  type?: string;
  container_type_id?: string;
  location_id?: string;
  location_description?: string;
  notes?: string;
}

export interface UpdateContainerInput {
  name?: string;
  type?: string | null;
  container_type_id?: string | null;
  location_id?: string | null;
  location_description?: string | null;
  notes?: string | null;
  archived?: boolean;
}

export interface ContainerSummary {
  container: {
    id: string;
    name: string;
    type: string | null;
    container_type_id: string | null;
    container_type_name: string | null;
    location_id: string | null;
    location_name: string | null;
    location_description: string | null;
    notes: string | null;
    created_datetime: string;
    updated_datetime: string | null;
    archived: boolean;
    archived_datetime: string | null;
  };
  summary: {
    upload_session_count: number;
    upload_group_count: number;
    image_count: number;
    pending_image_count: number;
    uploaded_image_count: number;
    processing_image_count: number;
    processed_image_count: number;
    failed_image_count: number;
    latest_upload_datetime: string | null;
  };
}

export interface ContainerDeletePreview {
  container: ContainerSummary['container'];
  counts: {
    items: number;
    upload_sessions: number;
    upload_groups: number;
    image_assets: number;
    file_paths: number;
  };
}

export interface DeleteContainerResponse {
  container_id: string;
  deleted: {
    containers: number;
    items: number;
    upload_sessions: number;
    upload_groups: number;
    image_assets: number;
    files_deleted: number;
    files_missing: number;
    file_delete_errors: number;
  };
  warnings: string[];
}

export interface UploadImageResponseFile {
  image_asset_id: string;
  client_file_id: string;
  original_filename: string;
  stored_filename: string;
  status: string;
}

export interface UploadImageResponseGroup {
  upload_group_id: string;
  client_group_id: string;
  inventory_group_id: string;
  title: string | null;
  files: UploadImageResponseFile[];
}

export interface UploadImageResponse {
  upload_session_id: string;
  status: string;
  groups: UploadImageResponseGroup[];
}

export type UploadState = 'idle' | 'uploading' | 'success' | 'error';

export type UploadSessionStatus = 'pending' | 'processing' | 'processed' | 'failed' | 'completed_with_errors' | string;
export type ImageAssetStatus = 'pending' | 'uploaded' | 'processing' | 'processed' | 'failed' | string;

export interface UploadSessionStatusFile {
  image_asset_id: string;
  client_file_id: string | null;
  original_filename: string | null;
  stored_filename: string | null;
  file_path: string;
  mime_type: string | null;
  file_size_bytes: number | null;
  upload_order: number;
  status: ImageAssetStatus;
  error_message: string | null;
}

export interface UploadSessionStatusGroup {
  upload_group_id: string;
  client_group_id: string | null;
  inventory_group_id: string | null;
  inventory_group_code: string | null;
  inventory_group_name: string | null;
  title: string | null;
  notes: string | null;
  sort_order: number;
  files: UploadSessionStatusFile[];
}

export interface UploadSessionStatusResponse {
  upload_session_id: string;
  status: UploadSessionStatus;
  groups: UploadSessionStatusGroup[];
}

export interface NextUploadItemNumberResponse {
  next_number: number;
  step: number;
  title_prefix: string;
  suggested_title: string;
  scope: 'container' | 'global';
}

export type PollingState = 'idle' | 'polling' | 'stopped' | 'timeout' | 'error' | 'complete';
