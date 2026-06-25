export interface AdminMetricsSummary {
  total_current_inventory_items: number;
  total_current_approx_value: number;
  for_sale_count: number;
  in_use_count: number;
  sale_pending_count: number;
  sold_count: number;
  donated_count: number;
  disposed_count: number;
  archived_count: number;
  ai_enriched_count: number;
  missing_approx_value_count: number;
}

export interface AdminMetricsTopValueItem {
  id: string;
  title: string | null;
  approx_value: number | null;
  disposition_code: string | null;
  container_id: string | null;
  container_name: string | null;
  primary_image_id: string | null;
}

export interface AdminMetricsDuplicateItem {
  id: string;
  title: string | null;
  approx_value: number | null;
  disposition_code: string | null;
  container_id: string | null;
  container_name: string | null;
}

export interface AdminMetricsDuplicateTitleGroup {
  normalized_title: string;
  count: number;
  total_approx_value: number;
  items: AdminMetricsDuplicateItem[];
}

export interface AdminMetricsResponse {
  summary: AdminMetricsSummary;
  top_value_items: AdminMetricsTopValueItem[];
  duplicate_title_groups: AdminMetricsDuplicateTitleGroup[];
  generated_datetime: string;
}
