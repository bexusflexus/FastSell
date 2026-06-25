import type { InventoryContainer } from './items';
import type { IntakeContextPayload } from './upload';

export interface WholeSceneImageAsset {
  image_asset_id: string;
  client_file_id: string | null;
  original_filename: string | null;
  stored_filename: string | null;
  mime_type: string | null;
  file_size_bytes: number | null;
  status: string;
  error_message: string | null;
  upload_order: number;
  thumbnail_available: boolean;
  normalized_available: boolean;
}

export interface WholeSceneScanImage {
  id: string;
  image_asset_id: string;
  sort_order: number;
  created_datetime: string;
  image: WholeSceneImageAsset;
}

export interface WholeSceneAnalysisRun {
  id: string;
  run_number: number;
  status: string;
  ai_provider_config_id: string | null;
  provider_type: string;
  model_name: string;
  prompt_version: string;
  raw_response_available: boolean;
  error_message: string | null;
  queued_datetime: string;
  started_datetime: string | null;
  completed_datetime: string | null;
  created_datetime: string;
  updated_datetime: string | null;
}

export interface WholeSceneBoundingBox {
  x: number | null;
  y: number | null;
  width: number | null;
  height: number | null;
}

export interface WholeSceneCandidateAppearance {
  id: string;
  candidate_id: string;
  scan_image_id: string;
  source_image_index: number | null;
  bounding_box: WholeSceneBoundingBox;
  localization_data: unknown | null;
  confidence_label: string | null;
  notes: string | null;
  created_datetime: string;
}

export interface WholeSceneCandidateCrop {
  id: string;
  candidate_id: string;
  appearance_id: string | null;
  scan_image_id: string | null;
  crop_image_asset_id: string | null;
  status: string;
  is_preferred: boolean;
  bounding_box: WholeSceneBoundingBox;
  crop_metadata: unknown | null;
  error_message: string | null;
  created_datetime: string;
  updated_datetime: string | null;
  crop_image: WholeSceneImageAsset | null;
}

export interface WholeSceneApprovedItem {
  id: string;
  title: string | null;
  approx_value: string | null;
  current_inventory: boolean;
  archived: boolean;
}

export interface WholeSceneCandidate {
  id: string;
  analysis_run_id: string | null;
  source: 'ai' | 'manual' | string;
  status: 'proposed' | 'edited' | 'rejected' | 'approved' | string;
  title: string | null;
  description: string | null;
  approx_value: string | null;
  confidence_label: string | null;
  uncertainty_notes: string | null;
  raw_candidate: unknown | null;
  parse_warnings: string | null;
  ai_assist_status: 'idle' | 'queued' | 'processing' | 'succeeded' | 'failed' | string;
  ai_assist_error_message: string;
  ai_assist_requested_at: string | null;
  ai_assist_started_at: string | null;
  ai_assist_completed_at: string | null;
  ai_assist_provider_config_id: string | null;
  ai_assist_provider: string;
  ai_assist_model: string;
  approved_item_id: string | null;
  approved_datetime: string | null;
  rejected_datetime: string | null;
  created_by: string | null;
  created_datetime: string;
  updated_by: string | null;
  updated_datetime: string | null;
  approved_item: WholeSceneApprovedItem | null;
  appearances: WholeSceneCandidateAppearance[];
  crops: WholeSceneCandidateCrop[];
}

export interface WholeSceneInventoryGroup {
  id: string;
  code: string;
  name: string;
}

export interface WholeSceneScan {
  id: string;
  upload_session_id: string;
  container: InventoryContainer | null;
  location_id: string | null;
  location_name: string | null;
  location_detail: string | null;
  inventory_group: WholeSceneInventoryGroup;
  hint: string | null;
  status: string;
  created_by: string | null;
  created_datetime: string;
  updated_by: string | null;
  updated_datetime: string | null;
  images: WholeSceneScanImage[];
  analysis_runs: WholeSceneAnalysisRun[];
  latest_analysis_run: WholeSceneAnalysisRun | null;
  candidates: WholeSceneCandidate[];
}

export interface WholeSceneCandidateCounts {
  pending: number;
  approved: number;
  rejected: number;
  total: number;
}

export interface WholeSceneReviewScanSummary {
  id: string;
  upload_session_id: string;
  container: InventoryContainer | null;
  location_id: string | null;
  location_name: string | null;
  location_detail: string | null;
  inventory_group: WholeSceneInventoryGroup;
  hint: string | null;
  status: string;
  image_count: number;
  processed_image_count: number;
  failed_image_count: number;
  candidate_counts: WholeSceneCandidateCounts;
  latest_analysis_run: WholeSceneAnalysisRun | null;
  images: WholeSceneScanImage[];
  created_datetime: string;
  updated_datetime: string | null;
}

export interface ListWholeSceneReviewScansResponse {
  scans: WholeSceneReviewScanSummary[];
}

export interface WholeSceneFilePayload {
  client_file_id: string;
  original_filename: string;
  mime_type: string;
  size_bytes: number;
}

export interface CreateWholeSceneScanPayload {
  intake_context: IntakeContextPayload;
  hint?: string | null;
  inventory_group_id?: string | null;
  files: WholeSceneFilePayload[];
}

export interface GetWholeSceneScanResponse {
  scan: WholeSceneScan;
}

export interface WholeSceneCleanupSummary {
  deleted_image_asset_count: number;
  deleted_upload_session_count: number;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}

export interface WholeSceneCandidateMutationResponse {
  scan?: WholeSceneScan | null;
  scan_id: string;
  cleaned_up: boolean;
  approved_item_id: string | null;
  cleanup?: WholeSceneCleanupSummary | null;
}

export interface WholeSceneCandidateImageDeleteResponse {
  scan?: WholeSceneScan | null;
  scan_id: string;
  deleted_crop_id: string;
  deleted_image_asset_id: string;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}

export interface DeleteWholeSceneScanResponse {
  scan_id: string;
  cleanup: WholeSceneCleanupSummary;
}

export interface QueueWholeSceneAnalysisResponse {
  scan: WholeSceneScan;
  analysis_run: WholeSceneAnalysisRun | null;
  queued: boolean;
}

export interface PatchWholeSceneCandidateInput {
  title?: string | null;
  description?: string | null;
  approx_value?: string | null;
  confidence_label?: string | null;
  uncertainty_notes?: string | null;
}

export interface ApproveWholeSceneCandidateInput {
  inventory_group_id?: string | null;
}

export interface AssistWholeSceneCandidateInput {
  title?: string | null;
  description?: string | null;
  approx_value?: string | null;
  user_hint?: string | null;
}

export interface AddWholeSceneCandidateInput {
  title: string;
  description?: string | null;
  approx_value?: string | null;
  confidence_label?: string | null;
  uncertainty_notes?: string | null;
}
