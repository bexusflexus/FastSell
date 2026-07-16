import type { BackupJob, BackupSettings, DatabaseBackup } from '../types/backups';
import { apiFetch, readJson } from './client';

export async function getBackupSettings(): Promise<BackupSettings> {
  return readJson(await apiFetch('/api/admin/backup-settings'));
}

export async function getBackupTimezones(): Promise<string[]> {
  const response = await readJson<{ timezones: string[] }>(await apiFetch('/api/admin/backup/timezones'));
  return response.timezones;
}

export async function saveBackupSettings(settings: BackupSettings): Promise<BackupSettings> {
  return readJson(await apiFetch('/api/admin/backup-settings', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  }));
}

export async function listBackups(): Promise<DatabaseBackup[]> {
  const response = await readJson<{ backups: DatabaseBackup[] }>(await apiFetch('/api/admin/backups'));
  return response.backups;
}

export async function createDatabaseBackup(): Promise<BackupJob> {
  return readJson(await apiFetch('/api/admin/backups', { method: 'POST' }));
}

export async function createMediaArchive(): Promise<BackupJob> {
  return readJson(await apiFetch('/api/admin/backups/media', { method: 'POST' }));
}

export async function getBackupJob(jobId: string, restore = false): Promise<BackupJob> {
  const path = restore ? `/api/admin/restores/jobs/${encodeURIComponent(jobId)}` : `/api/admin/backups/jobs/${encodeURIComponent(jobId)}`;
  return readJson(await apiFetch(path));
}

export async function validateBackup(backupId: string): Promise<DatabaseBackup> {
  return readJson(await apiFetch(`/api/admin/backups/${encodeURIComponent(backupId)}/validate`, { method: 'POST' }));
}

export async function deleteBackup(backupId: string): Promise<void> {
  await apiFetch(`/api/admin/backups/${encodeURIComponent(backupId)}`, { method: 'DELETE' });
}

export async function restoreBackup(backupId: string, confirmation: string): Promise<BackupJob> {
  return readJson(await apiFetch(`/api/admin/backups/${encodeURIComponent(backupId)}/restore`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ confirmation }),
  }));
}
