import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '../api/client';
import {
  createSellProvider,
  deleteSellProvider,
  disableSellProvider,
  enableSellProvider,
  getSellProvider,
  listSellProviders,
  updateSellProvider,
} from '../api/sell';
import { Panel } from '../components/Panel';
import type {
  CreateSellProviderInput,
  SellProviderConfig,
  SellProviderType,
  UpdateSellProviderInput,
} from '../types/sell';

interface ProviderFormState {
  providerType: SellProviderType;
  displayName: string;
  enabled: boolean;
  sortOrder: string;
  iconKey: string;
  baseUrl: string;
  sellerProfileUrl: string;
  notes: string;
}

const supportedProviderTypes: SellProviderType[] = ['facebook_marketplace', 'ebay', 'craigslist', 'etsy'];

const providerDefaults: Record<SellProviderType, { displayName: string; iconKey: string; sortOrder: number; baseUrl: string }> = {
  facebook_marketplace: {
    displayName: 'Facebook Marketplace',
    iconKey: 'meta',
    sortOrder: 10,
    baseUrl: 'https://www.facebook.com/marketplace/',
  },
  ebay: {
    displayName: 'eBay',
    iconKey: 'ebay',
    sortOrder: 20,
    baseUrl: 'https://www.ebay.com/',
  },
  craigslist: {
    displayName: 'Craigslist',
    iconKey: 'craigslist',
    sortOrder: 30,
    baseUrl: 'https://www.craigslist.org/',
  },
  etsy: {
    displayName: 'Etsy',
    iconKey: 'etsy',
    sortOrder: 40,
    baseUrl: 'https://www.etsy.com/',
  },
};

const initialCreateForm = buildCreateForm('facebook_marketplace');

