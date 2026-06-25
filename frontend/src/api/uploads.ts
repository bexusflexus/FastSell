import { apiFetch, readJson } from './client';
import type { ItemGroupDraft, NextUploadItemNumberResponse, UploadImageResponse, UploadSessionPayload, UploadSessionStatusResponse } from '../types/upload';

export async function uploadGroupedImages(payload: UploadSessionPayload, groups: ItemGroupDraft[]): Promise<UploadImageResponse> {
  const formData = new FormData();
  formData.append('metadata', JSON.stringify(payload));

  for (const group of groups) {
    for (const image of group.images) {
      formData.append(`file_${image.clientFileId}`, image.file, image.originalFilename);
    }
  }

  const response = await apiFetch('/api/uploads/images', {
    method: 'POST',
    body: formData,
  });

  return readJson<UploadImageResponse>(response);
}

export async function getUploadSession(uploadSessionId: string, signal?: AbortSignal): Promise<UploadSessionStatusResponse> {
  const response = await apiFetch(`/api/uploads/${encodeURIComponent(uploadSessionId)}`, { signal });
  return readJson<UploadSessionStatusResponse>(response);
}

export async function getNextUploadItemNumber(containerId?: string): Promise<NextUploadItemNumberResponse> {
  const params = new URLSearchParams();
  if (containerId?.trim()) {
    params.set('container_id', containerId.trim());
  }

  const query = params.toString();
  const response = await apiFetch(`/api/uploads/next-item-number${query ? `?${query}` : ''}`);
  return readJson<NextUploadItemNumberResponse>(response);
}
