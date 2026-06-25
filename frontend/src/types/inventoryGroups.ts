export interface InventoryGroup {
  id: string;
  code: string;
  name: string;
  description: string;
  archived: boolean;
  created_datetime: string;
  updated_datetime: string | null;
}

export interface ListInventoryGroupsResponse {
  inventory_groups: InventoryGroup[];
}

export interface CreateInventoryGroupInput {
  code: string;
  name: string;
  description?: string;
}

export interface UpdateInventoryGroupInput {
  code?: string;
  name?: string;
  description?: string;
  archived?: boolean;
}

export interface DeleteInventoryGroupBlockedResponse {
  message: string;
  counts: {
    items: number;
    upload_groups: number;
  };
}
