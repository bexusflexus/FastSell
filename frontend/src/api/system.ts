import { apiFetch, readJson } from './client';
import type { AdminSystemHealthResponse } from '../types/system';

export async function getAdminSystemHealth(): Promise<AdminSystemHealthResponse> {
  const response = await apiFetch('/api/admin/system/health');
  return readJson<AdminSystemHealthResponse>(response);
}
