import { apiFetch, readJson } from './client';
import type { AdminSystemHealthResponse, SystemVersionResponse } from '../types/system';

export async function getAdminSystemHealth(): Promise<AdminSystemHealthResponse> {
  const response = await apiFetch('/api/admin/system/health');
  return readJson<AdminSystemHealthResponse>(response);
}

export async function getSystemVersion(): Promise<SystemVersionResponse> {
  const response = await apiFetch('/api/system/version');
  return readJson<SystemVersionResponse>(response);
}
