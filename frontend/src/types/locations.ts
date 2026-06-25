export interface LocationOption {
  id: string;
  name: string;
  description: string | null;
  archived: boolean;
  archivedDatetime: string | null;
  createdDatetime: string;
  updatedDatetime: string | null;
}

export interface CreateLocationInput {
  name: string;
  description?: string;
}

export interface UpdateLocationInput {
  name?: string;
  description?: string | null;
  archived?: boolean;
}

export interface LocationDeletePreview {
  id: string;
  name: string;
  canDelete: boolean;
  usageCount: number;
  blockingReason: string | null;
}

export interface DeleteLocationResponse {
  id: string;
  name: string;
  deleted: boolean;
  usageCount: number;
}
