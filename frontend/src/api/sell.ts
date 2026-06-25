import { apiFetch, readJson } from './client';
import type {
  CreateSellProviderInput,
  DeleteSellProviderResponse,
  GetSellProviderResponse,
  ListPublicSellProvidersResponse,
  ListSellProvidersResponse,
  UpdateSellProviderInput,
} from '../types/sell';

export async function listEnabledSellProviders(): Promise<ListPublicSellProvidersResponse> {
  const response = await apiFetch('/api/sell/providers');
  return readJson<ListPublicSellProvidersResponse>(response);
}

export async function listSellProviders(): Promise<ListSellProvidersResponse> {
  const response = await apiFetch('/api/admin/sell/providers');
  return readJson<ListSellProvidersResponse>(response);
}

export async function getSellProvider(providerId: string): Promise<GetSellProviderResponse> {
  const response = await apiFetch(`/api/admin/sell/providers/${encodeURIComponent(providerId)}`);
  return readJson<GetSellProviderResponse>(response);
}

export async function createSellProvider(input: CreateSellProviderInput): Promise<GetSellProviderResponse> {
  const response = await apiFetch('/api/admin/sell/providers', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetSellProviderResponse>(response);
}

export async function updateSellProvider(providerId: string, input: UpdateSellProviderInput): Promise<GetSellProviderResponse> {
  const response = await apiFetch(`/api/admin/sell/providers/${encodeURIComponent(providerId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetSellProviderResponse>(response);
}

export async function enableSellProvider(providerId: string): Promise<GetSellProviderResponse> {
  const response = await apiFetch(`/api/admin/sell/providers/${encodeURIComponent(providerId)}/enable`, {
    method: 'POST',
  });
  return readJson<GetSellProviderResponse>(response);
}

export async function disableSellProvider(providerId: string): Promise<GetSellProviderResponse> {
  const response = await apiFetch(`/api/admin/sell/providers/${encodeURIComponent(providerId)}/disable`, {
    method: 'POST',
  });
  return readJson<GetSellProviderResponse>(response);
}

export async function deleteSellProvider(providerId: string): Promise<DeleteSellProviderResponse> {
  const response = await apiFetch(`/api/admin/sell/providers/${encodeURIComponent(providerId)}`, {
    method: 'DELETE',
  });
  return readJson<DeleteSellProviderResponse>(response);
}
