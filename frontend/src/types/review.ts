export interface ReviewContainer {
  id: string;
  name: string;
  type: string | null;
  location_description: string | null;
}

export interface ReviewImageAsset {
  image_asset_id: string;
  original_filename: string | null;
  stored_filename: string | null;
  file_path: string;
  thumbnail_path: string | null;
  mime_type: string | null;
  file_size_bytes: number | null;
  status: string;
  upload_order: number;
}

export interface ReviewUploadGroup {
  upload_group_id: string;
  upload_session_id: string;
  container: ReviewContainer | null;
  inventory_group_id: string | null;
  inventory_group_code: string | null;
  inventory_group_name: string | null;
  client_group_id: string | null;
  title: string | null;
  notes: string | null;
  sort_order: number;
  status: string;
  created_datetime: string;
  updated_datetime: string | null;
  image_count: number;
  processed_image_count: number;
  failed_image_count: number;
  ai_assist_status: 'not_requested' | 'queued' | 'processing' | 'succeeded' | 'failed';
  ai_assist_error_message: string | null;
  ai_assist_requested_datetime: string | null;
  ai_assist_started_datetime: string | null;
  ai_assist_completed_datetime: string | null;
  ai_suggested_title: string | null;
  ai_suggested_description: string | null;
  ai_suggested_approx_value: string | null;
  images: ReviewImageAsset[];
}

export interface ReviewQueueResponse {
  groups: ReviewUploadGroup[];
}

export interface GetReviewUploadGroupResponse {
  group: ReviewUploadGroup;
}

export interface ReviewUploadGroupImageMutationResponse {
  group: ReviewUploadGroup;
}

export interface ApproveUploadGroupInput {
  title?: string;
  description?: string;
  approx_value?: string;
  sold_date?: string | null;
  notes?: string | null;
  inventory_group_id?: string;
}

export interface ReviewItem {
  id: string;
  container_id: string | null;
  inventory_group_id: string | null;
  inventory_group_code: string | null;
  inventory_group_name: string | null;
  title: string | null;
  description: string | null;
  approx_value: string | null;
  sold_date: string | null;
  notes: string;
  ai_enriched: boolean;
  created_datetime: string;
  updated_datetime: string | null;
}

export interface ApproveUploadGroupResponse {
  item: ReviewItem;
  linked_image_count: number;
}

export interface QueueReviewAIAssistResponse {
  upload_group_id: string;
  ai_assist_status: 'queued' | 'processing' | 'succeeded' | 'failed' | 'not_requested';
  ai_assist_error_message: string | null;
  ai_suggested_title: string | null;
  ai_suggested_description: string | null;
  ai_suggested_approx_value: string | null;
  ai_assist_requested_datetime: string | null;
  ai_assist_started_datetime: string | null;
  ai_assist_completed_datetime: string | null;
}

export interface QueueReviewAIAssistInput {
  user_hint?: string;
}

export interface ReviewGroupDeletePreview {
  upload_group_id: string;
  upload_session_id: string;
  client_group_id: string | null;
  title: string | null;
  image_count: number;
  file_count: number;
  total_file_size_bytes: number;
  warnings: string[];
  files: Array<{
    image_asset_id: string;
    kind: string;
    path: string;
    exists: boolean;
    size_bytes: number;
  }>;
}

export interface ReviewGroupDeleteResponse {
  deleted_upload_group_id: string;
  deleted_image_asset_count: number;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}

export interface ReviewImageDeleteResponse {
  group: ReviewUploadGroup | null;
  deleted_image_asset_id: string;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}
