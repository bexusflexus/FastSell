import type { ImageDraft, UploadSessionDraft } from '../types/upload';

const dbName = 'fastsell-upload-drafts';
const dbVersion = 1;
const imageStoreName = 'images';
const metadataStorageKey = 'fastsell.uploadDraft.v1';

interface StoredImageMetadata {
  clientFileId: string;
  originalFilename: string;
  sizeBytes: number;
  mimeType: string;
}

interface StoredGroupDraft {
  clientGroupId: string;
  title: string;
  notes: string;
  autoTitle: boolean;
  images: StoredImageMetadata[];
}

interface StoredUploadDraft {
  selectedContainerId: string | null;
  noContainer: boolean;
  inventoryGroupId: string | null;
  locationId: string | null;
  locationDetail: string;
  sessionNotes: string;
  groups: StoredGroupDraft[];
}

interface StoredImageRecord {
  clientFileId: string;
  file: File;
}

export interface RestoredUploadDraftResult {
  draft: UploadSessionDraft | null;
  missingImageCount: number;
}

export async function saveUploadDraft(draft: UploadSessionDraft): Promise<void> {
  const metadata = toStoredDraft(draft);
  localStorage.setItem(metadataStorageKey, JSON.stringify(metadata));

  const imageIds = new Set(metadata.groups.flatMap((group) => group.images.map((image) => image.clientFileId)));
  const db = await openUploadDraftDb();

  await Promise.all([
    ...draft.groups.flatMap((group) => group.images.map((image) => putImageRecord(db, image))),
    deleteImagesNotIn(db, imageIds),
  ]);
}

export async function restoreUploadDraft(): Promise<RestoredUploadDraftResult> {
  const metadata = readStoredDraft();
  if (!metadata) {
    return { draft: null, missingImageCount: 0 };
  }

  const db = await openUploadDraftDb();
  let missingImageCount = 0;

  const groups = await Promise.all(
    metadata.groups.map(async (group) => {
      const images: ImageDraft[] = [];

      for (const image of group.images) {
        const file = await getImageFile(db, image.clientFileId);
        if (!file) {
          missingImageCount += 1;
          continue;
        }

        images.push({
          clientFileId: image.clientFileId,
          originalFilename: image.originalFilename,
          sizeBytes: image.sizeBytes,
          mimeType: image.mimeType,
          objectUrl: URL.createObjectURL(file),
          file,
        });
      }

      return {
        clientGroupId: group.clientGroupId,
        title: group.title,
        notes: group.notes,
        autoTitle: group.autoTitle,
        images,
      };
    }),
  );

  return {
    draft: {
      selectedContainerId: metadata.selectedContainerId,
      noContainer: metadata.noContainer,
      inventoryGroupId: metadata.inventoryGroupId,
      locationId: metadata.locationId,
      locationDetail: metadata.locationDetail,
      sessionNotes: metadata.sessionNotes,
      groups: groups.length > 0 ? groups : [],
    },
    missingImageCount,
  };
}

export async function clearUploadDraft(): Promise<void> {
  localStorage.removeItem(metadataStorageKey);
  const db = await openUploadDraftDb();
  await clearImages(db);
}

function toStoredDraft(draft: UploadSessionDraft): StoredUploadDraft {
  return {
    selectedContainerId: draft.selectedContainerId,
    noContainer: draft.noContainer,
    inventoryGroupId: draft.inventoryGroupId,
    locationId: draft.locationId,
    locationDetail: draft.locationDetail,
    sessionNotes: draft.sessionNotes,
    groups: draft.groups.map((group) => ({
      clientGroupId: group.clientGroupId,
      title: group.title,
      notes: group.notes,
      autoTitle: group.autoTitle,
      images: group.images.map((image) => ({
        clientFileId: image.clientFileId,
        originalFilename: image.originalFilename,
        sizeBytes: image.sizeBytes,
        mimeType: image.mimeType,
      })),
    })),
  };
}

