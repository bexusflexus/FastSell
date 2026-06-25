import { apiFetch, readJson } from './client';
import type {
  CreateLocationInput,
  DeleteLocationResponse,
  LocationDeletePreview,
  LocationOption,
  UpdateLocationInput,
} from '../types/locations';

interface ApiLocation {
  id: string;
  name: string;
  description: string | null;
  archived: boolean;
  archived_datetime: string | null;
  created_datetime: string;
  updated_datetime: string | null;
}

interface ApiLocationDeletePreview {
  id: string;
  name: string;
  can_delete: boolean;
  usage_count: number;
  blocking_reason: string | null;
}

interface ApiDeleteLocationResponse {
  id: string;
  name: string;
  deleted: boolean;
  usage_count: number;
}

interface ListLocationsResponse {
  locations: ApiLocation[];
}

export async function listLocations(options: { includeArchived?: boolean } = {}): Promise<LocationOption[]> {
  const query = options.includeArchived ? '?include_archived=true' : '';
  const response = await apiFetch(`/api/admin/locations${query}`);
  const payload = await readJson<ListLocationsResponse>(response);
  return payload.locations.map(mapLocation);
}

export async function createLocation(input: CreateLocationInput): Promise<LocationOption> {
  const response = await apiFetch('/api/admin/locations', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return mapLocation(await readJson<ApiLocation>(response));
}

export async function updateLocation(locationId: string, patch: UpdateLocationInput): Promise<LocationOption> {
  const response = await apiFetch(`/api/admin/locations/${encodeURIComponent(locationId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(patch),
  });
  return mapLocation(await readJson<ApiLocation>(response));
}

export async function archiveLocation(locationId: string): Promise<LocationOption> {
  const response = await apiFetch(`/api/admin/locations/${encodeURIComponent(locationId)}/archive`, {
    method: 'POST',
  });
  return mapLocation(await readJson<ApiLocation>(response));
}

export async function unarchiveLocation(locationId: string): Promise<LocationOption> {
  const response = await apiFetch(`/api/admin/locations/${encodeURIComponent(locationId)}/unarchive`, {
    method: 'POST',
  });
  return mapLocation(await readJson<ApiLocation>(response));
}

export async function getLocationDeletePreview(locationId: string): Promise<LocationDeletePreview> {
  const response = await apiFetch(`/api/admin/locations/${encodeURIComponent(locationId)}/delete-preview`);
  return mapLocationDeletePreview(await readJson<ApiLocationDeletePreview>(response));
}

export async function deleteLocation(locationId: string): Promise<DeleteLocationResponse> {
  const response = await apiFetch(`/api/admin/locations/${encodeURIComponent(locationId)}`, {
    method: 'DELETE',
  });
  return mapDeleteLocationResponse(await readJson<ApiDeleteLocationResponse>(response));
}

function mapLocation(location: ApiLocation): LocationOption {
  return {
    id: location.id,
    name: location.name,
    description: location.description,
    archived: location.archived,
    archivedDatetime: location.archived_datetime,
    createdDatetime: location.created_datetime,
    updatedDatetime: location.updated_datetime,
  };
}

function mapLocationDeletePreview(preview: ApiLocationDeletePreview): LocationDeletePreview {
  return {
    id: preview.id,
    name: preview.name,
    canDelete: preview.can_delete,
    usageCount: preview.usage_count,
    blockingReason: preview.blocking_reason,
  };
}

function mapDeleteLocationResponse(response: ApiDeleteLocationResponse): DeleteLocationResponse {
  return {
    id: response.id,
    name: response.name,
    deleted: response.deleted,
    usageCount: response.usage_count,
  };
}
