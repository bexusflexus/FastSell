export interface InventoryContainerSummary {
  container_id: string;
  container_name: string;
  is_synthetic: boolean;
  container_type: string | null;
  container_type_id: string | null;
  container_type_name: string | null;
  location_id: string | null;
  location_name: string | null;
  location_description: string | null;
  notes: string | null;
  item_count: number;
  active_item_count: number;
  archived_item_count: number;
  for_sale_count: number;
  in_use_count: number;
  sale_pending_count: number;
  sold_count: number;
  donated_count: number;
  disposed_count: number;
  total_approx_value: number;
  latest_item_datetime: string | null;
}
