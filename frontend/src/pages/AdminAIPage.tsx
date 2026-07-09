import { useEffect, useMemo, useState } from 'react';
import { ApiError } from '../api/client';
import {
  createAIProvider,
  deleteAIProvider,
  getAIProvider,
  getAISettings,
  listAIProviders,
  setActiveAIProvider,
  testAIProvider,
  updateAIProvider,
  updateAISettings,
} from '../api/ai';
import { Panel } from '../components/Panel';
import type {
  AIProviderConfig,
  AIProviderTestResult,
  AIProviderType,
  AISettings,
  CreateAIProviderInput,
  UpdateAIProviderInput,
} from '../types/ai';

interface ProviderFormState {
  name: string;
  providerType: AIProviderType;
  enabled: boolean;
  active: boolean;
  baseUrl: string;
  apiKeyValue: string;
  apiKeyEnvVar: string;
  modelName: string;
  visionEnabled: boolean;
  timeoutSeconds: string;
  maxOutputTokens: string;
  temperature: string;
  clearApiKey: boolean;
}

const providerTypes: AIProviderType[] = ['gemini', 'openai', 'ollama'];

const createFormDefaultsByProviderType: Record<AIProviderType, ProviderFormState> = {
  gemini: {
    name: 'Gemini Vision',
    providerType: 'gemini',
    enabled: true,
    active: false,
    baseUrl: '',
    apiKeyValue: '',
    apiKeyEnvVar: 'GEMINI_API_KEY',
    modelName: 'gemini-3.1-flash-lite',
    visionEnabled: true,
    timeoutSeconds: '60',
    maxOutputTokens: '2048',
    temperature: '0.2',
    clearApiKey: false,
  },
  openai: {
    name: '',
    providerType: 'openai',
    enabled: true,
    active: false,
    baseUrl: '',
    apiKeyValue: '',
    apiKeyEnvVar: 'OPENAI_API_KEY',
    modelName: '',
    visionEnabled: true,
    timeoutSeconds: '60',
    maxOutputTokens: '',
    temperature: '',
    clearApiKey: false,
  },
  ollama: {
    name: '',
    providerType: 'ollama',
    enabled: true,
    active: false,
    baseUrl: 'http://localhost:11434',
    apiKeyValue: '',
    apiKeyEnvVar: '',
    modelName: '',
    visionEnabled: true,
    timeoutSeconds: '60',
    maxOutputTokens: '',
    temperature: '',
    clearApiKey: false,
  },
};

const initialForm: ProviderFormState = createFormDefaultsByProviderType.gemini;

