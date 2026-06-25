export const API_BASE_URL = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/+$/, '');

export class ApiError extends Error {
  status: number | null;
  payload: unknown;

  constructor(message: string, status: number | null = null, payload: unknown = null) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
    this.payload = payload;
  }
}

export async function apiFetch(path: string, init?: RequestInit): Promise<Response> {
  let response: Response;

  try {
    response = await fetch(`${API_BASE_URL}${path}`, init);
  } catch (error) {
    throw new ApiError('API is unreachable. Check the API URL and network connection.', null, error);
  }

  if (!response.ok) {
    const payload = await readResponsePayload(response);
    throw new ApiError(
      errorMessageFromPayload(payload) ?? defaultErrorMessage(response.status),
      response.status,
      payload,
    );
  }

  return response;
}

export async function readJson<T>(response: Response): Promise<T> {
  return (await response.json()) as T;
}

async function readResponsePayload(response: Response): Promise<unknown> {
  const contentType = response.headers.get('Content-Type') ?? '';
  if (contentType.includes('application/json')) {
    try {
      return await response.json();
    } catch {
      return null;
    }
  }

  try {
    return await response.text();
  } catch {
    return null;
  }
}

function errorMessageFromPayload(payload: unknown): string | null {
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

function defaultErrorMessage(status: number): string {
  if (status === 413) {
    return 'Upload too large. Try fewer photos or smaller images.';
  }

  return `API request failed with HTTP ${status}.`;
}
