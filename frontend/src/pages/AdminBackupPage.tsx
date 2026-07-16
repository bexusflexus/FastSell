import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import {
  createDatabaseBackup,
  createMediaArchive,
  deleteBackup,
  getBackupJob,
  getBackupSettings,
  getBackupTimezones,
  listBackups,
  restoreBackup,
  saveBackupSettings,
  validateBackup,
} from '../api/backups';
import { ApiError } from '../api/client';
import { Panel } from '../components/Panel';
import type { BackupJob, BackupSettings, DatabaseBackup } from '../types/backups';
import { formatBytes } from '../utils/formatBytes';

const pollIntervalMs = 1_500;

export function AdminBackupPage() {
  const [settings, setSettings] = useState<BackupSettings | null>(null);
  const [timezones, setTimezones] = useState<string[]>([]);
  const [backups, setBackups] = useState<DatabaseBackup[]>([]);
  const [activeJob, setActiveJob] = useState<BackupJob | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [restoreTarget, setRestoreTarget] = useState<DatabaseBackup | null>(null);
  const [restoreConfirmation, setRestoreConfirmation] = useState('');
  const pollTimer = useRef<number | null>(null);

  const refreshBackups = useCallback(async () => {
    setBackups(await listBackups());
  }, []);

  useEffect(() => {
    let mounted = true;
    const load = async () => {
      try {
        const [loadedSettings, loadedBackups, loadedTimezones] = await Promise.all([getBackupSettings(), listBackups(), getBackupTimezones()]);
        if (mounted) {
          setSettings(loadedSettings);
          setBackups(loadedBackups);
          setTimezones(loadedTimezones);
        }
      } catch (err) {
        if (mounted) setError(errorMessage(err, 'Failed to load backup configuration.'));
      } finally {
        if (mounted) setLoading(false);
      }
    };
    void load();
    return () => {
      mounted = false;
      if (pollTimer.current !== null) window.clearTimeout(pollTimer.current);
    };
  }, []);

  const timezoneGroups = useMemo(() => groupTimezones(timezones, settings?.timezone), [timezones, settings?.timezone]);

  const pollJob = useCallback(async (job: BackupJob) => {
    try {
      const next = await getBackupJob(job.job_id, job.kind === 'database_restore');
      setActiveJob(next);
      if (next.state === 'queued' || next.state === 'running') {
        pollTimer.current = window.setTimeout(() => void pollJob(next), pollIntervalMs);
      } else {
        setBusy(null);
        if (next.state === 'succeeded') {
          setMessage(next.kind === 'database_restore' ? 'Database restore completed and passed health validation.' : 'Backup operation completed successfully.');
          await refreshBackups();
          setSettings(await getBackupSettings());
        } else {
          setError([next.error_message, next.recovery_message].filter(Boolean).join(' '));
        }
      }
    } catch (err) {
      setBusy(null);
      setError(errorMessage(err, 'Failed to load backup job status.'));
    }
  }, [refreshBackups]);

  const beginPolling = (job: BackupJob) => {
    setActiveJob(job);
    pollTimer.current = window.setTimeout(() => void pollJob(job), pollIntervalMs);
  };

  const handleSave = async () => {
    if (!settings) return;
    setBusy('settings');
    setError(null);
    setMessage(null);
    try {
      setSettings(await saveBackupSettings(settings));
      setMessage('Backup settings saved and scheduler updated.');
    } catch (err) {
      setError(errorMessage(err, 'Failed to save backup settings.'));
    } finally {
      setBusy(null);
    }
  };

  const handleCreate = async (media = false) => {
    setBusy(media ? 'media' : 'backup');
    setError(null);
    setMessage(null);
    try {
      beginPolling(media ? await createMediaArchive() : await createDatabaseBackup());
    } catch (err) {
      setBusy(null);
      setError(errorMessage(err, 'Failed to start backup operation.'));
    }
  };

  const handleValidate = async (backup: DatabaseBackup) => {
    setBusy(`validate:${backup.backup_id}`);
    setError(null);
    try {
      const validated = await validateBackup(backup.backup_id);
      setBackups((current) => current.map((entry) => entry.backup_id === validated.backup_id ? validated : entry));
      setMessage(`${backup.filename} passed checksum and pg_restore validation.`);
    } catch (err) {
      setError(errorMessage(err, 'Backup validation failed.'));
    } finally {
      setBusy(null);
    }
  };

  const handleDelete = async (backup: DatabaseBackup) => {
    if (!window.confirm(`Delete ${backup.filename} and both sidecar files?`)) return;
    setBusy(`delete:${backup.backup_id}`);
    setError(null);
    try {
      await deleteBackup(backup.backup_id);
      setBackups((current) => current.filter((entry) => entry.backup_id !== backup.backup_id));
      setMessage('Backup set deleted.');
    } catch (err) {
      setError(errorMessage(err, 'Failed to delete backup.'));
    } finally {
      setBusy(null);
    }
  };

  const handleRestore = async () => {
    if (!restoreTarget || restoreConfirmation !== 'RESTORE FASTSELL') return;
    setBusy('restore');
    setError(null);
    setMessage(null);
    try {
      const job = await restoreBackup(restoreTarget.backup_id, restoreConfirmation);
      setRestoreTarget(null);
      setRestoreConfirmation('');
      beginPolling(job);
    } catch (err) {
      setBusy(null);
      setError(errorMessage(err, 'Failed to start restore.'));
    }
  };

  if (loading) return <Panel title="Backup & Restore"><p className="text-sm text-stone-300">Loading backup configuration…</p></Panel>;

  return (
    <div className="grid gap-6">
      <Panel title="Backup & Restore" eyebrow="Logical data protection">
        <p className="text-sm text-stone-300">FastSell creates PostgreSQL logical dumps under the fixed backup root. Configure an external backup system to copy <code>/srv/fastsell/backups</code> to separate storage.</p>
        {message ? <p className="mt-3 rounded border border-emerald-500/30 bg-emerald-500/10 p-3 text-sm text-emerald-100">{message}</p> : null}
        {error ? <p role="alert" className="mt-3 rounded border border-red-500/40 bg-red-950/25 p-3 text-sm text-red-100">{error}</p> : null}
      </Panel>

      {settings ? (
        <Panel title="Automatic database backups" eyebrow="In-process scheduler">
          <div className="grid gap-4">
            <label className="flex items-center gap-3 text-sm text-stone-200">
              <input aria-label="Enable automatic database backups" type="checkbox" checked={settings.automatic_enabled} onChange={(event) => setSettings({ ...settings, automatic_enabled: event.target.checked })} />
              Enable automatic database backups
            </label>
            <label className="grid gap-1 text-sm text-stone-300">Schedule preset
              <select aria-label="Schedule preset" value={settings.schedule_preset} onChange={(event) => setSettings({ ...settings, schedule_preset: event.target.value as BackupSettings['schedule_preset'] })} className="rounded border border-rack-steel/30 bg-graphite-950 px-3 py-2">
                <option value="daily">Daily at 2:00 AM</option>
                <option value="weekly">Weekly on Sunday at 2:00 AM</option>
                <option value="advanced">Advanced cron expression</option>
              </select>
            </label>
            {settings.schedule_preset === 'advanced' ? (
              <label className="grid gap-1 text-sm text-stone-300">Advanced cron expression
                <input aria-label="Advanced cron expression" value={settings.cron_expression} onChange={(event) => setSettings({ ...settings, cron_expression: event.target.value })} className="rounded border border-rack-steel/30 bg-graphite-950 px-3 py-2 font-mono" />
              </label>
            ) : null}
            <div className="grid gap-4 sm:grid-cols-2">
              <label className="grid gap-1 text-sm text-stone-300">Effective timezone (IANA)
                <select aria-label="Effective timezone" value={settings.timezone} onChange={(event) => setSettings({ ...settings, timezone: event.target.value })} className="rounded border border-rack-steel/30 bg-graphite-950 px-3 py-2">
                  <option value="UTC">UTC</option>
                  {timezoneGroups.map(([group, options]) => (
                    <optgroup key={group} label={group}>
                      {options.map((timezone) => <option key={timezone} value={timezone}>{timezone}</option>)}
                    </optgroup>
                  ))}
                </select>
              </label>
              <label className="grid gap-1 text-sm text-stone-300">Retention count
                <input aria-label="Retention count" type="number" min={1} max={365} value={settings.retention_count} onChange={(event) => setSettings({ ...settings, retention_count: Number(event.target.value) })} className="rounded border border-rack-steel/30 bg-graphite-950 px-3 py-2" />
              </label>
            </div>
            <Info label="Fixed host location" value={settings.host_location} mono />
            <Info label="Last backup attempt" value={formatDate(settings.last_attempt)} />
            <Info label="Last successful backup" value={formatDate(settings.last_success)} />
            <Info label="Last failure" value={settings.last_failure ? `${formatDate(settings.last_failure)}${settings.last_failure_message ? ` — ${settings.last_failure_message}` : ''}` : 'None'} />
            <button type="button" disabled={busy !== null} onClick={() => void handleSave()} className="w-fit rounded bg-copper-500 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50">{busy === 'settings' ? 'Saving…' : 'Save settings'}</button>
          </div>
        </Panel>
      ) : null}

      <div className="grid gap-6 lg:grid-cols-2">
        <Panel title="Manual database backup" eyebrow="Available even when scheduling is disabled">
          <button type="button" disabled={busy !== null} onClick={() => void handleCreate()} className="rounded bg-copper-500 px-4 py-2 text-sm font-semibold text-white disabled:opacity-50">Backup database now</button>
        </Panel>
        <Panel title="Manual media archive" eyebrow="Images, exports, and videos">
          <p className="mb-3 text-sm text-stone-400">Creates a tar.zst archive. Media restore is not included in this release.</p>
          <button type="button" disabled={busy !== null} onClick={() => void handleCreate(true)} className="rounded border border-copper-400/50 px-4 py-2 text-sm font-semibold text-amberline-100 disabled:opacity-50">Create media archive now</button>
        </Panel>
      </div>

      {activeJob ? (
        <Panel title="Active or latest job" eyebrow={activeJob.kind.replaceAll('_', ' ')}>
          <Info label="State" value={activeJob.state} />
          <Info label="Current phase" value={activeJob.phase} />
          {activeJob.pre_restore_backup_id ? <Info label="Pre-restore backup" value={activeJob.pre_restore_backup_id} mono /> : null}
          {activeJob.error_message ? <p role="alert" className="mt-3 text-sm text-red-100">{activeJob.error_message} {activeJob.recovery_message}</p> : null}
        </Panel>
      ) : null}

      <Panel title="Existing database backups" eyebrow="Completed logical dumps">
        {backups.length === 0 ? <p className="text-sm text-stone-400">No completed database backups found.</p> : (
          <div className="grid gap-4">
            {backups.map((backup) => (
              <div key={backup.backup_id} className="rounded border border-rack-steel/25 bg-black/10 p-4">
                <p className="break-all font-mono text-xs text-stone-200">{backup.filename}</p>
                <div className="mt-3 grid gap-2 text-sm text-stone-300 sm:grid-cols-3">
                  <span>Created: {formatDate(backup.created_time)}</span><span>Size: {formatBytes(backup.size)}</span><span>Source: {backup.source}</span>
                  <span>FastSell: {backup.fastsell_version}</span><span>PostgreSQL: {backup.postgresql_version}</span><span>Schema: {backup.schema_version}</span>
                  <span>Validation: {backup.validation_status}</span>
                </div>
                <div className="mt-4 flex flex-wrap gap-2">
                  <Action disabled={busy !== null} onClick={() => void handleValidate(backup)}>Validate</Action>
                  <Action disabled={busy !== null} onClick={() => { setRestoreTarget(backup); setRestoreConfirmation(''); }}>Restore</Action>
                  <Action disabled={busy !== null} danger onClick={() => void handleDelete(backup)}>Delete</Action>
                </div>
              </div>
            ))}
          </div>
        )}
      </Panel>

      {restoreTarget ? (
        <div role="dialog" aria-modal="true" aria-label="Restore database backup" className="fixed inset-0 z-50 grid place-items-center bg-black/75 p-4">
          <div className="w-full max-w-xl rounded-lg border border-red-500/45 bg-graphite-950 p-6 shadow-2xl">
            <h2 className="text-xl font-semibold text-red-100">Destructive database restore</h2>
            <p className="mt-3 text-sm text-stone-300">FastSell will enter maintenance mode and replace the current database contents. A validated pre-restore backup is created automatically before changes begin.</p>
            <p className="mt-3 break-all font-mono text-xs text-stone-400">{restoreTarget.filename}</p>
            <label className="mt-4 grid gap-2 text-sm text-stone-200">Type RESTORE FASTSELL to continue
              <input aria-label="Restore confirmation" value={restoreConfirmation} onChange={(event) => setRestoreConfirmation(event.target.value)} className="rounded border border-red-500/40 bg-black/30 px-3 py-2 font-mono" />
            </label>
            <div className="mt-5 flex justify-end gap-3">
              <button type="button" onClick={() => setRestoreTarget(null)} className="rounded border border-rack-steel/30 px-4 py-2 text-sm">Cancel</button>
              <button type="button" disabled={restoreConfirmation !== 'RESTORE FASTSELL' || busy !== null} onClick={() => void handleRestore()} className="rounded bg-red-700 px-4 py-2 text-sm font-semibold text-white disabled:opacity-40">Restore database</button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  );
}

function groupTimezones(available: string[], saved?: string): Array<[string, string[]]> {
  const selectable = new Set(available.filter((timezone) => timezone !== 'UTC'));
  if (saved && saved !== 'UTC') selectable.add(saved);
  const groups = new Map<string, string[]>();
  for (const timezone of selectable) {
    const separator = timezone.indexOf('/');
    const group = separator > 0 ? timezone.slice(0, separator) : 'Other';
    const options = groups.get(group) ?? [];
    options.push(timezone);
    groups.set(group, options);
  }
  return [...groups.entries()]
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([group, options]) => [group, options.sort((left, right) => left.localeCompare(right))]);
}

function Action({ children, disabled, danger = false, onClick }: { children: ReactNode; disabled: boolean; danger?: boolean; onClick: () => void }) {
  return <button type="button" disabled={disabled} onClick={onClick} className={`rounded border px-3 py-1.5 text-xs font-semibold disabled:opacity-40 ${danger ? 'border-red-500/40 text-red-100' : 'border-rack-steel/30 text-stone-200'}`}>{children}</button>;
}

function Info({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return <div className="flex flex-col gap-1 border-b border-rack-steel/15 pb-2 text-sm sm:flex-row sm:justify-between"><span className="text-stone-400">{label}</span><span className={`${mono ? 'font-mono text-xs' : ''} text-stone-200 sm:text-right`}>{value}</span></div>;
}

function formatDate(value: string | null): string {
  return value ? new Date(value).toLocaleString() : 'Never';
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError || error instanceof Error) return error.message || fallback;
  return fallback;
}
