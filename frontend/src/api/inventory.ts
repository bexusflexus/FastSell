import { apiFetch, readJson } from './client';
import type { InventoryContainerSummary } from '../types/inventory';

export interface ListInventoryContainerSummariesOptions {
  includeArchived?: boolean;
  archived?: boolean;
  dispositionCode?: string;
  aiEnriched?: boolean;
  missingApproxValue?: boolean;
}

export async function getInventoryContainerSummaries(
  options: ListInventoryContainerSummariesOptions = {},
): Promise<InventoryContainerSummary[]> {
  const params = new URLSearchParams();

  if (typeof options.archived === 'boolean') {
    params.set('archived', String(options.archived));
  } else if (options.includeArchived) {
    params.set('include_archived', 'true');
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

  const query = params.toString();
  const response = await apiFetch(`/api/inventory/containers${query ? `?${query}` : ''}`);
  return readJson<InventoryContainerSummary[]>(response);
}
