import { apiFetch, readJson } from './client';
import type { AdminMetricsResponse } from '../types/metrics';

export async function getAdminMetrics(): Promise<AdminMetricsResponse> {
  const response = await apiFetch('/api/admin/metrics');
  return readJson<AdminMetricsResponse>(response);
}
