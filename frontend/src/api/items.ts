import { API_BASE_URL, ApiError, apiFetch, readJson } from './client';
import type {
  GetItemResponse,
  ItemImageDeleteResponse,
  ItemImageUploadEntry,
  ItemDeletePreview,
  ItemDeleteResponse,
  ListItemDispositionHistoryResponse,
  ListItemDispositionsResponse,
  ListItemsResponse,
  PatchItemInput,
} from '../types/items';

export interface ListItemsOptions {
  containerId?: string;
  search?: string;
  dispositionCode?: string;
  aiEnriched?: boolean;
  missingApproxValue?: boolean;
  archived?: boolean;
  includeArchived?: boolean;
  inventoryState?: 'current' | 'former' | 'all';
  inventoryGroupId?: string;
  limit?: number;
  offset?: number;
  sort?: string;
}

export async function listItems(options: ListItemsOptions = {}): Promise<ListItemsResponse> {
  const params = new URLSearchParams();

  if (options.containerId) {
    params.set('container_id', options.containerId);
  }
  if (options.search?.trim()) {
    params.set('search', options.search.trim());
  }
  if (options.dispositionCode?.trim()) {
    params.set('disposition_code', options.dispositionCode.trim());
  }
  if (typeof options.aiEnriched === 'boolean') {
    params.set('ai_enriched', String(options.aiEnriched));
  }
  if (typeof options.missingApproxValue === 'boolean') {
    params.set('missing_approx_value', String(options.missingApproxValue));
  }
  if (typeof options.archived === 'boolean') {
    params.set('archived', String(options.archived));
  } else if (options.includeArchived) {
    params.set('include_archived', 'true');
  }
  if (options.inventoryState) {
    params.set('inventory_state', options.inventoryState);
  }
  if (options.inventoryGroupId?.trim()) {
    params.set('inventory_group_id', options.inventoryGroupId.trim());
  }
  if (typeof options.limit === 'number') {
    params.set('limit', String(options.limit));
  }
  if (typeof options.offset === 'number') {
    params.set('offset', String(options.offset));
  }
  if (options.sort?.trim()) {
    params.set('sort', options.sort.trim());
  }

  const query = params.toString();
  const response = await apiFetch(`/api/items${query ? `?${query}` : ''}`);
  return readJson<ListItemsResponse>(response);
}

export async function getItem(itemId: string): Promise<GetItemResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}`);
  return readJson<GetItemResponse>(response);
}

export async function patchItem(itemId: string, input: PatchItemInput): Promise<GetItemResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetItemResponse>(response);
}

export async function archiveItem(itemId: string): Promise<GetItemResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/archive`, {
    method: 'POST',
  });
  return readJson<GetItemResponse>(response);
}

export async function unarchiveItem(itemId: string): Promise<GetItemResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/unarchive`, {
    method: 'POST',
  });
  return readJson<GetItemResponse>(response);
}

export async function getItemDeletePreview(itemId: string): Promise<ItemDeletePreview> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/delete-preview`);
  return readJson<ItemDeletePreview>(response);
}

export async function deleteItem(itemId: string): Promise<ItemDeleteResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}`, {
    method: 'DELETE',
  });
  return readJson<ItemDeleteResponse>(response);
}

export async function listItemDispositions(): Promise<ListItemDispositionsResponse> {
  const response = await apiFetch('/api/item-dispositions');
  return readJson<ListItemDispositionsResponse>(response);
}

export async function listItemDispositionHistory(itemId: string): Promise<ListItemDispositionHistoryResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/disposition-history`);
  return readJson<ListItemDispositionHistoryResponse>(response);
}

export async function uploadItemImages(
  itemId: string,
  files: File[],
  onProgress?: (entries: ItemImageUploadEntry[]) => void,
): Promise<GetItemResponse> {
  if (files.length === 0) {
    throw new Error('Select at least one image to upload.');
  }

  const formData = new FormData();
  for (const file of files) {
    formData.append('images', file, file.name);
  }

  return new Promise<GetItemResponse>((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', `${API_BASE_URL}/api/items/${encodeURIComponent(itemId)}/images`);

    const reportProgress = (loaded: number, total: number, complete: boolean, failed: boolean) => {
      if (!onProgress) {
        return;
      }

      let consumed = 0;
      const entries: ItemImageUploadEntry[] = files.map((file) => {
        const fileTotal = file.size;
        const fileLoaded = total > 0
          ? Math.min(fileTotal, Math.max(0, loaded - consumed))
          : 0;
        consumed += fileTotal;

        let status: ItemImageUploadEntry['status'] = 'pending';
        if (failed) {
          status = fileLoaded > 0 ? 'failed' : 'pending';
        } else if (complete) {
          status = 'complete';
        } else if (loaded > 0) {
          status = 'uploading';
        }

        return {
          file_name: file.name,
          loaded_bytes: fileLoaded,
          total_bytes: fileTotal,
          progress_percent: fileTotal > 0 ? Math.min(100, Math.round((fileLoaded / fileTotal) * 100)) : 0,
          status,
        };
      });

      onProgress(entries);
    };

    request.upload.onprogress = (event) => {
      if (event.lengthComputable) {
        reportProgress(event.loaded, event.total, false, false);
      }
    };

    request.onload = () => {
      if (request.status >= 200 && request.status < 300) {
        reportProgress(files.reduce((sum, file) => sum + file.size, 0), files.reduce((sum, file) => sum + file.size, 0), true, false);
        try {
          resolve(JSON.parse(request.responseText) as GetItemResponse);
        } catch (error) {
          reject(new ApiError('Failed to parse API response.', request.status, error));
        }
        return;
      }

      reportProgress(0, files.reduce((sum, file) => sum + file.size, 0), false, true);
      let payload: unknown = null;
      try {
        payload = request.responseText ? JSON.parse(request.responseText) : null;
      } catch {
        payload = request.responseText;
      }
      reject(new ApiError(readApiErrorMessage(payload) ?? `API request failed with HTTP ${request.status}.`, request.status, payload));
    };

    request.onerror = () => {
      reportProgress(0, files.reduce((sum, file) => sum + file.size, 0), false, true);
      reject(new ApiError('API is unreachable. Check the API URL and network connection.', null, null));
    };

    reportProgress(0, files.reduce((sum, file) => sum + file.size, 0), false, false);
    request.send(formData);
  });
}

export async function deleteItemImage(itemId: string, imageId: string): Promise<ItemImageDeleteResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/images/${encodeURIComponent(imageId)}`, {
    method: 'DELETE',
  });
  return readJson<ItemImageDeleteResponse>(response);
}

function readApiErrorMessage(payload: unknown): string | null {
  if (!payload || typeof payload !== 'object') {
    return null;
  }

  const maybeError = payload as { error?: unknown; message?: unknown };
  if (typeof maybeError.message === 'string' && maybeError.message.trim()) {
    return maybeError.message;
  }
  if (typeof maybeError.error === 'string' && maybeError.error.trim()) {
    return maybeError.error;
  }

  return null;
}