function readStoredDraft(): StoredUploadDraft | null {
  const value = localStorage.getItem(metadataStorageKey);
  if (!value) {
    return null;
  }

  try {
    const parsed = JSON.parse(value) as unknown;
    if (!isRecord(parsed) || !Array.isArray(parsed.groups)) {
      return null;
    }

    return {
      selectedContainerId: typeof parsed.selectedContainerId === 'string' ? parsed.selectedContainerId : null,
      noContainer: Boolean(parsed.noContainer),
      inventoryGroupId: typeof parsed.inventoryGroupId === 'string' ? parsed.inventoryGroupId : null,
      locationId: typeof parsed.locationId === 'string' ? parsed.locationId : null,
      locationDetail: typeof parsed.locationDetail === 'string' ? parsed.locationDetail : '',
      sessionNotes: typeof parsed.sessionNotes === 'string' ? parsed.sessionNotes : '',
      groups: parsed.groups.filter(isRecord).map((group) => ({
        clientGroupId: typeof group.clientGroupId === 'string' ? group.clientGroupId : crypto.randomUUID(),
        title: typeof group.title === 'string' ? group.title : '',
        notes: typeof group.notes === 'string' ? group.notes : '',
        autoTitle: Boolean(group.autoTitle),
        images: Array.isArray(group.images)
          ? group.images.filter(isRecord).filter(hasClientFileId).map((image) => ({
              clientFileId: image.clientFileId,
              originalFilename: typeof image.originalFilename === 'string' ? image.originalFilename : 'image',
              sizeBytes: typeof image.sizeBytes === 'number' ? image.sizeBytes : 0,
              mimeType: typeof image.mimeType === 'string' ? image.mimeType : 'image/*',
            }))
          : [],
      })),
    };
  } catch {
    return null;
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null;
}

function hasClientFileId(value: Record<string, unknown>): value is Record<string, unknown> & { clientFileId: string } {
  return typeof value.clientFileId === 'string';
}

function openUploadDraftDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const request = indexedDB.open(dbName, dbVersion);

    request.onupgradeneeded = () => {
      const db = request.result;
      if (!db.objectStoreNames.contains(imageStoreName)) {
        db.createObjectStore(imageStoreName, { keyPath: 'clientFileId' });
      }
    };

    request.onerror = () => reject(request.error ?? new Error('Failed to open upload draft database.'));
    request.onsuccess = () => resolve(request.result);
  });
}

function putImageRecord(db: IDBDatabase, image: ImageDraft): Promise<void> {
  return runStoreRequest(db, 'readwrite', (store) =>
    store.put({
      clientFileId: image.clientFileId,
      file: image.file,
    } satisfies StoredImageRecord),
  ).then(() => undefined);
}

function getImageFile(db: IDBDatabase, clientFileId: string): Promise<File | null> {
  return runStoreRequest<StoredImageRecord | undefined>(db, 'readonly', (store) => store.get(clientFileId)).then((record) => {
    if (!record?.file) {
      return null;
    }
    return record.file;
  });
}

function deleteImagesNotIn(db: IDBDatabase, imageIds: Set<string>): Promise<void> {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(imageStoreName, 'readwrite');
    const store = transaction.objectStore(imageStoreName);
    const request = store.getAllKeys();

    request.onsuccess = () => {
      request.result.forEach((key) => {
        if (typeof key === 'string' && !imageIds.has(key)) {
          store.delete(key);
        }
      });
    };
    request.onerror = () => reject(request.error ?? new Error('Failed to inspect upload draft images.'));
    transaction.onerror = () => reject(transaction.error ?? new Error('Failed to prune upload draft images.'));
    transaction.oncomplete = () => resolve();
  });
}

function clearImages(db: IDBDatabase): Promise<void> {
  return runStoreRequest(db, 'readwrite', (store) => store.clear()).then(() => undefined);
}

function runStoreRequest<T = unknown>(
  db: IDBDatabase,
  mode: IDBTransactionMode,
  createRequest: (store: IDBObjectStore) => IDBRequest<T>,
): Promise<T> {
  return new Promise((resolve, reject) => {
    const transaction = db.transaction(imageStoreName, mode);
    const request = createRequest(transaction.objectStore(imageStoreName));

    request.onerror = () => reject(request.error ?? new Error('Upload draft storage request failed.'));
    request.onsuccess = () => resolve(request.result);
    transaction.onerror = () => reject(transaction.error ?? new Error('Upload draft storage transaction failed.'));
  });
}
