import type { SellProviderType } from './sell';

export type ListingDraftStatus = 'draft' | 'ready' | 'listed' | 'archived';

export interface ListingDraftItemSummary {
  id: string;
  title: string | null;
  approx_value: string | null;
  disposition_code: string | null;
  container_id: string | null;
  container_name: string | null;
}

export interface ListingPhotoExportFile {
  filename: string;
  size_bytes: number;
}

export interface ListingPhotoExport {
  export_id: string;
  export_path: string;
  expires_at: string;
  image_count: number;
  files: ListingPhotoExportFile[];
  warnings: string[];
}

export interface ListingDraft {
  id: string;
  item_id: string;
  sell_provider_config_id: string | null;
  provider_type: SellProviderType;
  provider_display_name: string | null;
  provider_icon_key: string | null;
  status: ListingDraftStatus;
  title: string;
  description: string | null;
  asking_price: string | null;
  currency: string;
  listing_url: string | null;
  notes: string | null;
  created_datetime: string;
  updated_datetime: string | null;
  item?: ListingDraftItemSummary | null;
  photo_export?: ListingPhotoExport | null;
}

export interface ListListingDraftsResponse {
  drafts: ListingDraft[];
}

export interface GetListingDraftResponse {
  draft: ListingDraft;
}

export interface CreateListingDraftInput {
  sell_provider_config_id?: string | null;
  provider_type?: SellProviderType | null;
}

export interface UpdateListingDraftInput {
  title?: string | null;
  description?: string | null;
  asking_price?: string | null;
  currency?: string | null;
  status?: ListingDraftStatus | null;
  listing_url?: string | null;
  notes?: string | null;
}

export interface DeleteListingDraftResponse {
  draft_id: string;
  deleted: boolean;
}