export function AdminAIPage() {
  const [providers, setProviders] = useState<AIProviderConfig[]>([]);
  const [settings, setSettings] = useState<AISettings | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [formMode, setFormMode] = useState<'create' | 'edit'>('create');
  const [editingProviderId, setEditingProviderId] = useState<string | null>(null);
  const [form, setForm] = useState<ProviderFormState>(initialForm);
  const [editingProviderConfiguredKey, setEditingProviderConfiguredKey] = useState(false);
  const [lastTestResult, setLastTestResult] = useState<AIProviderTestResult | null>(null);

  const activeProvider = useMemo(
    () => providers.find((provider) => provider.active) ?? null,
    [providers],
  );

  const loadData = async () => {
    setIsLoading(true);
    setLoadError(null);

    try {
      const [providersResponse, settingsResponse] = await Promise.all([listAIProviders(), getAISettings()]);
      setProviders(providersResponse.providers);
      setSettings(settingsResponse.settings);
    } catch (error) {
      console.error('Failed to load AI admin data', error);
      setLoadError(errorMessage(error, 'Failed to load AI provider configuration.'));
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    void loadData();
  }, []);

  const resetForm = () => {
    setForm(initialForm);
    setFormMode('create');
    setEditingProviderId(null);
    setEditingProviderConfiguredKey(false);
  };

  const handleProviderTypeChange = (providerType: AIProviderType) => {
    setForm((current) => {
      if (formMode !== 'create') {
        return {
          ...current,
          providerType,
          baseUrl: providerType === 'ollama' && !current.baseUrl.trim() ? 'http://localhost:11434' : current.baseUrl,
        };
      }

      return {
        ...createFormDefaultsByProviderType[providerType],
        active: current.active,
      };
    });
  };

  const handleEdit = async (providerId: string) => {
    setBusyAction(`edit:${providerId}`);
    setActionError(null);

    try {
      const response = await getAIProvider(providerId);
      const provider = response.provider;
      setFormMode('edit');
      setEditingProviderId(provider.id);
      setEditingProviderConfiguredKey(provider.api_key_configured);
      setForm({
        name: provider.name,
        providerType: provider.provider_type,
        enabled: provider.enabled,
        active: provider.active,
        baseUrl: provider.base_url ?? '',
        apiKeyValue: '',
        apiKeyEnvVar: provider.api_key_env_var ?? '',
        modelName: provider.model_name,
        visionEnabled: provider.vision_enabled,
        timeoutSeconds: String(provider.timeout_seconds),
        maxOutputTokens: provider.max_output_tokens != null ? String(provider.max_output_tokens) : '',
        temperature: provider.temperature != null ? String(provider.temperature) : '',
        clearApiKey: false,
      });
    } catch (error) {
      console.error('Failed to load provider for edit', error);
      setActionError(errorMessage(error, 'Failed to load AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleSubmit = async () => {
    const validationError = validateForm(form);
    if (validationError) {
      setActionError(validationError);
      return;
    }

    setBusyAction(formMode === 'create' ? 'create' : `save:${editingProviderId}`);
    setActionError(null);

    try {
      if (formMode === 'create') {
        const payload = buildCreateInput(form);
        await createAIProvider(payload);
      } else if (editingProviderId) {
        const payload = buildUpdateInput(form);
        await updateAIProvider(editingProviderId, payload);
      }

      resetForm();
      await loadData();
    } catch (error) {
      console.error(formMode === 'create' ? 'Failed to create AI provider' : 'Failed to update AI provider', error);
      setActionError(errorMessage(error, formMode === 'create' ? 'Failed to create AI provider.' : 'Failed to update AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleDelete = async (provider: AIProviderConfig) => {
    if (!window.confirm(`Delete AI provider "${provider.name}"? This cannot be undone.`)) {
      return;
    }

    setBusyAction(`delete:${provider.id}`);
    setActionError(null);

    try {
      await deleteAIProvider(provider.id);
      if (editingProviderId === provider.id) {
        resetForm();
      }
      await loadData();
    } catch (error) {
      console.error('Failed to delete AI provider', error);
      setActionError(errorMessage(error, 'Failed to delete AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleSetActive = async (provider: AIProviderConfig) => {
    setBusyAction(`active:${provider.id}`);
    setActionError(null);

    try {
      await setActiveAIProvider(provider.id);
      await loadData();
    } catch (error) {
      console.error('Failed to set active AI provider', error);
      setActionError(errorMessage(error, 'Failed to set active AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleEnableToggle = async (provider: AIProviderConfig) => {
    setBusyAction(`enable:${provider.id}`);
    setActionError(null);

    try {
      await updateAIProvider(provider.id, { enabled: !provider.enabled });
      await loadData();
    } catch (error) {
      console.error('Failed to update AI provider enabled state', error);
      setActionError(errorMessage(error, 'Failed to update AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleTest = async (provider: AIProviderConfig) => {
    setBusyAction(`test:${provider.id}`);
    setActionError(null);

    try {
      const result = await testAIProvider(provider.id);
      setLastTestResult(result);
      await loadData();
    } catch (error) {
      console.error('Failed to test AI provider', error);
      setActionError(errorMessage(error, 'Failed to test AI provider.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleDisableAI = async () => {
    setBusyAction('settings:disable');
    setActionError(null);

    try {
      const response = await updateAISettings({ ai_assist_enabled: false });
      setSettings(response.settings);
      await loadData();
    } catch (error) {
      console.error('Failed to disable AI assist', error);
      setActionError(errorMessage(error, 'Failed to disable AI assist.'));
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel title="AI Configuration" eyebrow="Admin / provider setup">
        <p className="text-sm text-stone-300">
          Configure Gemini, OpenAI, or Ollama providers. Only one provider can be active at a time.
        </p>
        {actionError ? <p className="mt-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{actionError}</p> : null}
        {loadError ? <p className="mt-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{loadError}</p> : null}
      </Panel>

      <div className="grid gap-6 xl:grid-cols-[0.9fr_1.1fr]">
        <Panel title="Global AI Status" eyebrow="Derived settings">
          {isLoading ? (
            <p className="text-sm text-stone-400">Loading AI settings...</p>
          ) : (
            <div className="grid gap-3">
              <InfoRow label="AI assist enabled" value={settings?.ai_assist_enabled ? 'Yes' : 'No'} />
              <InfoRow label="Active provider" value={settings?.active_provider_name ?? 'None'} />
              <InfoRow label="Provider type" value={settings?.active_provider_type ?? 'None'} />
              <InfoRow label="Model" value={settings?.active_model_name ?? 'None'} />
              <div className="pt-2">
                <button
                  type="button"
                  disabled={busyAction === 'settings:disable' || !settings?.ai_assist_enabled}
                  onClick={() => void handleDisableAI()}
                  className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {busyAction === 'settings:disable' ? 'Disabling...' : 'Disable AI assist'}
                </button>
              </div>
            </div>
          )}
        </Panel>

        <Panel title="Active Provider" eyebrow="Current selection">
          {isLoading ? (
            <p className="text-sm text-stone-400">Loading active provider...</p>
          ) : activeProvider ? (
            <div className="grid gap-3">
              <InfoRow label="Name" value={activeProvider.name} />
              <InfoRow label="Type" value={activeProvider.provider_type} />
              <InfoRow label="Model" value={activeProvider.model_name} />
              <InfoRow label="Vision enabled" value={activeProvider.vision_enabled ? 'Yes' : 'No'} />
              <InfoRow label="Base URL" value={activeProvider.base_url ?? 'Default'} />
            </div>
          ) : (
            <p className="text-sm text-stone-300">No active provider is configured.</p>
          )}
        </Panel>
      </div>

      <Panel
        title="Configured Providers"
        eyebrow="Gemini / OpenAI / Ollama"
        action={
          <button
            type="button"
            onClick={resetForm}
            className="rounded-md border border-copper-500/35 px-3 py-2 text-sm font-semibold text-amberline-100 hover:bg-copper-500/12"
          >
            Add Provider
          </button>
        }
      >
        {isLoading ? <p className="text-sm text-stone-400">Loading providers...</p> : null}
        {!isLoading && providers.length === 0 ? <p className="text-sm text-stone-300">No AI providers configured.</p> : null}

        <div className="grid gap-3">
          {providers.map((provider) => (
            <ProviderCard
              key={provider.id}
              provider={provider}
              busyAction={busyAction}
              onEdit={() => void handleEdit(provider.id)}
              onTest={() => void handleTest(provider)}
              onSetActive={() => void handleSetActive(provider)}
              onToggleEnabled={() => void handleEnableToggle(provider)}
              onDelete={() => void handleDelete(provider)}
            />
          ))}
        </div>
      </Panel>

      <Panel title="Provider Form" eyebrow={formMode === 'create' ? 'Create provider' : 'Edit provider'}>
        <div className="grid gap-4 lg:grid-cols-2">
          <TextInput label="Name" value={form.name} onChange={(value) => setForm((current) => ({ ...current, name: value }))} />

          <SelectInput
            label="Provider Type"
            value={form.providerType}
            onChange={(value) => handleProviderTypeChange(value as AIProviderType)}
            options={providerTypes.map((providerType) => ({ value: providerType, label: providerType }))}
          />

          <TextInput label="Base URL" value={form.baseUrl} onChange={(value) => setForm((current) => ({ ...current, baseUrl: value }))} placeholder={form.providerType === 'ollama' ? 'http://localhost:11434' : 'Optional override'} />

          <TextInput label="Model Name" value={form.modelName} onChange={(value) => setForm((current) => ({ ...current, modelName: value }))} />

          <PasswordInput
            label="API Key"
            value={form.apiKeyValue}
            onChange={(value) => setForm((current) => ({ ...current, apiKeyValue: value }))}
            helperText={formMode === 'edit' && editingProviderConfiguredKey ? 'API key configured. Enter a new key to replace it.' : 'Leave blank to keep empty.'}
          />

          <TextInput label="API Key Env Var" value={form.apiKeyEnvVar} onChange={(value) => setForm((current) => ({ ...current, apiKeyEnvVar: value }))} placeholder={apiKeyEnvVarPlaceholder(form.providerType)} />

          <TextInput label="Timeout Seconds" value={form.timeoutSeconds} onChange={(value) => setForm((current) => ({ ...current, timeoutSeconds: value }))} />

          <TextInput label="Max Output Tokens" value={form.maxOutputTokens} onChange={(value) => setForm((current) => ({ ...current, maxOutputTokens: value }))} placeholder="Optional" />

          <TextInput label="Temperature" value={form.temperature} onChange={(value) => setForm((current) => ({ ...current, temperature: value }))} placeholder="0.0 - 2.0" />

          <div className="grid gap-3 lg:col-span-2 lg:grid-cols-4">
            <CheckboxInput label="Enabled" checked={form.enabled} onChange={(checked) => setForm((current) => ({ ...current, enabled: checked }))} />
            <CheckboxInput label="Set Active" checked={form.active} onChange={(checked) => setForm((current) => ({ ...current, active: checked }))} />
            <CheckboxInput label="Vision Enabled" checked={form.visionEnabled} onChange={(checked) => setForm((current) => ({ ...current, visionEnabled: checked }))} />
            <CheckboxInput
              label="Clear Stored API Key"
              checked={form.clearApiKey}
              disabled={formMode !== 'edit'}
              onChange={(checked) => setForm((current) => ({ ...current, clearApiKey: checked }))}
            />
          </div>
        </div>

        <div className="mt-4 flex flex-wrap gap-2">
          <button
            type="button"
            disabled={busyAction === 'create' || (editingProviderId !== null && busyAction === `save:${editingProviderId}`)}
            onClick={() => void handleSubmit()}
            className="rounded-md border border-copper-500/35 px-4 py-3 text-sm font-semibold text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {busyAction === 'create' || (editingProviderId !== null && busyAction === `save:${editingProviderId}`)
              ? 'Saving...'
              : formMode === 'create'
                ? 'Create Provider'
                : 'Save Provider'}
          </button>
          <button
            type="button"
            onClick={resetForm}
            className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12"
          >
            Cancel
          </button>
        </div>
      </Panel>

      <Panel title="Test Results" eyebrow="Latest provider test">
        {lastTestResult ? (
          <div className="grid gap-3">
            <InfoRow label="Provider ID" value={lastTestResult.provider_id} />
            <InfoRow label="Provider Type" value={lastTestResult.provider_type} />
            <InfoRow label="Model" value={lastTestResult.model_name} />
            <InfoRow label="Status" value={lastTestResult.status} />
            <InfoRow label="Tested" value={new Date(lastTestResult.tested_datetime).toLocaleString()} />
            <InfoRow label="Message" value={lastTestResult.message} />
          </div>
        ) : (
          <p className="text-sm text-stone-300">No provider test has been run in this session.</p>
        )}
      </Panel>
    </div>
  );
}

function ProviderCard({
  provider,
  busyAction,
  onEdit,
  onTest,
  onSetActive,
  onToggleEnabled,
  onDelete,
}: {
  provider: AIProviderConfig;
  busyAction: string | null;
  onEdit: () => void;
  onTest: () => void;
  onSetActive: () => void;
  onToggleEnabled: () => void;
  onDelete: () => void;
}) {
  const isBusy = busyAction !== null && busyAction.includes(provider.id);

  return (
    <article className={`rounded-md border p-4 ${provider.active ? 'border-copper-500/45 bg-rack-soot/85' : 'border-rack-steel/28 bg-rack-soot/70'}`}>
      <div className="grid gap-3 lg:grid-cols-[minmax(0,1.2fr)_minmax(0,1fr)]">
        <div className="grid gap-2">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-lg font-semibold text-stone-100">{provider.name}</h3>
            <Badge tone={provider.active ? 'active' : 'default'}>{provider.active ? 'Active' : 'Inactive'}</Badge>
            <Badge tone={provider.enabled ? 'success' : 'danger'}>{provider.enabled ? 'Enabled' : 'Disabled'}</Badge>
          </div>
          <p className="text-sm text-stone-300">
            {provider.provider_type} · {provider.model_name}
          </p>
          <p className="text-sm text-stone-400">Base URL: {provider.base_url ?? 'Default'}</p>
          <p className="text-sm text-stone-400">API key configured: {provider.api_key_configured ? 'Yes' : 'No'}</p>
          <p className="text-sm text-stone-400">API key display: {provider.api_key_display || 'None'}</p>
          <p className="text-sm text-stone-400">API key env var: {provider.api_key_env_var ?? 'None'}</p>
        </div>

        <div className="grid gap-2 text-sm text-stone-300">
          <InfoRow label="Last test status" value={provider.last_test_status ?? 'Not tested'} compact />
          <InfoRow label="Last test time" value={provider.last_test_datetime ? new Date(provider.last_test_datetime).toLocaleString() : 'Never'} compact />
          <InfoRow label="Last error" value={provider.last_error_message ?? 'None'} compact />
          <InfoRow label="Timeout" value={`${provider.timeout_seconds} seconds`} compact />
        </div>
      </div>

      <div className="mt-4 flex flex-wrap gap-2">
        <button type="button" onClick={onEdit} disabled={isBusy} className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:opacity-50">
          {busyAction === `edit:${provider.id}` ? 'Loading...' : 'Edit'}
        </button>
        <button type="button" onClick={onTest} disabled={isBusy} className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:opacity-50">
          {busyAction === `test:${provider.id}` ? 'Testing...' : 'Test Connection'}
        </button>
        <button
          type="button"
          onClick={onSetActive}
          disabled={isBusy || provider.active}
          className="rounded-md border border-copper-500/35 px-3 py-2 text-sm text-amberline-100 hover:bg-copper-500/12 disabled:opacity-50"
        >
          Set Active
        </button>
        <button
          type="button"
          onClick={onToggleEnabled}
          disabled={isBusy}
          className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:opacity-50"
        >
          {busyAction === `enable:${provider.id}` ? 'Updating...' : provider.enabled ? 'Disable' : 'Enable'}
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={isBusy}
          className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:opacity-50"
        >
          {busyAction === `delete:${provider.id}` ? 'Deleting...' : 'Delete'}
        </button>
      </div>
    </article>
  );
}

function TextInput({
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
        className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
      />
    </label>
  );
}

function PasswordInput({
  label,
  value,
  onChange,
  helperText,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  helperText: string;
}) {
  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      <input
        type="password"
        autoComplete="new-password"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
      />
      <span className="text-xs text-stone-500">{helperText}</span>
    </label>
  );
}

function SelectInput({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  options: Array<{ value: string; label: string }>;
}) {
  return (
    <label className="grid gap-2 text-sm text-stone-200">
      {label}
      <select
        value={value}
        onChange={(event) => onChange(event.target.value)}
        className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
      >
        {options.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </select>
    </label>
  );
}

function CheckboxInput({
  label,
  checked,
  onChange,
  disabled,
}: {
  label: string;
  checked: boolean;
  onChange: (checked: boolean) => void;
  disabled?: boolean;
}) {
  return (
    <label className="flex items-center gap-2 rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-3 text-sm text-stone-200">
      <input type="checkbox" checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} />
      {label}
    </label>
  );
}

function InfoRow({ label, value, compact = false }: { label: string; value: string; compact?: boolean }) {
  return (
    <div className={compact ? 'flex items-start justify-between gap-4' : 'rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-2'}>
      <div className={compact ? 'text-stone-400' : 'text-xs uppercase tracking-[0.16em] text-rack-glass'}>{label}</div>
      <div className={compact ? 'max-w-[60%] text-right text-stone-200' : 'mt-1 text-sm text-stone-200'}>{value}</div>
    </div>
  );
}

function Badge({ children, tone }: { children: string; tone: 'active' | 'success' | 'danger' | 'default' }) {
  const toneClass =
    tone === 'active'
      ? 'border-copper-500/45 bg-copper-500/12 text-amberline-100'
      : tone === 'success'
        ? 'border-signal-green/35 bg-signal-green/10 text-green-100'
        : tone === 'danger'
          ? 'border-red-400/35 bg-red-500/10 text-red-100'
          : 'border-rack-steel/24 bg-rack-soot/75 text-stone-300';

  return <span className={`rounded-full border px-2.5 py-1 text-[11px] ${toneClass}`}>{children}</span>;
}

function buildCreateInput(form: ProviderFormState): CreateAIProviderInput {
  return {
    name: form.name.trim(),
    provider_type: form.providerType,
    enabled: form.enabled,
    active: form.active,
    base_url: form.baseUrl.trim() || null,
    api_key_value: form.apiKeyValue.trim() || null,
    api_key_env_var: form.apiKeyEnvVar.trim() || null,
    model_name: form.modelName.trim(),
    vision_enabled: form.visionEnabled,
    timeout_seconds: parseIntegerInput(form.timeoutSeconds) ?? 60,
    max_output_tokens: parseOptionalIntegerInput(form.maxOutputTokens),
    temperature: parseOptionalFloatInput(form.temperature),
  };
}

function buildUpdateInput(form: ProviderFormState): UpdateAIProviderInput {
  const payload: UpdateAIProviderInput = {
    name: form.name.trim(),
    provider_type: form.providerType,
    enabled: form.enabled,
    active: form.active,
    base_url: form.baseUrl.trim() || null,
    api_key_env_var: form.apiKeyEnvVar.trim() || null,
    model_name: form.modelName.trim(),
    vision_enabled: form.visionEnabled,
    timeout_seconds: parseIntegerInput(form.timeoutSeconds),
    max_output_tokens: parseOptionalIntegerInput(form.maxOutputTokens),
    temperature: parseOptionalFloatInput(form.temperature),
    clear_api_key: form.clearApiKey || undefined,
  };

  if (form.apiKeyValue.trim()) {
    payload.api_key_value = form.apiKeyValue.trim();
  }

  return payload;
}

function apiKeyEnvVarPlaceholder(providerType: AIProviderType): string {
  switch (providerType) {
    case 'gemini':
      return 'GEMINI_API_KEY';
    case 'openai':
      return 'OPENAI_API_KEY';
    case 'ollama':
      return 'Optional';
  }
}

function validateForm(form: ProviderFormState): string | null {
  if (!form.name.trim()) {
    return 'Name is required.';
  }
  if (!form.modelName.trim()) {
    return 'Model Name is required.';
  }
  const timeout = parseIntegerInput(form.timeoutSeconds);
  if (timeout == null || timeout <= 0) {
    return 'Timeout Seconds must be a positive integer.';
  }
  const maxOutputTokens = parseOptionalIntegerInput(form.maxOutputTokens);
  if (form.maxOutputTokens.trim() && (maxOutputTokens == null || maxOutputTokens <= 0)) {
    return 'Max Output Tokens must be a positive integer when set.';
  }
  const temperature = parseOptionalFloatInput(form.temperature);
  if (form.temperature.trim() && (temperature == null || temperature < 0 || temperature > 2)) {
    return 'Temperature must be between 0 and 2.';
  }
  if (form.providerType === 'ollama' && !form.baseUrl.trim()) {
    return 'Base URL is required for Ollama.';
  }
  if (!form.enabled && form.active) {
    return 'Active provider must be enabled.';
  }
  if (form.clearApiKey && form.apiKeyValue.trim()) {
    return 'Clear Stored API Key cannot be used with a replacement API key.';
  }
  return null;
}

function parseIntegerInput(value: string): number | null {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number.parseInt(trimmed, 10);
  return Number.isFinite(parsed) ? parsed : null;
}

function parseOptionalIntegerInput(value: string): number | null {
  return parseIntegerInput(value);
}

function parseOptionalFloatInput(value: string): number | null {
  const trimmed = value.trim();
  if (!trimmed) {
    return null;
  }
  const parsed = Number.parseFloat(trimmed);
  return Number.isFinite(parsed) ? parsed : null;
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
