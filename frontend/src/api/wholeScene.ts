import { API_BASE_URL, ApiError, apiFetch, readJson } from './client';
import type { ItemImageUploadEntry } from '../types/items';
import type {
  AddWholeSceneCandidateInput,
  ApproveWholeSceneCandidateInput,
  AssistWholeSceneCandidateInput,
  CreateWholeSceneScanPayload,
  DeleteWholeSceneScanResponse,
  GetWholeSceneScanResponse,
  ListWholeSceneReviewScansResponse,
  PatchWholeSceneCandidateInput,
  QueueWholeSceneAnalysisResponse,
  WholeSceneCandidateImageDeleteResponse,
  WholeSceneCandidateMutationResponse,
  WholeSceneScan,
} from '../types/wholeScene';

export async function listWholeSceneReviewScans(options: { containerId?: string } = {}): Promise<ListWholeSceneReviewScansResponse> {
  const query = options.containerId ? `?container_id=${encodeURIComponent(options.containerId)}` : '';
  const response = await apiFetch(`/api/review/whole-scene-scans${query}`);
  return readJson<ListWholeSceneReviewScansResponse>(response);
}

export async function createWholeSceneScan(payload: CreateWholeSceneScanPayload, files: File[]): Promise<WholeSceneScan> {
  const formData = new FormData();
  formData.append('metadata', JSON.stringify(payload));

  payload.files.forEach((filePayload, index) => {
    const file = files[index];
    if (file) {
      formData.append(`file_${filePayload.client_file_id}`, file, filePayload.original_filename);
    }
  });

  const response = await apiFetch('/api/whole-scene/scans', {
    method: 'POST',
    body: formData,
  });
  return (await readJson<GetWholeSceneScanResponse>(response)).scan;
}

export async function getWholeSceneScan(scanId: string, signal?: AbortSignal): Promise<WholeSceneScan> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}`, { signal });
  return (await readJson<GetWholeSceneScanResponse>(response)).scan;
}

export async function deleteWholeSceneScan(scanId: string): Promise<DeleteWholeSceneScanResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}`, {
    method: 'DELETE',
  });
  return readJson<DeleteWholeSceneScanResponse>(response);
}

export async function queueWholeSceneAnalysis(scanId: string): Promise<QueueWholeSceneAnalysisResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/analyze`, {
    method: 'POST',
  });
  return readJson<QueueWholeSceneAnalysisResponse>(response);
}

export async function patchWholeSceneCandidate(
  scanId: string,
  candidateId: string,
  input: PatchWholeSceneCandidateInput,
): Promise<WholeSceneCandidateMutationResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<WholeSceneCandidateMutationResponse>(response);
}

export async function assistWholeSceneCandidate(
  scanId: string,
  candidateId: string,
  input: AssistWholeSceneCandidateInput = {},
): Promise<WholeSceneCandidateMutationResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}/ai-assist`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<WholeSceneCandidateMutationResponse>(response);
}

export async function rejectWholeSceneCandidate(scanId: string, candidateId: string): Promise<WholeSceneCandidateMutationResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}/reject`, {
    method: 'POST',
  });
  return readJson<WholeSceneCandidateMutationResponse>(response);
}

export async function addWholeSceneCandidate(scanId: string, input: AddWholeSceneCandidateInput): Promise<WholeSceneCandidateMutationResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<WholeSceneCandidateMutationResponse>(response);
}

export async function approveWholeSceneCandidate(
  scanId: string,
  candidateId: string,
  input: ApproveWholeSceneCandidateInput = {},
): Promise<WholeSceneCandidateMutationResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}/approve`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<WholeSceneCandidateMutationResponse>(response);
}

export async function uploadWholeSceneCandidateImages(
  scanId: string,
  candidateId: string,
  files: File[],
  onProgress?: (entries: ItemImageUploadEntry[]) => void,
): Promise<WholeSceneCandidateMutationResponse> {
  if (files.length === 0) {
    throw new Error('Select at least one image to upload.');
  }

  const formData = new FormData();
  for (const file of files) {
    formData.append('images', file, file.name);
  }

  return new Promise<WholeSceneCandidateMutationResponse>((resolve, reject) => {
    const request = new XMLHttpRequest();
    request.open('POST', `${API_BASE_URL}/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}/images`);

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
          resolve(JSON.parse(request.responseText) as WholeSceneCandidateMutationResponse);
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

export async function deleteWholeSceneCandidateImage(scanId: string, candidateId: string, cropId: string): Promise<WholeSceneCandidateImageDeleteResponse> {
  const response = await apiFetch(`/api/whole-scene/scans/${encodeURIComponent(scanId)}/candidates/${encodeURIComponent(candidateId)}/images/${encodeURIComponent(cropId)}`, {
    method: 'DELETE',
  });
  return readJson<WholeSceneCandidateImageDeleteResponse>(response);
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
