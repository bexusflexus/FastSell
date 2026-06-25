import { apiFetch, readJson } from './client';
import type {
  ContainerDeletePreview,
  ContainerOption,
  ContainerSummary,
  CreateContainerInput,
  DeleteContainerResponse,
  UpdateContainerInput,
} from '../types/upload';

interface ApiContainer {
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
}

export async function listContainers(options: { includeArchived?: boolean } = {}): Promise<ContainerOption[]> {
  const query = options.includeArchived ? '?include_archived=true' : '';
  const response = await apiFetch(`/api/containers${query}`);
  const containers = await readJson<ApiContainer[]>(response);
  return containers.map(mapContainer);
}

export async function createContainer(input: CreateContainerInput): Promise<ContainerOption> {
  const response = await apiFetch('/api/containers', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  const container = await readJson<ApiContainer>(response);
  return mapContainer(container);
}

export async function updateContainer(containerId: string, patch: UpdateContainerInput): Promise<ContainerOption> {
  const response = await apiFetch(`/api/containers/${encodeURIComponent(containerId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(patch),
  });
  const container = await readJson<ApiContainer>(response);
  return mapContainer(container);
}

export async function getContainerSummary(containerId: string): Promise<ContainerSummary> {
  const response = await apiFetch(`/api/containers/${encodeURIComponent(containerId)}/summary`);
  return readJson<ContainerSummary>(response);
}

export async function getContainerDeletePreview(containerId: string): Promise<ContainerDeletePreview> {
  const response = await apiFetch(`/api/containers/${encodeURIComponent(containerId)}/delete-preview`);
  const data = await readJson<{ preview?: ContainerDeletePreview }>(response);
  // Support both flat and nested 'preview' key response
  if (data.preview) {
    return data.preview;
  }
  return data as unknown as ContainerDeletePreview;
}

export async function deleteContainer(containerId: string): Promise<DeleteContainerResponse> {
  const response = await apiFetch(`/api/containers/${encodeURIComponent(containerId)}`, {
    method: 'DELETE',
  });

  if (response.status === 204) {
    return {
      container_id: containerId,
      deleted: {
        containers: 1,
        items: 0,
        upload_sessions: 0,
        upload_groups: 0,
        image_assets: 0,
        files_deleted: 0,
        files_missing: 0,
        file_delete_errors: 0,
      },
      warnings: [],
    };
  }

  return readJson<DeleteContainerResponse>(response);
}

function mapContainer(container: ApiContainer): ContainerOption {
  return {
    id: container.id,
    name: container.name,
    type: container.type,
    containerTypeId: container.container_type_id,
    containerTypeName: container.container_type_name,
    locationId: container.location_id,
    locationName: container.location_name,
    locationDescription: container.location_description,
    notes: container.notes,
    createdDatetime: container.created_datetime,
    updatedDatetime: container.updated_datetime,
    archived: container.archived,
    archivedDatetime: container.archived_datetime,
  };
}