export function AdminSellPage() {
  const [providers, setProviders] = useState<SellProviderConfig[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [modalError, setModalError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [editingProviderId, setEditingProviderId] = useState<string | null>(null);
  const [editForm, setEditForm] = useState<ProviderFormState | null>(null);
  const [createForm, setCreateForm] = useState<ProviderFormState>(initialCreateForm);
  const [isCreateModalOpen, setIsCreateModalOpen] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<SellProviderConfig | null>(null);
  const [disableTarget, setDisableTarget] = useState<SellProviderConfig | null>(null);

  const existingProviderTypes = useMemo(() => new Set(providers.map((provider) => provider.provider_type)), [providers]);

  const availableCreateTypes = useMemo(
    () => supportedProviderTypes.filter((providerType) => !existingProviderTypes.has(providerType)),
    [existingProviderTypes],
  );

  const loadProviders = async () => {
    setIsLoading(true);
    setLoadError(null);

    try {
      const response = await listSellProviders();
      setProviders(response.providers);
    } catch (error) {
      console.error('Failed to load sell providers', error);
      setLoadError(errorMessage(error, 'Failed to load sell providers.'));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadProviders();
  }, []);

  useEffect(() => {
    if (availableCreateTypes.length === 0) {
      return;
    }

    setCreateForm((current) => {
      if (availableCreateTypes.includes(current.providerType)) {
        return current;
      }
      return buildCreateForm(availableCreateTypes[0]);
    });
  }, [availableCreateTypes]);

  useEffect(() => {
    const hasModal = isCreateModalOpen || !!editingProviderId || !!deleteTarget || !!disableTarget;
    if (!hasModal) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key !== 'Escape' || busyAction !== null) {
        return;
      }
      closeAllModals();
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, [busyAction, deleteTarget, disableTarget, editingProviderId, isCreateModalOpen]);

  const handleRefresh = async () => {
    setMessage(null);
    setActionError(null);
    await loadProviders();
  };

  const closeAllModals = () => {
    setEditingProviderId(null);
    setEditForm(null);
    setIsCreateModalOpen(false);
    setDeleteTarget(null);
    setDisableTarget(null);
    setModalError(null);
  };

  const handleOpenCreateModal = () => {
    setModalError(null);
    setActionError(null);
    setMessage(null);
    if (availableCreateTypes.length === 0) {
      return;
    }
    setCreateForm(buildCreateForm(availableCreateTypes[0]));
    setIsCreateModalOpen(true);
  };

  const handleEdit = async (providerId: string) => {
    setBusyAction(`edit:${providerId}`);
    setActionError(null);
    setModalError(null);
    setMessage(null);

    try {
      const response = await getSellProvider(providerId);
      setEditingProviderId(providerId);
      setEditForm(providerToForm(response.provider));
    } catch (error) {
      console.error('Failed to load sell provider', error);
      setActionError(errorMessage(error, 'Failed to load sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleSave = async () => {
    if (!editingProviderId || !editForm) {
      return;
    }

    const validationError = validateForm(editForm, 'edit');
    if (validationError) {
      setModalError(validationError);
      return;
    }

    setBusyAction(`save:${editingProviderId}`);
    setModalError(null);
    setMessage(null);

    try {
      const payload = buildUpdateInput(editForm);
      const response = await updateSellProvider(editingProviderId, payload);
      setProviders((current) => current.map((provider) => (provider.id === response.provider.id ? response.provider : provider)));
      closeAllModals();
      setMessage(`Updated "${response.provider.display_name}".`);
    } catch (error) {
      console.error('Failed to update sell provider', error);
      setModalError(errorMessage(error, 'Failed to update sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleCreate = async () => {
    const validationError = validateForm(createForm, 'create');
    if (validationError) {
      setModalError(validationError);
      return;
    }

    setBusyAction('create');
    setModalError(null);
    setMessage(null);

    try {
      const payload = buildCreateInput(createForm);
      const response = await createSellProvider(payload);
      setProviders((current) => sortProviders([...current, response.provider]));
      closeAllModals();
      setMessage(`Created "${response.provider.display_name}".`);
    } catch (error) {
      console.error('Failed to create sell provider', error);
      setModalError(errorMessage(error, 'Failed to create sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleEnable = async (provider: SellProviderConfig) => {
    setBusyAction(`toggle:${provider.id}`);
    setActionError(null);
    setMessage(null);

    try {
      const response = await enableSellProvider(provider.id);
      setProviders((current) => current.map((item) => (item.id === response.provider.id ? response.provider : item)));
      if (editingProviderId === response.provider.id) {
        setEditForm(providerToForm(response.provider));
      }
      setMessage(`${response.provider.display_name} enabled.`);
    } catch (error) {
      console.error('Failed to enable sell provider', error);
      setActionError(errorMessage(error, 'Failed to enable sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDisable = (provider: SellProviderConfig) => {
    setDisableTarget(provider);
    setModalError(null);
    setActionError(null);
    setMessage(null);
  };

  const handleConfirmDisable = async () => {
    if (!disableTarget) {
      return;
    }

    setBusyAction(`toggle:${disableTarget.id}`);
    setModalError(null);
    setMessage(null);

    try {
      const response = await disableSellProvider(disableTarget.id);
      setProviders((current) => current.map((item) => (item.id === response.provider.id ? response.provider : item)));
      closeAllModals();
      setMessage(`${response.provider.display_name} disabled.`);
    } catch (error) {
      console.error('Failed to disable sell provider', error);
      setModalError(errorMessage(error, 'Failed to disable sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDelete = (provider: SellProviderConfig) => {
    setDeleteTarget(provider);
    setModalError(null);
    setActionError(null);
    setMessage(null);
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) {
      return;
    }

    setBusyAction(`delete:${deleteTarget.id}`);
    setModalError(null);
    setMessage(null);

    try {
      await deleteSellProvider(deleteTarget.id);
      setProviders((current) => current.filter((item) => item.id !== deleteTarget.id));
      closeAllModals();
      setMessage(`Deleted "${deleteTarget.display_name}".`);
    } catch (error) {
      console.error('Failed to delete sell provider', error);
      setModalError(errorMessage(error, 'Failed to delete sell provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel
        title="Admin / Sell"
        eyebrow="Provider configuration"
        action={(
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={handleOpenCreateModal}
              disabled={busyAction !== null || availableCreateTypes.length === 0}
              className="rounded-md border border-copper-500/35 bg-copper-500/12 px-3 py-2 text-sm font-semibold text-amberline-100 transition hover:bg-copper-500/18 disabled:cursor-not-allowed disabled:opacity-60"
            >
              Add Provider
            </button>
            <button
              type="button"
              onClick={() => {
                void handleRefresh();
              }}
              disabled={isLoading}
              className="rounded-md border border-amberline-500/35 bg-copper-500/12 px-3 py-2 text-sm font-semibold text-amberline-100 transition hover:bg-copper-500/18 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {isLoading ? 'Refreshing...' : 'Refresh'}
            </button>
          </div>
        )}
      >
        <div className="grid gap-3">
          <p className="text-sm text-stone-300">
            Configure marketplace providers for future listing workflows. This version does not log in or post listings.
          </p>
          <div className="rounded-md border border-copper-500/28 bg-copper-500/10 px-4 py-3 text-sm text-amberline-100">
            Provider login and automatic posting are not implemented yet.
          </div>
          {availableCreateTypes.length === 0 ? (
            <p className="text-sm text-stone-400">All supported providers are already configured.</p>
          ) : null}
        </div>
      </Panel>

      {loadError ? (
        <Panel title="Load Error" eyebrow="Sell providers unavailable">
          <div className="grid gap-3">
            <p className="text-sm text-red-200">{loadError}</p>
            <div>
              <button
                type="button"
                onClick={() => {
                  void handleRefresh();
                }}
                className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12"
              >
                Retry
              </button>
            </div>
          </div>
        </Panel>
      ) : null}

      {message ? <StatusPanel title="Status" message={message} tone="success" /> : null}
      {actionError ? <StatusPanel title="Action Error" message={actionError} tone="error" /> : null}

      <Panel title="Providers" eyebrow="Seeded marketplace set">
        {isLoading && providers.length === 0 ? (
          <p className="text-sm text-stone-300">Loading sell providers...</p>
        ) : providers.length === 0 ? (
          <p className="text-sm text-stone-400">No sell providers configured.</p>
        ) : (
          <div className="grid gap-4">
            {providers.map((provider) => (
              <article key={provider.id} className={`rounded-md border p-4 ${provider.enabled ? 'border-rack-steel/25 bg-rack-soot/60' : 'border-red-400/18 bg-rack-soot/45 opacity-85'}`}>
                <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                  <div className="flex items-start gap-3">
                    <ProviderIconBadge iconKey={provider.icon_key} />
                    <div className="grid gap-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <h3 className="text-base font-semibold text-stone-100">{provider.display_name}</h3>
                        <ProviderStateBadge enabled={provider.enabled} />
                      </div>
                      <p className="text-xs uppercase tracking-[0.18em] text-rack-glass">{provider.provider_type}</p>
                    </div>
                  </div>
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      onClick={() => {
                        void handleEdit(provider.id);
                      }}
                      disabled={busyAction !== null}
                      className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      Edit
                    </button>
                    {provider.enabled ? (
                      <button
                        type="button"
                        onClick={() => handleRequestDisable(provider)}
                        disabled={busyAction !== null}
                        className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
                      >
                        Disable
                      </button>
                    ) : (
                      <button
                        type="button"
                        onClick={() => {
                          void handleEnable(provider);
                        }}
                        disabled={busyAction !== null}
                        className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
                      >
                        {busyAction === `toggle:${provider.id}` ? 'Updating...' : 'Enable'}
                      </button>
                    )}
                    <button
                      type="button"
                      onClick={() => handleRequestDelete(provider)}
                      disabled={busyAction !== null}
                      className="rounded-md border border-red-400/35 bg-red-950/20 px-3 py-2 text-sm font-semibold text-red-100 transition hover:bg-red-900/30 disabled:cursor-not-allowed disabled:opacity-60"
                    >
                      Delete
                    </button>
                  </div>
                </div>

                <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
                  <MetaRow label="Sort order" value={String(provider.sort_order)} />
                  <MetaRow label="Icon key" value={provider.icon_key} />
                  <MetaRow label="Base URL" value={provider.base_url ?? 'Not set'} />
                  <MetaRow label="Seller profile URL" value={provider.seller_profile_url ?? 'Not set'} />
                </div>

                <div className="mt-4 grid gap-1">
                  <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-rack-glass">Notes</p>
                  <p className="text-sm text-stone-300">{truncateText(provider.notes?.trim() || 'No notes set.', 200)}</p>
                </div>
              </article>
            ))}
          </div>
        )}
      </Panel>

      {editingProviderId && editForm ? (
        <SellProviderModal
          title="Edit Sell Provider"
          onClose={busyAction === null ? closeAllModals : undefined}
          actions={(
            <>
              <button
                type="button"
                onClick={closeAllModals}
                disabled={busyAction !== null}
                className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleSave();
                }}
                disabled={busyAction !== null}
                className="rounded-md border border-amberline-500/35 bg-copper-500/12 px-3 py-2 text-sm font-semibold text-amberline-100 transition hover:bg-copper-500/18 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busyAction === `save:${editingProviderId}` ? 'Saving...' : 'Save'}
              </button>
            </>
          )}
        >
          {modalError ? <ModalError message={modalError} /> : null}
          <ReadOnlyField label="Provider type" value={editForm.providerType} />
          <ProviderFormFields form={editForm} onChange={(nextForm) => setEditForm(nextForm)} allowProviderTypeChange={false} />
        </SellProviderModal>
      ) : null}

      {isCreateModalOpen ? (
        <SellProviderModal
          title="Add Sell Provider"
          onClose={busyAction === null ? closeAllModals : undefined}
          actions={(
            <>
              <button
                type="button"
                onClick={closeAllModals}
                disabled={busyAction !== null}
                className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleCreate();
                }}
                disabled={busyAction !== null || availableCreateTypes.length === 0}
                className="rounded-md border border-amberline-500/35 bg-copper-500/12 px-3 py-2 text-sm font-semibold text-amberline-100 transition hover:bg-copper-500/18 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busyAction === 'create' ? 'Creating...' : 'Create'}
              </button>
            </>
          )}
        >
          {availableCreateTypes.length === 0 ? (
            <p className="text-sm text-stone-400">All supported providers are already configured.</p>
          ) : (
            <>
              {modalError ? <ModalError message={modalError} /> : null}
              <ProviderFormFields form={createForm} onChange={(nextForm) => setCreateForm(nextForm)} allowProviderTypeChange availableProviderTypes={availableCreateTypes} />
            </>
          )}
        </SellProviderModal>
      ) : null}

      {disableTarget ? (
        <SellProviderModal
          title="Disable Sell Provider"
          onClose={busyAction === null ? closeAllModals : undefined}
          actions={(
            <>
              <button
                type="button"
                onClick={closeAllModals}
                disabled={busyAction !== null}
                className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleConfirmDisable();
                }}
                disabled={busyAction !== null}
                className="rounded-md border border-copper-500/35 bg-copper-500/12 px-3 py-2 text-sm font-semibold text-amberline-100 transition hover:bg-copper-500/18 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busyAction === `toggle:${disableTarget.id}` ? 'Disabling...' : 'Disable'}
              </button>
            </>
          )}
        >
          {modalError ? <ModalError message={modalError} /> : null}
          <div className="grid gap-3 text-sm text-stone-300">
            <p>This disables the provider for future listing workflows.</p>
            <p>It does not delete the provider configuration and it can be re-enabled later.</p>
            <div className="rounded-md border border-rack-steel/24 bg-rack-soot/60 p-3">
              <p className="font-semibold text-stone-100">{disableTarget.display_name}</p>
              <p className="mt-1 text-xs uppercase tracking-[0.18em] text-rack-glass">{disableTarget.provider_type}</p>
            </div>
          </div>
        </SellProviderModal>
      ) : null}

      {deleteTarget ? (
        <SellProviderModal
          title="Delete Sell Provider"
          onClose={busyAction === null ? closeAllModals : undefined}
          actions={(
            <>
              <button
                type="button"
                onClick={closeAllModals}
                disabled={busyAction !== null}
                className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-60"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={() => {
                  void handleConfirmDelete();
                }}
                disabled={busyAction !== null}
                className="rounded-md border border-red-400/35 bg-red-950/25 px-3 py-2 text-sm font-semibold text-red-100 transition hover:bg-red-900/30 disabled:cursor-not-allowed disabled:opacity-60"
              >
                {busyAction === `delete:${deleteTarget.id}` ? 'Deleting...' : 'Delete Provider'}
              </button>
            </>
          )}
        >
          {modalError ? <ModalError message={modalError} /> : null}
          <div className="grid gap-3 text-sm text-stone-300">
            <p>This only deletes the provider configuration row.</p>
            <p>It does not delete inventory items, upload data, or post to or modify any external marketplace.</p>
            <div className="rounded-md border border-red-400/20 bg-red-950/10 p-3">
              <p className="font-semibold text-stone-100">{deleteTarget.display_name}</p>
              <p className="mt-1 text-xs uppercase tracking-[0.18em] text-rack-glass">{deleteTarget.provider_type}</p>
            </div>
          </div>
        </SellProviderModal>
      ) : null}
    </div>
  );
}

function ProviderFormFields({
  form,
  onChange,
  allowProviderTypeChange,
  availableProviderTypes = supportedProviderTypes,
}: {
  form: ProviderFormState;
  onChange: (value: ProviderFormState) => void;
  allowProviderTypeChange: boolean;
  availableProviderTypes?: SellProviderType[];
}) {
  const updateField = <K extends keyof ProviderFormState>(key: K, value: ProviderFormState[K]) => {
    onChange({ ...form, [key]: value });
  };

  return (
    <div className="grid gap-4">
      {allowProviderTypeChange ? (
        <label className="grid gap-2 text-sm text-stone-200">
          <span className="font-semibold">Provider type</span>
          <select
            value={form.providerType}
            onChange={(event) => {
              const nextType = event.target.value as SellProviderType;
              onChange(buildCreateForm(nextType));
            }}
            className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
          >
            {availableProviderTypes.map((providerType) => (
              <option key={providerType} value={providerType}>
                {providerDefaults[providerType].displayName}
              </option>
            ))}
          </select>
        </label>
      ) : null}

      <label className="grid gap-2 text-sm text-stone-200">
        <span className="font-semibold">Display name</span>
        <input
          value={form.displayName}
          onChange={(event) => updateField('displayName', event.target.value)}
          className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
        />
      </label>

      <label className="inline-flex items-center gap-3 text-sm text-stone-200">
        <input
          type="checkbox"
          checked={form.enabled}
          onChange={(event) => updateField('enabled', event.target.checked)}
          className="h-4 w-4 rounded border-rack-steel/30 bg-graphite-950 text-copper-500 focus:ring-copper-500"
        />
        <span className="font-semibold">Enabled</span>
      </label>

      <div className="grid gap-4 sm:grid-cols-2">
        <label className="grid gap-2 text-sm text-stone-200">
          <span className="font-semibold">Sort order</span>
          <input
            type="number"
            min={0}
            value={form.sortOrder}
            onChange={(event) => updateField('sortOrder', event.target.value)}
            className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
          />
        </label>

        <label className="grid gap-2 text-sm text-stone-200">
          <span className="font-semibold">Icon key</span>
          <input
            value={form.iconKey}
            onChange={(event) => updateField('iconKey', event.target.value)}
            className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
          />
        </label>
      </div>

      <label className="grid gap-2 text-sm text-stone-200">
        <span className="font-semibold">Base URL</span>
        <input
          value={form.baseUrl}
          onChange={(event) => updateField('baseUrl', event.target.value)}
          placeholder="https://example.com/"
          className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
        />
      </label>

      <label className="grid gap-2 text-sm text-stone-200">
        <span className="font-semibold">Seller profile URL</span>
        <input
          value={form.sellerProfileUrl}
          onChange={(event) => updateField('sellerProfileUrl', event.target.value)}
          placeholder="https://example.com/seller/..."
          className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
        />
      </label>

      <label className="grid gap-2 text-sm text-stone-200">
        <span className="font-semibold">Notes</span>
        <textarea
          value={form.notes}
          onChange={(event) => updateField('notes', event.target.value)}
          rows={4}
          className="rounded-md border border-rack-steel/25 bg-graphite-950 px-3 py-2 text-sm text-stone-100 outline-none transition focus:border-copper-400/60"
        />
      </label>
    </div>
  );
}

function ProviderIconBadge({ iconKey }: { iconKey: string }) {
  return (
    <div className="inline-flex h-12 w-12 items-center justify-center rounded-md border border-copper-500/28 bg-copper-500/10 text-xs font-semibold uppercase tracking-[0.18em] text-amberline-100">
      {iconLabel(iconKey)}
    </div>
  );
}

function ProviderStateBadge({ enabled }: { enabled: boolean }) {
  return (
    <span className={`inline-flex items-center rounded-full border px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.16em] ${enabled ? 'border-green-500/28 bg-green-500/10 text-green-100' : 'border-red-400/28 bg-red-500/10 text-red-100'}`}>
      {enabled ? 'Enabled' : 'Disabled'}
    </span>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1">
      <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-rack-glass">{label}</p>
      <p className="break-all text-sm text-stone-200">{value}</p>
    </div>
  );
}

function ReadOnlyField({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-2 text-sm text-stone-200">
      <span className="font-semibold">{label}</span>
      <div className="rounded-md border border-rack-steel/20 bg-black/15 px-3 py-2 text-sm text-stone-400">{value}</div>
    </div>
  );
}

function SellProviderModal({
  title,
  children,
  actions,
  onClose,
}: {
  title: string;
  children: React.ReactNode;
  actions: React.ReactNode;
  onClose?: () => void;
}) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 px-4 py-6"
      onClick={() => onClose?.()}
      role="dialog"
      aria-modal="true"
      aria-label={title}
    >
      <div className="max-h-[92vh] w-full max-w-2xl overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel title={title} eyebrow="Admin / Sell" action={<div className="flex flex-wrap gap-2">{actions}</div>}>
          <div className="grid gap-4">{children}</div>
        </Panel>
      </div>
    </div>
  );
}

function ModalError({ message }: { message: string }) {
  return <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{message}</p>;
}

function StatusPanel({ title, message, tone }: { title: string; message: string; tone: 'success' | 'error' }) {
  return (
    <Panel title={title} eyebrow={tone === 'success' ? 'Update complete' : 'Fix required'}>
      <p className={tone === 'success' ? 'text-sm text-green-100' : 'text-sm text-red-200'}>{message}</p>
    </Panel>
  );
}

function buildCreateForm(providerType: SellProviderType): ProviderFormState {
  const defaults = providerDefaults[providerType];
  return {
    providerType,
    displayName: defaults.displayName,
    enabled: true,
    sortOrder: String(defaults.sortOrder),
    iconKey: defaults.iconKey,
    baseUrl: defaults.baseUrl,
    sellerProfileUrl: '',
    notes: '',
  };
}

function providerToForm(provider: SellProviderConfig): ProviderFormState {
  return {
    providerType: provider.provider_type,
    displayName: provider.display_name,
    enabled: provider.enabled,
    sortOrder: String(provider.sort_order),
    iconKey: provider.icon_key,
    baseUrl: provider.base_url ?? '',
    sellerProfileUrl: provider.seller_profile_url ?? '',
    notes: provider.notes ?? '',
  };
}

function buildCreateInput(form: ProviderFormState): CreateSellProviderInput {
  return {
    provider_type: form.providerType,
    display_name: form.displayName.trim(),
    enabled: form.enabled,
    sort_order: Number(form.sortOrder),
    icon_key: form.iconKey.trim(),
    base_url: normalizeNullableString(form.baseUrl),
    seller_profile_url: normalizeNullableString(form.sellerProfileUrl),
    notes: normalizeNullableString(form.notes),
  };
}

function buildUpdateInput(form: ProviderFormState): UpdateSellProviderInput {
  return {
    display_name: form.displayName.trim(),
    enabled: form.enabled,
    sort_order: Number(form.sortOrder),
    icon_key: form.iconKey.trim(),
    base_url: normalizeNullableString(form.baseUrl),
    seller_profile_url: normalizeNullableString(form.sellerProfileUrl),
    notes: normalizeNullableString(form.notes),
  };
}

function validateForm(form: ProviderFormState, mode: 'create' | 'edit'): string | null {
  if (mode === 'create' && !supportedProviderTypes.includes(form.providerType)) {
    return 'Provider type must be one of the supported providers.';
  }
  if (!form.displayName.trim()) {
    return 'Display name is required.';
  }
  if (!form.iconKey.trim()) {
    return 'Icon key is required.';
  }
  const sortOrder = Number(form.sortOrder);
  if (!Number.isInteger(sortOrder) || sortOrder < 0) {
    return 'Sort order must be a whole number greater than or equal to 0.';
  }
  for (const [label, value] of [['Base URL', form.baseUrl], ['Seller profile URL', form.sellerProfileUrl]] as const) {
    if (value.trim() && !isValidHTTPURL(value.trim())) {
      return `${label} must be a valid http or https URL.`;
    }
  }
  return null;
}

function normalizeNullableString(value: string): string | null {
  const trimmed = value.trim();
  return trimmed ? trimmed : null;
}

function isValidHTTPURL(value: string): boolean {
  try {
    const parsed = new URL(value);
    return parsed.protocol === 'http:' || parsed.protocol === 'https:';
  } catch {
    return false;
  }
}

function sortProviders(providers: SellProviderConfig[]): SellProviderConfig[] {
  return [...providers].sort((a, b) => {
    if (a.sort_order !== b.sort_order) {
      return a.sort_order - b.sort_order;
    }
    return a.display_name.localeCompare(b.display_name);
  });
}

function truncateText(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, maxLength - 1)}...`;
}

function iconLabel(iconKey: string): string {
  switch (iconKey.trim().toLowerCase()) {
    case 'meta':
      return 'Meta';
    case 'ebay':
      return 'eBay';
    case 'craigslist':
      return 'CL';
    case 'etsy':
      return 'Etsy';
    default:
      return iconKey.slice(0, 4).toUpperCase() || 'Sell';
  }
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
