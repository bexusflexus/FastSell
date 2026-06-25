import { apiFetch, readJson } from './client';
import type {
  AIProviderTestResult,
  CreateAIProviderInput,
  DeleteAIProviderResponse,
  GetAIProviderResponse,
  GetAISettingsResponse,
  ListAIProvidersResponse,
  UpdateAIProviderInput,
  UpdateAISettingsInput,
} from '../types/ai';

export async function listAIProviders(): Promise<ListAIProvidersResponse> {
  const response = await apiFetch('/api/admin/ai/providers');
  return readJson<ListAIProvidersResponse>(response);
}

export async function createAIProvider(input: CreateAIProviderInput): Promise<GetAIProviderResponse> {
  const response = await apiFetch('/api/admin/ai/providers', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetAIProviderResponse>(response);
}

export async function getAIProvider(providerId: string): Promise<GetAIProviderResponse> {
  const response = await apiFetch(`/api/admin/ai/providers/${encodeURIComponent(providerId)}`);
  return readJson<GetAIProviderResponse>(response);
}

export async function updateAIProvider(providerId: string, input: UpdateAIProviderInput): Promise<GetAIProviderResponse> {
  const response = await apiFetch(`/api/admin/ai/providers/${encodeURIComponent(providerId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetAIProviderResponse>(response);
}

export async function deleteAIProvider(providerId: string): Promise<DeleteAIProviderResponse> {
  const response = await apiFetch(`/api/admin/ai/providers/${encodeURIComponent(providerId)}`, {
    method: 'DELETE',
  });
  return readJson<DeleteAIProviderResponse>(response);
}

export async function setActiveAIProvider(providerId: string): Promise<GetAIProviderResponse> {
  const response = await apiFetch(`/api/admin/ai/providers/${encodeURIComponent(providerId)}/set-active`, {
    method: 'POST',
  });
  return readJson<GetAIProviderResponse>(response);
}

export async function testAIProvider(providerId: string): Promise<AIProviderTestResult> {
  const response = await apiFetch(`/api/admin/ai/providers/${encodeURIComponent(providerId)}/test`, {
    method: 'POST',
  });
  return readJson<AIProviderTestResult>(response);
}

export async function getAISettings(): Promise<GetAISettingsResponse> {
  const response = await apiFetch('/api/admin/ai/settings');
  return readJson<GetAISettingsResponse>(response);
}

export async function updateAISettings(input: UpdateAISettingsInput): Promise<GetAISettingsResponse> {
  const response = await apiFetch('/api/admin/ai/settings', {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetAISettingsResponse>(response);
}
