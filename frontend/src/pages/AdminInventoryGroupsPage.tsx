import { useEffect, useState } from 'react';
import { ApiError } from '../api/client';
import { createInventoryGroup, deleteInventoryGroup, listInventoryGroups, updateInventoryGroup } from '../api/inventoryGroups';
import { Panel } from '../components/Panel';
import type { InventoryGroup } from '../types/inventoryGroups';

const emptyForm = {
  code: '',
  name: '',
  description: '',
};

export function AdminInventoryGroupsPage() {
  const [groups, setGroups] = useState<InventoryGroup[]>([]);
  const [showArchived, setShowArchived] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [busyId, setBusyId] = useState<string | null>(null);
  const [form, setForm] = useState(emptyForm);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState(emptyForm);
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);

  useEffect(() => {
    let isMounted = true;

    const loadGroups = async () => {
      setIsLoading(true);
      setError(null);
      try {
        const response = await listInventoryGroups({ includeArchived: showArchived });
        if (isMounted) {
          setGroups(response);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load inventory groups', err);
          setError(errorMessage(err, 'Failed to load inventory groups.'));
        }
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    };

    void loadGroups();

    return () => {
      isMounted = false;
    };
  }, [showArchived]);

  const handleCreate = async () => {
    setError(null);
    setMessage(null);
    try {
      const created = await createInventoryGroup({
        code: form.code.trim(),
        name: form.name.trim(),
        description: form.description.trim(),
      });
      setGroups((current) => [...current, created].sort(sortInventoryGroups));
      setForm(emptyForm);
      setMessage(`Created inventory group "${created.name}".`);
    } catch (err) {
      console.error('Failed to create inventory group', err);
      setError(errorMessage(err, 'Failed to create inventory group.'));
    }
  };

  const startEditing = (group: InventoryGroup) => {
    setEditingId(group.id);
    setEditForm({
      code: group.code,
      name: group.name,
      description: group.description ?? '',
    });
    setError(null);
    setMessage(null);
  };

  const handleSaveEdit = async (group: InventoryGroup) => {
    setBusyId(group.id);
    setError(null);
    setMessage(null);
    try {
      const updated = await updateInventoryGroup(group.id, {
        code: editForm.code.trim(),
        name: editForm.name.trim(),
        description: editForm.description.trim(),
      });
      setGroups((current) => current.map((candidate) => (candidate.id === updated.id ? updated : candidate)).sort(sortInventoryGroups));
      setEditingId(null);
      setMessage(`Updated inventory group "${updated.name}".`);
    } catch (err) {
      console.error('Failed to update inventory group', err);
      setError(errorMessage(err, 'Failed to update inventory group.'));
    } finally {
      setBusyId(null);
    }
  };

  const handleToggleArchive = async (group: InventoryGroup) => {
    setBusyId(group.id);
    setError(null);
    setMessage(null);
    try {
      const updated = await updateInventoryGroup(group.id, { archived: !group.archived });
      setGroups((current) => {
        const next = current.map((candidate) => (candidate.id === updated.id ? updated : candidate));
        return showArchived ? next.sort(sortInventoryGroups) : next.filter((candidate) => !candidate.archived).sort(sortInventoryGroups);
      });
      setMessage(`${updated.archived ? 'Archived' : 'Unarchived'} inventory group "${updated.name}".`);
    } catch (err) {
      console.error('Failed to update inventory group archive state', err);
      setError(errorMessage(err, 'Failed to update inventory group archive state.'));
    } finally {
      setBusyId(null);
    }
  };

  const handleDelete = async (group: InventoryGroup) => {
    if (!window.confirm(`Delete unused inventory group "${group.name}"? This is only allowed when it is not referenced.`)) {
      return;
    }

    setBusyId(group.id);
    setError(null);
    setMessage(null);
    try {
      await deleteInventoryGroup(group.id);
      setGroups((current) => current.filter((candidate) => candidate.id !== group.id));
      setMessage(`Deleted inventory group "${group.name}".`);
    } catch (err) {
      console.error('Failed to delete inventory group', err);
      setError(errorMessage(err, 'Failed to delete inventory group.'));
    } finally {
      setBusyId(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel title="Inventory Groups" eyebrow="Admin maintenance">
        <div className="grid gap-3">
          <p className="text-sm text-stone-300">
            Manage mandatory item classification groups such as Vintage Computers and Clothing.
          </p>
          <label className="flex items-center gap-2 text-sm text-stone-300">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(event) => setShowArchived(event.target.checked)}
              className="h-4 w-4 accent-amberline-500"
            />
            Show archived
          </label>
          {message ? <p className="rounded-md border border-signal-green/35 bg-signal-green/10 px-3 py-2 text-sm text-green-100">{message}</p> : null}
          {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
        </div>
      </Panel>

      <Panel title="Create Inventory Group" eyebrow="New group">
        <div className="grid gap-3 md:grid-cols-[minmax(0,12rem)_minmax(0,16rem)_minmax(0,1fr)_auto] md:items-end">
          <Field label="Code" value={form.code} onChange={(value) => setForm((current) => ({ ...current, code: value }))} placeholder="lowercase_code" />
          <Field label="Name" value={form.name} onChange={(value) => setForm((current) => ({ ...current, name: value }))} placeholder="Display name" />
          <Field label="Description" value={form.description} onChange={(value) => setForm((current) => ({ ...current, description: value }))} placeholder="Optional description" />
          <button
            type="button"
            onClick={() => void handleCreate()}
            className="rounded-md border border-amberline-400/45 bg-amberline-500/15 px-4 py-3 text-sm font-semibold text-amberline-100 hover:bg-amberline-500/25"
          >
            Create
          </button>
        </div>
      </Panel>

      <Panel title="Groups" eyebrow={showArchived ? 'Active and archived' : 'Active'}>
        <div className="grid gap-3">
          {isLoading ? <p className="text-sm text-stone-400">Loading inventory groups...</p> : null}
          {!isLoading && groups.length === 0 ? <p className="text-sm text-stone-300">No inventory groups found.</p> : null}
          {groups.map((group) => {
            const isEditing = editingId === group.id;
            const isBusy = busyId === group.id;

            return (
              <div key={group.id} className="grid gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/65 p-4">
                {isEditing ? (
                  <div className="grid gap-3 md:grid-cols-[minmax(0,12rem)_minmax(0,16rem)_minmax(0,1fr)]">
                    <Field label="Code" value={editForm.code} onChange={(value) => setEditForm((current) => ({ ...current, code: value }))} />
                    <Field label="Name" value={editForm.name} onChange={(value) => setEditForm((current) => ({ ...current, name: value }))} />
                    <Field label="Description" value={editForm.description} onChange={(value) => setEditForm((current) => ({ ...current, description: value }))} />
                  </div>
                ) : (
                  <div className="grid gap-2">
                    <div className="flex flex-wrap items-center gap-2">
                      <h2 className="text-base font-semibold text-stone-100">{group.name}</h2>
                      <span className="rounded-full border border-rack-steel/24 bg-black/20 px-2 py-0.5 text-xs text-stone-300">{group.code}</span>
                      {group.archived ? <span className="rounded-full border border-red-400/35 bg-red-950/25 px-2 py-0.5 text-xs text-red-100">Archived</span> : null}
                    </div>
                    {group.description ? <p className="text-sm text-stone-400">{group.description}</p> : null}
                    <p className="text-xs text-stone-500">ID: {group.id}</p>
                  </div>
                )}

                <div className="flex flex-wrap gap-2">
                  {isEditing ? (
                    <>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleSaveEdit(group)}
                        className="rounded-md border border-signal-green/35 px-3 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isBusy ? 'Saving...' : 'Save'}
                      </button>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => setEditingId(null)}
                        className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Cancel
                      </button>
                    </>
                  ) : (
                    <>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => startEditing(group)}
                        className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Edit
                      </button>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleToggleArchive(group)}
                        className="rounded-md border border-copper-500/35 px-3 py-2 text-sm text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {isBusy ? 'Updating...' : group.archived ? 'Unarchive' : 'Archive'}
                      </button>
                      <button
                        type="button"
                        disabled={isBusy}
                        onClick={() => void handleDelete(group)}
                        className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        Delete if unused
                      </button>
                    </>
                  )}
                </div>
              </div>
            );
          })}
        </div>
      </Panel>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      <input
        value={value}
        onChange={(event) => onChange(event.target.value)}
        placeholder={placeholder}
        className="w-full min-w-0 rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
      />
    </label>
  );
}

function sortInventoryGroups(a: InventoryGroup, b: InventoryGroup) {
  if (a.archived !== b.archived) {
    return a.archived ? 1 : -1;
  }
  return a.name.localeCompare(b.name) || a.code.localeCompare(b.code);
}

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    const payload = err.payload as { message?: unknown; counts?: { items?: unknown; upload_groups?: unknown } } | null;
    if (payload?.counts) {
      const items = typeof payload.counts.items === 'number' ? payload.counts.items : 0;
      const uploadGroups = typeof payload.counts.upload_groups === 'number' ? payload.counts.upload_groups : 0;
      return `${err.message} (${items} item refs, ${uploadGroups} upload group refs)`;
    }
    return err.message;
  }
  if (err instanceof Error) {
    return err.message;
  }
  return fallback;
}
