import { apiFetch, readJson } from './client';
import type {
  CreateInventoryGroupInput,
  InventoryGroup,
  ListInventoryGroupsResponse,
  UpdateInventoryGroupInput,
} from '../types/inventoryGroups';

export async function listInventoryGroups(options: { includeArchived?: boolean } = {}): Promise<InventoryGroup[]> {
  const query = options.includeArchived ? '?include_archived=true' : '';
  const response = await apiFetch(`/api/inventory-groups${query}`);
  const payload = await readJson<ListInventoryGroupsResponse>(response);
  return payload.inventory_groups;
}

export async function createInventoryGroup(input: CreateInventoryGroupInput): Promise<InventoryGroup> {
  const response = await apiFetch('/api/inventory-groups', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<InventoryGroup>(response);
}

export async function updateInventoryGroup(groupId: string, patch: UpdateInventoryGroupInput): Promise<InventoryGroup> {
  const response = await apiFetch(`/api/inventory-groups/${encodeURIComponent(groupId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(patch),
  });
  return readJson<InventoryGroup>(response);
}

export async function deleteInventoryGroup(groupId: string): Promise<void> {
  const response = await apiFetch(`/api/inventory-groups/${encodeURIComponent(groupId)}`, {
    method: 'DELETE',
  });

  if (response.status !== 204) {
    await readJson<unknown>(response);
  }
}
