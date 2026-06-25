import { useEffect, useMemo, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { listContainerTypes } from '../api/containerTypes';
import { createContainer, deleteContainer, getContainerDeletePreview, getContainerSummary, listContainers, updateContainer } from '../api/containers';
import { listLocations } from '../api/locations';
import { ContainerSummaryPanel } from '../components/ContainerSummaryPanel';
import { Panel } from '../components/Panel';
import type { ContainerTypeOption } from '../types/containerTypes';
import type { LocationOption } from '../types/locations';
import type { ContainerDeletePreview, ContainerOption, ContainerSummary, CreateContainerInput, UpdateContainerInput } from '../types/upload';

interface ContainerFormState {
  name: string;
  type: string;
  containerTypeId: string;
  locationId: string;
  locationDescription: string;
  notes: string;
}

const emptyForm: ContainerFormState = { name: '', type: '', containerTypeId: '', locationId: '', locationDescription: '', notes: '' };

export function AdminContainersPage() {
  const [containers, setContainers] = useState<ContainerOption[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [createForm, setCreateForm] = useState<ContainerFormState>(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState<ContainerFormState>(emptyForm);
  const [search, setSearch] = useState('');
  const [showArchived, setShowArchived] = useState(false);
  const [containerTypes, setContainerTypes] = useState<ContainerTypeOption[]>([]);
  const [isLoadingContainerTypes, setIsLoadingContainerTypes] = useState(true);
  const [containerTypesError, setContainerTypesError] = useState<string | null>(null);
  const [locations, setLocations] = useState<LocationOption[]>([]);
  const [isLoadingLocations, setIsLoadingLocations] = useState(true);
  const [locationsError, setLocationsError] = useState<string | null>(null);
  const [selectedSummaryId, setSelectedSummaryId] = useState<string | null>(null);
  const [summary, setSummary] = useState<ContainerSummary | null>(null);
  const [summaryError, setSummaryError] = useState<string | null>(null);
  const [isSummaryLoading, setIsSummaryLoading] = useState(false);
  const summaryRequestIdRef = useRef(0);
  const [deleteTarget, setDeleteTarget] = useState<ContainerOption | null>(null);
  const [deletePreview, setDeletePreview] = useState<ContainerDeletePreview | null>(null);
  const [deletePreviewError, setDeletePreviewError] = useState<string | null>(null);

  const loadContainers = async () => {
    setIsLoading(true);
    setLoadError(null);
    try {
      setContainers(await listContainers({ includeArchived: true }));
    } catch (error) {
      console.error('Failed to load containers', error);
      setLoadError(errorMessage(error, 'Failed to load containers.'));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadContainers();
  }, []);

  useEffect(() => {
    let isMounted = true;

    const loadActiveLocations = async () => {
      setIsLoadingLocations(true);
      setLocationsError(null);
      try {
        const response = await listLocations();
        if (isMounted) {
          setLocations(response.filter((location) => !location.archived));
        }
      } catch (error) {
        console.error('Failed to load locations', error);
        if (isMounted) {
          setLocationsError(errorMessage(error, 'Failed to load locations.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingLocations(false);
        }
      }
    };

    void loadActiveLocations();
    return () => {
      isMounted = false;
    };
  }, []);

  useEffect(() => {
    let isMounted = true;

    const loadActiveContainerTypes = async () => {
      setIsLoadingContainerTypes(true);
      setContainerTypesError(null);
      try {
        const response = await listContainerTypes();
        if (isMounted) {
          setContainerTypes(response.filter((containerType) => !containerType.archived));
        }
      } catch (error) {
        console.error('Failed to load container types', error);
        if (isMounted) {
          setContainerTypesError(errorMessage(error, 'Failed to load container types.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingContainerTypes(false);
        }
      }
    };

    void loadActiveContainerTypes();
    return () => {
      isMounted = false;
    };
  }, []);

  const visibleContainers = useMemo(() => {
    const normalizedSearch = search.trim().toLowerCase();
    return containers.filter((container) => {
      if (!showArchived && container.archived) {
        return false;
      }
      if (!normalizedSearch) {
        return true;
      }
      return [container.name, container.containerTypeName ?? '', container.type ?? '', container.locationName ?? '', container.locationDescription ?? '', container.notes ?? ''].some((value) =>
        value.toLowerCase().includes(normalizedSearch),
      );
    });
  }, [containers, search, showArchived]);

  const handleCreate = async () => {
    const input = formToCreateInput(createForm);
    if (!input.name) {
      setMessage('Container name is required.');
      return;
    }

    try {
      setActionError(null);
      setBusyAction('create');
      await createContainer(input);
      setCreateForm(emptyForm);
      setMessage('Container created.');
      await loadContainers();
    } catch (error) {
      console.error('Failed to create container', error);
      setActionError(errorMessage(error, 'Failed to create container.'));
    } finally {
      setBusyAction(null);
    }
  };

  const beginEdit = (container: ContainerOption) => {
    setEditingId(container.id);
    setEditForm({
      name: container.name,
      type: container.type ?? '',
      containerTypeId: container.containerTypeId ?? '',
      locationId: container.locationId ?? '',
      locationDescription: container.locationDescription ?? '',
      notes: container.notes ?? '',
    });
  };

  const handleSave = async (containerId: string) => {
    const currentContainer = containers.find((container) => container.id === containerId) ?? null;
    const patch = formToUpdateInput(editForm, currentContainer);
    if (patch.name !== undefined && !patch.name) {
      setMessage('Container name is required.');
      return;
    }

    try {
      setActionError(null);
      setBusyAction(`save:${containerId}`);
      const updated = await updateContainer(containerId, patch);
      setContainers((current) => current.map((container) => (container.id === updated.id ? updated : container)));
      setEditingId(null);
      setMessage('Container updated.');
    } catch (error) {
      console.error('Failed to save container', error);
      setActionError(errorMessage(error, 'Failed to save container.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleArchiveToggle = async (container: ContainerOption) => {
    try {
      setActionError(null);
      setBusyAction(`archive:${container.id}`);
      const updated = await updateContainer(container.id, { archived: !container.archived });
      setContainers((current) => current.map((item) => (item.id === updated.id ? updated : item)));
      setMessage(container.archived ? 'Container unarchived.' : 'Container archived.');
    } catch (error) {
      console.error(container.archived ? 'Failed to unarchive container' : 'Failed to archive container', error);
      setActionError(errorMessage(error, container.archived ? 'Failed to unarchive container.' : 'Failed to archive container.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleShowSummary = async (container: ContainerOption) => {
    const requestId = summaryRequestIdRef.current + 1;
    summaryRequestIdRef.current = requestId;

    setSelectedSummaryId(container.id);
    setSummary(null);
    setSummaryError(null);
    setIsSummaryLoading(true);
    setActionError(null);
    setMessage('Loading container summary...');
    try {
      const nextSummary = await getContainerSummary(container.id);
      if (summaryRequestIdRef.current !== requestId) {
        return;
      }
      setSummary(nextSummary);
      setMessage(`Summary loaded for ${nextSummary.container.name}.`);
    } catch (error) {
      if (summaryRequestIdRef.current !== requestId) {
        return;
      }
      console.error('Failed to load container summary', error);
      setSummary(null);
      const message = errorMessage(error, 'Failed to load container summary.');
      setSummaryError(message);
      setActionError(message);
    } finally {
      if (summaryRequestIdRef.current === requestId) {
        setIsSummaryLoading(false);
      }
    }
  };

  const handleCloseSummary = () => {
    summaryRequestIdRef.current += 1;
    setSelectedSummaryId(null);
    setSummary(null);
    setSummaryError(null);
    setIsSummaryLoading(false);
  };

  const handleRequestDelete = async (container: ContainerOption) => {
    setDeleteTarget(container);
    setDeletePreview(null);
    setDeletePreviewError(null);
    setActionError(null);
    setMessage(null);
    setBusyAction(`delete-preview:${container.id}`);

    try {
      const preview = await getContainerDeletePreview(container.id);
      setDeletePreview(preview);
    } catch (error) {
      console.error('Failed to load container delete preview', error);
      const message = errorMessage(error, 'Failed to load container delete preview.');
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
      const response = await deleteContainer(deleteTarget.id);
      
      setContainers((current) => current.filter((container) => container.id !== deleteTarget.id));
      if (selectedSummaryId === deleteTarget.id) {
        setSelectedSummaryId(null);
        setSummary(null);
        setSummaryError(null);
      }
      if (editingId === deleteTarget.id) {
        setEditingId(null);
      }
      
      setMessage(
        `Container deleted. Removed ${response.deleted.upload_sessions} upload sessions, ${response.deleted.upload_groups} groups, ${response.deleted.image_assets} images, and ${response.deleted.items} items.`,
      );
      setDeleteTarget(null);
      setDeletePreview(null);
      setDeletePreviewError(null);
    } catch (error) {
      console.error('Failed to delete container', error);
      const message = errorMessage(error, 'Failed to delete container.');
      setDeletePreviewError(message);
      setActionError(message);
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel title="Admin Containers" eyebrow="Container management">
        <div className="grid gap-3">
          <div className="grid gap-3 md:grid-cols-3">
            <AdminInput label="Name" value={createForm.name} onChange={(name) => setCreateForm((current) => ({ ...current, name }))} />
            <AdminSelect
              label="Structured container type"
              value={createForm.containerTypeId}
              onChange={(containerTypeId) => setCreateForm((current) => ({ ...current, containerTypeId }))}
              options={containerTypes}
              isLoading={isLoadingContainerTypes}
              emptyLabel="No structured container type"
              loadingLabel="Loading container types..."
            />
            <AdminSelect
              label="Structured location"
              value={createForm.locationId}
              onChange={(locationId) => setCreateForm((current) => ({ ...current, locationId }))}
              options={locations}
              isLoading={isLoadingLocations}
              emptyLabel="No structured location"
              loadingLabel="Loading locations..."
            />
          </div>
          <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto] lg:items-end">
            <AdminInput
              label="Notes"
              value={createForm.notes}
              onChange={(notes) => setCreateForm((current) => ({ ...current, notes }))}
              multiline
            />
            <button
              type="button"
              disabled={busyAction === 'create'}
              onClick={() => void handleCreate()}
              className="rounded-md border border-copper-500/35 px-4 py-3 text-sm font-semibold text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busyAction === 'create' ? 'Creating...' : 'Create container'}
            </button>
          </div>
        </div>
        {containerTypesError ? <p className="mt-3 text-sm text-red-100">{containerTypesError}</p> : null}
        {locationsError ? <p className="mt-3 text-sm text-red-100">{locationsError}</p> : null}
        {message ? <p className="mt-3 text-sm text-amberline-100">{message}</p> : null}
        {actionError ? <p className="mt-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{actionError}</p> : null}
      </Panel>

      <Panel title="Containers" eyebrow="Active and archived">
        <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
          <input
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            placeholder="Search name, type, location, notes"
            className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-sm text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 md:max-w-md"
          />
          <label className="flex items-center gap-2 text-sm text-stone-300">
            <input type="checkbox" checked={showArchived} onChange={(event) => setShowArchived(event.target.checked)} />
            Show archived
          </label>
        </div>

        {loadError ? <p className="mt-4 text-sm text-red-100">{loadError}</p> : null}
        {isLoading ? <p className="mt-4 text-sm text-stone-400">Loading containers...</p> : null}

        <div className="mt-4 grid gap-3">
          {visibleContainers.map((container) => (
            <ContainerRow
              key={container.id}
              container={container}
              editingId={editingId}
              editForm={editForm}
              containerTypes={containerTypes}
              isLoadingContainerTypes={isLoadingContainerTypes}
              locations={locations}
              isLoadingLocations={isLoadingLocations}
              onEditFormChange={setEditForm}
              onBeginEdit={beginEdit}
              onCancelEdit={() => setEditingId(null)}
              onSave={(id) => void handleSave(id)}
              onArchiveToggle={(item) => void handleArchiveToggle(item)}
              onShowSummary={(container) => void handleShowSummary(container)}
              onRequestDelete={(container) => void handleRequestDelete(container)}
              busyAction={busyAction}
            />
          ))}
        </div>
      </Panel>

      {deleteTarget ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6">
          <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto">
            <DeleteConfirmationPanel
              container={deleteTarget}
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

      {selectedSummaryId ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6">
          <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto">
            <SummaryModalPanel
              summary={summary}
              isLoading={isSummaryLoading}
              error={summaryError}
              containerName={summary?.container.name ?? visibleContainers.find((container) => container.id === selectedSummaryId)?.name ?? 'Container'}
              onClose={handleCloseSummary}
            />
          </div>
        </div>
      ) : null}
    </div>
  );
}

function ContainerRow({
  container,
  editingId,
  editForm,
  containerTypes,
  isLoadingContainerTypes,
  locations,
  isLoadingLocations,
  onEditFormChange,
  onBeginEdit,
  onCancelEdit,
  onSave,
  onArchiveToggle,
  onShowSummary,
  onRequestDelete,
  busyAction,
}: {
  container: ContainerOption;
  editingId: string | null;
  editForm: ContainerFormState;
  containerTypes: ContainerTypeOption[];
  isLoadingContainerTypes: boolean;
  locations: LocationOption[];
  isLoadingLocations: boolean;
  onEditFormChange: (form: ContainerFormState) => void;
  onBeginEdit: (container: ContainerOption) => void;
  onCancelEdit: () => void;
  onSave: (id: string) => void;
  onArchiveToggle: (container: ContainerOption) => void;
  onShowSummary: (container: ContainerOption) => void;
  onRequestDelete: (container: ContainerOption) => void;
  busyAction: string | null;
}) {
  const isEditing = editingId === container.id;
  const isSaving = busyAction === `save:${container.id}`;
  const isArchiving = busyAction === `archive:${container.id}`;
  const isDeletePreviewing = busyAction === `delete-preview:${container.id}`;
  const isDeleting = busyAction === `delete:${container.id}`;
  const isBusy = isSaving || isArchiving || isDeletePreviewing || isDeleting;
  const scopedLinks = [
    { label: 'Upload more photos', href: `/upload?container_id=${encodeURIComponent(container.id)}` },
    { label: 'View inventory', href: `/inventory?container_id=${encodeURIComponent(container.id)}` },
    { label: 'Review uploads', href: `/review?container_id=${encodeURIComponent(container.id)}` },
  ];

  return (
    <article className={`rounded-md border p-4 ${container.archived ? 'border-rack-steel/20 bg-rack-soot/45 opacity-75' : 'border-rack-steel/30 bg-rack-soot/75'}`}>
      {isEditing ? (
        <div className="grid gap-3">
          <div className="grid gap-3 md:grid-cols-3">
            <AdminInput label="Name" value={editForm.name} onChange={(name) => onEditFormChange({ ...editForm, name })} />
            <AdminSelect
              label="Structured container type"
              value={editForm.containerTypeId}
              onChange={(containerTypeId) => onEditFormChange({ ...editForm, containerTypeId })}
              options={containerTypes}
              isLoading={isLoadingContainerTypes}
              emptyLabel="No structured container type"
              loadingLabel="Loading container types..."
              missingValueLabel={container.containerTypeName ?? container.type ?? 'Current container type'}
            />
            <AdminSelect
              label="Structured location"
              value={editForm.locationId}
              onChange={(locationId) => onEditFormChange({ ...editForm, locationId })}
              options={locations}
              isLoading={isLoadingLocations}
              emptyLabel="No structured location"
              loadingLabel="Loading locations..."
              missingValueLabel={container.locationName ?? container.locationDescription ?? 'Current location'}
            />
          </div>
          <AdminInput label="Notes" value={editForm.notes} onChange={(notes) => onEditFormChange({ ...editForm, notes })} multiline />
        </div>
      ) : (
        <div className="grid gap-2">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-lg font-semibold text-stone-100">{container.name}</h2>
            <span className={container.archived ? 'text-sm font-semibold text-red-100' : 'text-sm font-semibold text-green-100'}>
              {container.archived ? 'Archived' : 'Active'}
            </span>
          </div>
          <p className="text-sm text-stone-300">Type: {container.containerTypeName || container.type || 'None'}</p>
          {container.containerTypeName && container.type ? <p className="text-xs text-stone-500">Legacy type: {container.type}</p> : null}
          <p className="text-sm text-stone-300">Location: {container.locationName || container.locationDescription || 'None'}</p>
          {container.locationName && container.locationDescription ? <p className="text-xs text-stone-500">Legacy location: {container.locationDescription}</p> : null}
          {container.notes ? <p className="text-sm text-stone-300">Notes: {container.notes}</p> : null}
          <p className="text-xs text-stone-500">Created: {formatDate(container.createdDatetime)}</p>
          <p className="text-xs text-stone-500">Updated: {formatDate(container.updatedDatetime)}</p>
          {container.archivedDatetime ? <p className="text-xs text-stone-500">Archived: {formatDate(container.archivedDatetime)}</p> : null}
        </div>
      )}

      <div className="mt-4 flex flex-wrap gap-2">
        {isEditing ? (
          <>
            <button
              type="button"
              disabled={isSaving}
              onClick={() => onSave(container.id)}
              className="rounded-md border border-signal-green/35 px-3 py-2 text-sm text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isSaving ? 'Saving...' : 'Save'}
            </button>
            <button type="button" onClick={onCancelEdit} className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-300 hover:bg-rack-steel/12">
              Cancel
            </button>
          </>
        ) : (
          <>
            <button type="button" onClick={() => onBeginEdit(container)} className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12">
              Edit
            </button>
            <button
              type="button"
              disabled={isBusy}
              onClick={() => onArchiveToggle(container)}
              className="rounded-md border border-copper-500/35 px-3 py-2 text-sm text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isArchiving ? 'Updating...' : container.archived ? 'Unarchive' : 'Archive'}
            </button>
            <button type="button" onClick={() => onShowSummary(container)} className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12">
              Inspect summary
            </button>
            <button
              type="button"
              disabled={isBusy}
              onClick={() => onRequestDelete(container)}
              className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isDeletePreviewing ? 'Preparing delete...' : isDeleting ? 'Deleting...' : 'Delete'}
            </button>
            {scopedLinks.map((link) => (
              <Link key={link.href} to={link.href} className="rounded-md border border-rack-steel/25 px-3 py-2 text-sm text-stone-300 hover:bg-rack-steel/12">
                {link.label}
              </Link>
            ))}
          </>
        )}
      </div>
    </article>
  );
}

function DeleteConfirmationPanel({
  container,
  preview,
  error,
  isLoading,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  container: ContainerOption;
  preview: ContainerDeletePreview | null;
  error: string | null;
  isLoading: boolean;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const counts = preview?.counts;

  return (
    <Panel title="Permanently delete this container?" eyebrow="Destructive action">
      <div className="grid gap-4">
        <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
          <p className="text-base font-semibold text-red-100">Container: {container.name}</p>
          <p className="mt-3 text-sm leading-6 text-stone-200">
            This will permanently delete this container and linked data. This may remove upload sessions, upload groups, inventory items, image metadata, and image files from disk.
          </p>
          <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
        </div>

        {isLoading ? <p className="text-sm text-stone-300">Loading linked data counts...</p> : null}
        {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}

        <dl className="grid gap-2 rounded-md border border-rack-steel/30 bg-rack-soot/75 p-4 text-sm sm:grid-cols-2">
          <DeleteCount label="Upload sessions" value={counts?.upload_sessions} />
          <DeleteCount label="Upload groups" value={counts?.upload_groups} />
          <DeleteCount label="Items" value={counts?.items} />
          <DeleteCount label="Images" value={counts?.image_assets} />
          <DeleteCount label="Files" value={counts?.file_paths} />
        </dl>

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            disabled={!preview || isLoading || isDeleting}
            onClick={onConfirm}
            className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isDeleting ? 'Deleting...' : 'Yes, delete permanently'}
          </button>
          <button
            type="button"
            disabled={isDeleting}
            onClick={onCancel}
            className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
          >
            No, cancel
          </button>
        </div>
      </div>
    </Panel>
  );
}

function DeleteCount({ label, value }: { label: string; value?: number }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-stone-400">{label}</dt>
      <dd className="font-semibold text-stone-100">{value ?? '-'}</dd>
    </div>
  );
}

function SummaryModalPanel({
  summary,
  isLoading,
  error,
  containerName,
  onClose,
}: {
  summary: ContainerSummary | null;
  isLoading: boolean;
  error: string | null;
  containerName: string;
  onClose: () => void;
}) {
  return (
    <Panel title="Container summary" eyebrow={containerName}>
      <div className="grid gap-4">
        <ContainerSummaryPanel summary={summary} isLoading={isLoading} error={error} />
        <div className="flex justify-end">
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-rack-steel/35 px-4 py-2 text-sm text-stone-200 hover:bg-rack-steel/12"
          >
            Close
          </button>
        </div>
      </div>
    </Panel>
  );
}

function AdminInput({
  label,
  value,
  onChange,
  multiline = false,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  multiline?: boolean;
}) {
  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      {multiline ? (
        <textarea
          value={value}
          onChange={(event) => onChange(event.target.value)}
          rows={3}
          className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
        />
      ) : (
        <input
          value={value}
          onChange={(event) => onChange(event.target.value)}
          className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
        />
      )}
    </label>
  );
}

function AdminSelect({
  label,
  value,
  onChange,
  options,
  isLoading,
  emptyLabel,
  loadingLabel,
  missingValueLabel,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ id: string; name: string }>;
  isLoading: boolean;
  emptyLabel: string;
  loadingLabel: string;
  missingValueLabel?: string;
}) {
  const hasSelectedOption = value === '' || options.some((option) => option.id === value);

  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
      >
        <option value="">{isLoading ? loadingLabel : emptyLabel}</option>
        {!hasSelectedOption && value ? <option value={value}>{missingValueLabel ?? 'Current location (unavailable)'}</option> : null}
        {options.map((option) => (
          <option key={option.id} value={option.id}>
            {option.name}
          </option>
        ))}
      </select>
    </label>
  );
}

function formToCreateInput(form: ContainerFormState): CreateContainerInput {
  return {
    name: form.name.trim(),
    type: form.type.trim() || undefined,
    container_type_id: form.containerTypeId || undefined,
    location_id: form.locationId || undefined,
    location_description: form.locationDescription.trim() || undefined,
    notes: form.notes.trim() || undefined,
  };
}

function formToUpdateInput(form: ContainerFormState, currentContainer: ContainerOption | null): UpdateContainerInput {
  const patch: UpdateContainerInput = {
    name: form.name.trim(),
    type: form.type.trim() || null,
    location_description: form.locationDescription.trim() || null,
    notes: form.notes.trim() || null,
  };

  const nextContainerTypeId = form.containerTypeId || null;
  const currentContainerTypeId = currentContainer?.containerTypeId ?? null;
  if (nextContainerTypeId !== currentContainerTypeId) {
    patch.container_type_id = nextContainerTypeId;
  }

  const nextLocationId = form.locationId || null;
  const currentLocationId = currentContainer?.locationId ?? null;
  if (nextLocationId !== currentLocationId) {
    patch.location_id = nextLocationId;
  }

  return patch;
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
