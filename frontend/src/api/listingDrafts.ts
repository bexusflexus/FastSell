import { apiFetch, readJson } from './client';
import type {
  CreateListingDraftInput,
  DeleteListingDraftResponse,
  GetListingDraftResponse,
  ListListingDraftsResponse,
  UpdateListingDraftInput,
} from '../types/listingDrafts';

export async function listItemListingDrafts(itemId: string): Promise<ListListingDraftsResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/listing-drafts`);
  return readJson<ListListingDraftsResponse>(response);
}

export async function createOrOpenListingDraft(itemId: string, input: CreateListingDraftInput): Promise<GetListingDraftResponse> {
  const response = await apiFetch(`/api/items/${encodeURIComponent(itemId)}/listing-drafts`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetListingDraftResponse>(response);
}

export async function getListingDraft(draftId: string): Promise<GetListingDraftResponse> {
  const response = await apiFetch(`/api/listing-drafts/${encodeURIComponent(draftId)}`);
  return readJson<GetListingDraftResponse>(response);
}

export async function prepareListingDraftPhotos(draftId: string): Promise<GetListingDraftResponse> {
  const response = await apiFetch(`/api/listing-drafts/${encodeURIComponent(draftId)}/prepare-photos`, {
    method: 'POST',
  });
  return readJson<GetListingDraftResponse>(response);
}

export async function updateListingDraft(draftId: string, input: UpdateListingDraftInput): Promise<GetListingDraftResponse> {
  const response = await apiFetch(`/api/listing-drafts/${encodeURIComponent(draftId)}`, {
    method: 'PATCH',
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(input),
  });
  return readJson<GetListingDraftResponse>(response);
}

export async function deleteListingDraft(draftId: string): Promise<DeleteListingDraftResponse> {
  const response = await apiFetch(`/api/listing-drafts/${encodeURIComponent(draftId)}`, {
    method: 'DELETE',
  });
  return readJson<DeleteListingDraftResponse>(response);
}
