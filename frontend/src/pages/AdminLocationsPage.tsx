import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '../api/client';
import {
  archiveLocation,
  createLocation,
  deleteLocation,
  getLocationDeletePreview,
  listLocations,
  unarchiveLocation,
  updateLocation,
} from '../api/locations';
import { Panel } from '../components/Panel';
import type { CreateLocationInput, LocationDeletePreview, LocationOption, UpdateLocationInput } from '../types/locations';

interface LocationFormState {
  name: string;
  description: string;
}

const emptyForm: LocationFormState = { name: '', description: '' };

export function AdminLocationsPage() {
  const [locations, setLocations] = useState<LocationOption[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [createForm, setCreateForm] = useState<LocationFormState>(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState<LocationFormState>(emptyForm);
  const [search, setSearch] = useState('');
  const [showArchived, setShowArchived] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<LocationOption | null>(null);
  const [deletePreview, setDeletePreview] = useState<LocationDeletePreview | null>(null);
  const [deletePreviewError, setDeletePreviewError] = useState<string | null>(null);

  const loadAllLocations = async () => {
    setIsLoading(true);
    setLoadError(null);
    try {
      setLocations(await listLocations({ includeArchived: true }));
    } catch (error) {
      console.error('Failed to load locations', error);
      setLoadError(errorMessage(error, 'Failed to load locations.'));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadAllLocations();
  }, []);

  const visibleLocations = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return locations.filter((location) => {
      if (!showArchived && location.archived) {
        return false;
      }
      if (!normalizedSearch) {
        return true;
      }
      return [location.name, location.description ?? ''].some((value) => value.toLowerCase().includes(normalizedSearch));
    });
  }, [locations, search, showArchived]);

  const handleCreate = async () => {
    const input = formToCreateInput(createForm);
    if (!input.name) {
      setActionError('Location name is required.');
      return;
    }

    try {
      setBusyAction('create');
      setActionError(null);
      const created = await createLocation(input);
      setLocations((current) => [created, ...current]);
      setCreateForm(emptyForm);
      setMessage('Location created.');
    } catch (error) {
      console.error('Failed to create location', error);
      setActionError(errorMessage(error, 'Failed to create location.'));
    } finally {
      setBusyAction(null);
    }
  };

  const beginEdit = (location: LocationOption) => {
    setEditingId(location.id);
    setEditForm({ name: location.name, description: location.description ?? '' });
  };

  const handleSave = async (locationId: string) => {
    const patch = formToUpdateInput(editForm);
    if (patch.name !== undefined && !patch.name) {
      setActionError('Location name is required.');
      return;
    }

    try {
      setBusyAction(`save:${locationId}`);
      setActionError(null);
      const updated = await updateLocation(locationId, patch);
      setLocations((current) => current.map((location) => (location.id === updated.id ? updated : location)));
      setEditingId(null);
      setMessage('Location updated.');
    } catch (error) {
      console.error('Failed to update location', error);
      setActionError(errorMessage(error, 'Failed to update location.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleArchiveToggle = async (location: LocationOption) => {
    try {
      setBusyAction(`archive:${location.id}`);
      setActionError(null);
      const updated = location.archived ? await unarchiveLocation(location.id) : await archiveLocation(location.id);
      setLocations((current) => current.map((entry) => (entry.id === updated.id ? updated : entry)));
      setMessage(location.archived ? 'Location unarchived.' : 'Location archived.');
    } catch (error) {
      console.error('Failed to toggle location archive state', error);
      setActionError(errorMessage(error, 'Failed to update location archive state.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDelete = async (location: LocationOption) => {
    setDeleteTarget(location);
    setDeletePreview(null);
    setDeletePreviewError(null);
    setActionError(null);
    setMessage(null);
    setBusyAction(`delete-preview:${location.id}`);

    try {
      const preview = await getLocationDeletePreview(location.id);
      setDeletePreview(preview);
    } catch (error) {
      console.error('Failed to load location delete preview', error);
      const message = errorMessage(error, 'Failed to load location delete preview.');
      setDeletePreviewError(message);
      setActionError(message);
    } finally {
      setBusyAction(null);
    }
  };

  const handleCancelDelete = () => {
    setDeleteTarget(null);
    setDeletePreview(null);
    setDeletePreviewError(null);
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) {
      return;
    }

    try {
      setActionError(null);
      setDeletePreviewError(null);
      setBusyAction(`delete:${deleteTarget.id}`);
      const response = await deleteLocation(deleteTarget.id);
      setLocations((current) => current.filter((location) => location.id !== deleteTarget.id));
      if (editingId === deleteTarget.id) {
        setEditingId(null);
      }
      setMessage(`Deleted location "${response.name}".`);
      setDeleteTarget(null);
      setDeletePreview(null);
      setDeletePreviewError(null);
    } catch (error) {
      console.error('Failed to delete location', error);
      const message = errorMessage(error, 'Failed to delete location.');
      setDeletePreviewError(message);
      setActionError(message);
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel title="Admin Locations" eyebrow="Structured container locations">
        <div className="grid gap-4 lg:grid-cols-[1fr_auto] lg:items-end">
          <div className="grid gap-3 sm:grid-cols-2">
            <AdminInput label="Name" value={createForm.name} onChange={(name) => setCreateForm((current) => ({ ...current, name }))} />
            <AdminInput
              label="Description"
              value={createForm.description}
              onChange={(description) => setCreateForm((current) => ({ ...current, description }))}
            />
          </div>
          <button
            type="button"
            disabled={busyAction === 'create'}
            onClick={() => void handleCreate()}
            className="rounded-md border border-copper-500/35 px-4 py-3 text-sm font-semibold text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {busyAction === 'create' ? 'Creating...' : 'Create location'}
          </button>
        </div>
        {message ? <p className="mt-3 text-sm text-amberline-100">{message}</p> : null}
        {actionError ? <p className="mt-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{actionError}</p> : null}
      </Panel>

      <Panel title="Locations" eyebrow="Active and archived">
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search location name or description"
            className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-sm text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 md:max-w-md"
          />
          <label className="flex items-center gap-2 text-sm text-stone-300">
            <input type="checkbox" checked={showArchived} onChange={(event) => setShowArchived(event.target.checked)} />
            Show archived
          </label>
        </div>

        {loadError ? <p className="mt-4 text-sm text-red-100">{loadError}</p> : null}
        {isLoading ? <p className="mt-4 text-sm text-stone-400">Loading locations...</p> : null}

        <div className="mt-4 grid gap-3">
          {visibleLocations.map((location) => {
            const isEditing = editingId === location.id;
            const isSaving = busyAction === `save:${location.id}`;
            const isArchiving = busyAction === `archive:${location.id}`;
            const isDeletePreviewing = busyAction === `delete-preview:${location.id}`;
            const isDeleting = busyAction === `delete:${location.id}`;
            const isBusy = isSaving || isArchiving || isDeletePreviewing || isDeleting;

            return (
              <article
                key={location.id}
                className={`rounded-md border p-4 ${location.archived ? 'border-rack-steel/20 bg-rack-soot/45 opacity-75' : 'border-rack-steel/30 bg-rack-soot/75'}`}
              >
                {isEditing ? (
                  <div className="grid gap-3 sm:grid-cols-2">
                    <AdminInput label="Name" value={editForm.name} onChange={(name) => setEditForm((current) => ({ ...current, name }))} />
                    <AdminInput
                      label="Description"
                      value={editForm.description}
                      onChange={(description) => setEditForm((current) => ({ ...current, description }))}
                    />
                  </div>
                ) : (
                  <div className="grid gap-2">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
                      <h2 className="text-lg font-semibold text-stone-100">{location.name}</h2>
                      <span className={location.archived ? 'text-sm font-semibold text-red-100' : 'text-sm font-semibold text-green-100'}>
                        {location.archived ? 'Archived' : 'Active'}
                      </span>
                    </div>
                    {location.description ? <p className="text-sm text-stone-300">{location.description}</p> : <p className="text-sm text-stone-500">No description.</p>}
                    <p className="text-xs text-stone-500">Created: {formatDate(location.createdDatetime)}</p>
                    <p className="text-xs text-stone-500">Updated: {formatDate(location.updatedDatetime)}</p>
                    {location.archivedDatetime ? <p className="text-xs text-stone-500">Archived: {formatDate(location.archivedDatetime)}</p> : null}
                  </div>
                )}

                <div className="mt-4 flex flex-wrap gap-2">
                  {isEditing ? (
                    <>
                      <button
                        type="button"
                        disabled={isSaving}
                        onClick={() => void handleSave(location.id)}
                        className="rounded-md border border-signal-green/35 px-3 py-2 text-sm text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isSaving ? 'Saving...' : 'Save'}
                      </button>
                      <button
                        type="button"
                        onClick={() => setEditingId(null)}
                        className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-300 hover:bg-rack-steel/12"
                      >
                        Cancel
                      </button>
                    </>
                  ) : (
                    <>
                      <button
                        type="button"
                        onClick={() => beginEdit(location)}
                        className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12"
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleArchiveToggle(location)}
                        className="rounded-md border border-copper-500/35 px-3 py-2 text-sm text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isArchiving ? 'Updating...' : location.archived ? 'Unarchive' : 'Archive'}
                      </button>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleRequestDelete(location)}
                        className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isDeletePreviewing ? 'Checking...' : isDeleting ? 'Deleting...' : 'Delete if unused'}
                      </button>
                    </>
                  )}
                </div>
              </article>
            );
          })}
        </div>
      </Panel>

      {deleteTarget ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6">
          <div className="max-h-[90vh] w-full max-w-xl overflow-y-auto">
            <DeleteLocationPanel
              location={deleteTarget}
              preview={deletePreview}
              error={deletePreviewError}
              isLoading={busyAction === `delete-preview:${deleteTarget.id}`}
              isDeleting={busyAction === `delete:${deleteTarget.id}`}
              onCancel={handleCancelDelete}
              onConfirm={() => void handleConfirmDelete()}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function DeleteLocationPanel({
  location,
  preview,
  error,
  isLoading,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  location: LocationOption;
  preview: LocationDeletePreview | null;
  error: string | null;
  isLoading: boolean;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const canDelete = preview?.canDelete ?? false;

  return (
    <Panel title="Delete Location" eyebrow="Admin maintenance">
      <div className="grid gap-4">
        <div className="grid gap-2">
          <p className="text-sm text-stone-200">
            Review delete readiness for <span className="font-semibold text-stone-100">{location.name}</span>.
          </p>
          {location.description ? <p className="text-sm text-stone-400">{location.description}</p> : null}
        </div>

        {isLoading ? <p className="rounded-md border border-rack-steel/30 bg-rack-soot/70 px-3 py-2 text-sm text-stone-300">Loading delete preview...</p> : null}
        {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}

        {preview ? (
          <div className="grid gap-2 rounded-md border border-rack-steel/24 bg-rack-soot/65 p-4">
            <p className="text-sm text-stone-200">Containers using this location: <span className="font-semibold text-stone-100">{preview.usageCount}</span></p>
            <p className={`text-sm ${preview.canDelete ? 'text-green-100' : 'text-red-100'}`}>
              {preview.canDelete ? 'This location can be deleted.' : preview.blockingReason ?? 'This location cannot be deleted.'}
            </p>
          </div>
        ) : null}

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            onClick={onCancel}
            className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-300 hover:bg-rack-steel/12"
          >
            Cancel
          </button>
          <button
            type="button"
            disabled={isLoading || isDeleting || !canDelete}
            onClick={onConfirm}
            className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isDeleting ? 'Deleting...' : 'Delete location'}
          </button>
        </div>
      </div>
    </Panel>
  );
}

function AdminInput({ label, value, onChange }: { label: string; value: string; onChange: (value: string) => void }) {
  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
      />
    </label>
  );
}

function formToCreateInput(form: LocationFormState): CreateLocationInput {
  return {
    name: form.name.trim(),
    description: form.description.trim() || undefined,
  };
}

function formToUpdateInput(form: LocationFormState): UpdateLocationInput {
  return {
    name: form.name.trim(),
    description: form.description.trim() || null,
  };
}

function formatDate(value?: string | null) {
  return value ? new Date(value).toLocaleString() : 'Never';
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.message;
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return fallback;
}
