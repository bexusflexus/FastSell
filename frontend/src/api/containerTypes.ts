import { apiFetch, readJson } from './client';
import type {
  ContainerTypeDeletePreview,
  ContainerTypeOption,
  CreateContainerTypeInput,
  DeleteContainerTypeResponse,
  UpdateContainerTypeInput,
} from '../types/containerTypes';

interface ApiContainerType {
  id: string;
  name: string;
  description: string | null;
  archived: boolean;
  archived_datetime: string | null;
  created_datetime: string;
  updated_datetime: string | null;
}

interface ApiContainerTypeDeletePreview {
  id: string;
  name: string;
  can_delete: boolean;
  usage_count: number;
  blocking_reason: string | null;
}

interface ApiDeleteContainerTypeResponse {
  id: string;
  name: string;
  deleted: boolean;
  usage_count: number;
}

interface ListContainerTypesResponse {
  container_types: ApiContainerType[];
}

export async function listContainerTypes(options: { includeArchived?: boolean } = {}): Promise<ContainerTypeOption[]> {
  const query = options.includeArchived ? '?include_archived=true' : '';
  const response = await apiFetch(`/api/admin/container-types${query}`);
  const payload = await readJson<ListContainerTypesResponse>(response);
  return payload.container_types.map(mapContainerType);
}

export async function createContainerType(input: CreateContainerTypeInput): Promise<ContainerTypeOption> {
  const response = await apiFetch('/api/admin/container-types', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return mapContainerType(await readJson<ApiContainerType>(response));
}

export async function updateContainerType(containerTypeId: string, patch: UpdateContainerTypeInput): Promise<ContainerTypeOption> {
  const response = await apiFetch(`/api/admin/container-types/${encodeURIComponent(containerTypeId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(patch),
  });
  return mapContainerType(await readJson<ApiContainerType>(response));
}

export async function archiveContainerType(containerTypeId: string): Promise<ContainerTypeOption> {
  const response = await apiFetch(`/api/admin/container-types/${encodeURIComponent(containerTypeId)}/archive`, {
    method: 'POST',
  });
  return mapContainerType(await readJson<ApiContainerType>(response));
}

export async function unarchiveContainerType(containerTypeId: string): Promise<ContainerTypeOption> {
  const response = await apiFetch(`/api/admin/container-types/${encodeURIComponent(containerTypeId)}/unarchive`, {
    method: 'POST',
  });
  return mapContainerType(await readJson<ApiContainerType>(response));
}

export async function getContainerTypeDeletePreview(containerTypeId: string): Promise<ContainerTypeDeletePreview> {
  const response = await apiFetch(`/api/admin/container-types/${encodeURIComponent(containerTypeId)}/delete-preview`);
  return mapContainerTypeDeletePreview(await readJson<ApiContainerTypeDeletePreview>(response));
}

export async function deleteContainerType(containerTypeId: string): Promise<DeleteContainerTypeResponse> {
  const response = await apiFetch(`/api/admin/container-types/${encodeURIComponent(containerTypeId)}`, {
    method: 'DELETE',
  });
  return mapDeleteContainerTypeResponse(await readJson<ApiDeleteContainerTypeResponse>(response));
}

function mapContainerType(containerType: ApiContainerType): ContainerTypeOption {
  return {
    id: containerType.id,
    name: containerType.name,
    description: containerType.description,
    archived: containerType.archived,
    archivedDatetime: containerType.archived_datetime,
    createdDatetime: containerType.created_datetime,
    updatedDatetime: containerType.updated_datetime,
  };
}

function mapContainerTypeDeletePreview(preview: ApiContainerTypeDeletePreview): ContainerTypeDeletePreview {
  return {
    id: preview.id,
    name: preview.name,
    canDelete: preview.can_delete,
    usageCount: preview.usage_count,
    blockingReason: preview.blocking_reason,
  };
}

function mapDeleteContainerTypeResponse(response: ApiDeleteContainerTypeResponse): DeleteContainerTypeResponse {
  return {
    id: response.id,
    name: response.name,
    deleted: response.deleted,
    usageCount: response.usage_count,
  };
}
