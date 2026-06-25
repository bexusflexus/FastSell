export interface InventoryContainer {
  id: string;
  name: string;
  type: string | null;
  container_type_id: string | null;
  container_type_name: string | null;
  location_id: string | null;
  location_name: string | null;
  location_description: string | null;
}

export interface InventoryImage {
  image_asset_id: string;
  original_filename: string | null;
  stored_filename: string | null;
  mime_type: string | null;
  file_size_bytes: number | null;
  status: string;
  upload_order: number;
  thumbnail_available: boolean;
  normalized_available: boolean;
}

export interface ItemImageDeleteResponse {
  item: InventoryItemDetail;
  deleted_image_asset_id: string;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}

export interface InventoryItemSummary {
  id: string;
  title: string | null;
  description: string | null;
  approx_value: string | null;
  sold_price: string | null;
  sold_date: string | null;
  notes: string;
  disposition_code: string | null;
  disposition_label: string | null;
  current_inventory: boolean;
  ai_enriched: boolean;
  archived: boolean;
  archived_datetime: string | null;
  created_datetime: string;
  updated_datetime: string | null;
  inventory_group_id: string | null;
  inventory_group_code: string | null;
  inventory_group_name: string | null;
  container: InventoryContainer | null;
  location_id: string | null;
  location_name: string | null;
  location_detail: string | null;
  image_count: number;
  primary_image: InventoryImage | null;
}

export interface InventoryItemDetail {
  id: string;
  title: string | null;
  description: string | null;
  approx_value: string | null;
  sold_price: string | null;
  sold_date: string | null;
  notes: string;
  disposition_code: string | null;
  disposition_label: string | null;
  current_inventory: boolean;
  ai_enriched: boolean;
  archived: boolean;
  archived_datetime: string | null;
  created_datetime: string;
  updated_datetime: string | null;
  inventory_group_id: string | null;
  inventory_group_code: string | null;
  inventory_group_name: string | null;
  container: InventoryContainer | null;
  location_id: string | null;
  location_name: string | null;
  location_detail: string | null;
  image_count: number;
  images: InventoryImage[];
}

export interface ListItemsResponse {
  items: InventoryItemSummary[];
  total_count: number;
  limit: number;
  offset: number;
}

export interface GetItemResponse {
  item: InventoryItemDetail;
}

export interface ItemDispositionHistoryEntry {
  id: string;
  item_id: string;
  previous_disposition_code: string | null;
  previous_disposition_label: string | null;
  new_disposition_code: string;
  new_disposition_label: string | null;
  previous_current_inventory: boolean;
  new_current_inventory: boolean;
  changed_datetime: string;
  changed_by: string | null;
}

export interface ListItemDispositionHistoryResponse {
  history: ItemDispositionHistoryEntry[];
}

export interface ItemImageUploadEntry {
  file_name: string;
  loaded_bytes: number;
  total_bytes: number;
  progress_percent: number;
  status: 'pending' | 'uploading' | 'complete' | 'failed';
}

export interface DeletePreviewFile {
  image_asset_id: string;
  kind: string;
  path: string;
  exists: boolean;
  size_bytes: number;
}

export interface ItemDeletePreview {
  item_id: string;
  title: string | null;
  container: InventoryContainer | null;
  image_count: number;
  file_count: number;
  total_file_size_bytes: number;
  linked_upload_group_count: number;
  linked_upload_session_count: number;
  warnings: string[];
  files: DeletePreviewFile[];
}

export interface ItemDeleteResponse {
  deleted_item_id: string;
  deleted_image_asset_count: number;
  deleted_file_count: number;
  missing_file_count: number;
  warnings: string[];
}

export interface PatchItemInput {
  title?: string | null;
  description?: string | null;
  approx_value?: string | null;
  sold_price?: string | null;
  sold_date?: string | null;
  notes?: string | null;
  disposition_code?: string | null;
  container_id?: string | null;
  location_id?: string | null;
  location_detail?: string | null;
  inventory_group_id?: string;
}

export interface ItemDisposition {
  code: string;
  label: string;
  sort_order: number;
  is_active: boolean;
}

export interface ListItemDispositionsResponse {
  dispositions: ItemDisposition[];
}
