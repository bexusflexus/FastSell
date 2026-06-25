import { ApiError, apiFetch, readJson } from './client';
import { API_BASE_URL } from './client';
import type {
  ApproveUploadGroupInput,
  ApproveUploadGroupResponse,
  GetReviewUploadGroupResponse,
  QueueReviewAIAssistInput,
  QueueReviewAIAssistResponse,
  ReviewGroupDeletePreview,
  ReviewGroupDeleteResponse,
  ReviewImageDeleteResponse,
  ReviewQueueResponse,
  ReviewUploadGroupImageMutationResponse,
} from '../types/review';
import type { ItemImageUploadEntry } from '../types/items';

export async function listReviewUploadGroups(options: { containerId?: string } = {}): Promise<ReviewQueueResponse> {
  const query = options.containerId ? `?container_id=${encodeURIComponent(options.containerId)}` : '';
  const response = await apiFetch(`/api/review/upload-groups${query}`);
  return readJson<ReviewQueueResponse>(response);
}

export async function approveReviewUploadGroup(
  groupId: string,
  input: ApproveUploadGroupInput,
): Promise<ApproveUploadGroupResponse> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}/approve`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<ApproveUploadGroupResponse>(response);
}

export async function approveAllReviewUploadGroup(
  groupId: string,
  input: ApproveUploadGroupInput = {},
): Promise<ApproveUploadGroupResponse> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}/approve-all`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<ApproveUploadGroupResponse>(response);
}

export async function getReviewUploadGroup(groupId: string): Promise<GetReviewUploadGroupResponse> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}`);
  return readJson<GetReviewUploadGroupResponse>(response);
}

export async function queueReviewUploadGroupAIAssist(groupId: string, input: QueueReviewAIAssistInput = {}): Promise<QueueReviewAIAssistResponse> {
  const userHint = input.user_hint?.trim();
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}/ai-assist`, {
    method: 'POST',
    ...(userHint
      ? {
          headers: {
            'Content-Type': 'application/json',
          },
          body: JSON.stringify({ user_hint: userHint }),
        }
      : {}),
  });
  return readJson<QueueReviewAIAssistResponse>(response);
}

export async function getReviewGroupDeletePreview(groupId: string): Promise<ReviewGroupDeletePreview> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}/delete-preview`);
  return readJson<ReviewGroupDeletePreview>(response);
}

export async function deleteReviewGroup(groupId: string): Promise<ReviewGroupDeleteResponse> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}`, {
    method: 'DELETE',
  });
  return readJson<ReviewGroupDeleteResponse>(response);
}

export async function uploadReviewGroupImages(
  groupId: string,
  files: File[],
  onProgress?: (entries: ItemImageUploadEntry[]) => void,
): Promise<ReviewUploadGroupImageMutationResponse> {
  if (files.length === 0) {
    throw new Error('Select at least one image to upload.');
  }

  const formData = new FormData();
  for (const file of files) {
    formData.append('images', file, file.name);
  }

  return new Promise<ReviewUploadGroupImageMutationResponse>((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', `${API_BASE_URL}/api/review/upload-groups/${encodeURIComponent(groupId)}/images`);

    const totalBytes = files.reduce((sum, file) => sum + file.size, 0);
    const reportProgress = (loaded: number, total: number, complete: boolean, failed: boolean) => {
      if (!onProgress) {
        return;
      }

      let consumed = 0;
      const entries: ItemImageUploadEntry[] = files.map((file) => {
        const fileLoaded = total > 0 ? Math.min(file.size, Math.max(0, loaded - consumed)) : 0;
        consumed += file.size;

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
          total_bytes: file.size,
          progress_percent: file.size > 0 ? Math.min(100, Math.round((fileLoaded / file.size) * 100)) : 0,
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
        reportProgress(totalBytes, totalBytes, true, false);
        try {
          resolve(JSON.parse(request.responseText) as ReviewUploadGroupImageMutationResponse);
        } catch (error) {
          reject(new ApiError('Failed to parse API response.', request.status, error));
        }
        return;
      }

      reportProgress(0, totalBytes, false, true);
      let payload: unknown = null;
      try {
        payload = request.responseText ? JSON.parse(request.responseText) : null;
      } catch {
        payload = request.responseText;
      }
      reject(new ApiError(readApiErrorMessage(payload) ?? `API request failed with HTTP ${request.status}.`, request.status, payload));
    };

    request.onerror = () => {
      reportProgress(0, totalBytes, false, true);
      reject(new ApiError('API is unreachable. Check the API URL and network connection.', null, null));
    };

    reportProgress(0, totalBytes, false, false);
    request.send(formData);
  });
}

export async function deleteReviewGroupImage(groupId: string, imageId: string): Promise<ReviewImageDeleteResponse> {
  const response = await apiFetch(`/api/review/upload-groups/${encodeURIComponent(groupId)}/images/${encodeURIComponent(imageId)}`, {
    method: 'DELETE',
  });
  return readJson<ReviewImageDeleteResponse>(response);
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
