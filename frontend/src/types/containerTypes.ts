export interface ContainerTypeOption {
  id: string;
  name: string;
  description: string | null;
  archived: boolean;
  archivedDatetime: string | null;
  createdDatetime: string;
  updatedDatetime: string | null;
}

export interface CreateContainerTypeInput {
  name: string;
  description?: string;
}

export interface UpdateContainerTypeInput {
  name?: string;
  description?: string | null;
  archived?: boolean;
}

export interface ContainerTypeDeletePreview {
  id: string;
  name: string;
  canDelete: boolean;
  usageCount: number;
  blockingReason: string | null;
}

export interface DeleteContainerTypeResponse {
  id: string;
  name: string;
  deleted: boolean;
  usageCount: number;
}
