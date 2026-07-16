// @vitest-environment jsdom

import '@testing-library/jest-dom/vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as backupApi from '../api/backups';
import { ApiError } from '../api/client';
import type { BackupJob, BackupSettings, DatabaseBackup } from '../types/backups';
import { AdminBackupPage } from './AdminBackupPage';

vi.mock('../api/backups', () => ({
  getBackupSettings: vi.fn(), saveBackupSettings: vi.fn(), listBackups: vi.fn(),
  getBackupTimezones: vi.fn(),
  createDatabaseBackup: vi.fn(), createMediaArchive: vi.fn(), getBackupJob: vi.fn(),
  validateBackup: vi.fn(), deleteBackup: vi.fn(), restoreBackup: vi.fn(),
}));

const settings: BackupSettings = {
  automatic_enabled: true, schedule_preset: 'daily', cron_expression: '0 2 * * *', timezone: 'UTC',
  retention_count: 14, host_location: '/srv/fastsell/backups/database', last_attempt: null,
  last_success: null, last_failure: null, last_failure_message: null,
};

const existing: DatabaseBackup = {
  backup_id: 'fastsell-db-backup-20260714T020000-v0.1.4-pg16.dump',
  filename: 'fastsell-db-backup-20260714T020000-v0.1.4-pg16.dump',
  created_time: '2026-07-14T02:00:00Z', fastsell_version: 'v0.1.4', postgresql_version: 16,
  schema_version: 3, size: 2048, validation_status: 'checksum_valid', source: 'scheduled',
};

const queued: BackupJob = {
  job_id: 'job-1', kind: 'database_backup', state: 'queued', phase: 'queued', created_at: '2026-07-14T03:00:00Z',
};

beforeEach(() => {
  vi.mocked(backupApi.getBackupSettings).mockResolvedValue({ ...settings });
  vi.mocked(backupApi.getBackupTimezones).mockResolvedValue(['UTC', 'Africa/Abidjan', 'America/Chicago', 'America/Denver']);
  vi.mocked(backupApi.listBackups).mockResolvedValue([existing]);
  vi.mocked(backupApi.saveBackupSettings).mockImplementation(async (value) => value);
  vi.mocked(backupApi.createDatabaseBackup).mockResolvedValue(queued);
  vi.mocked(backupApi.createMediaArchive).mockResolvedValue({ ...queued, kind: 'media_archive' });
  vi.mocked(backupApi.getBackupJob).mockResolvedValue({ ...queued, state: 'succeeded', phase: 'complete' });
  vi.mocked(backupApi.validateBackup).mockResolvedValue({ ...existing, validation_status: 'valid' });
  vi.mocked(backupApi.deleteBackup).mockResolvedValue();
  vi.mocked(backupApi.restoreBackup).mockResolvedValue({ ...queued, kind: 'database_restore' });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

describe('AdminBackupPage', () => {
  it('loads settings and renders backup history', async () => {
    render(<AdminBackupPage />);
    expect(await screen.findByLabelText('Enable automatic database backups')).toBeChecked();
    expect(screen.getByText(existing.filename)).toBeInTheDocument();
    expect(screen.getByText('/srv/fastsell/backups/database')).toBeInTheDocument();
    expect(screen.getByText('Source: scheduled')).toBeInTheDocument();
  });

  it('loads grouped timezones, selects the saved zone, and saves the exact identifier', async () => {
    vi.mocked(backupApi.getBackupSettings).mockResolvedValueOnce({ ...settings, timezone: 'America/Chicago' });
    render(<AdminBackupPage />);
    const select = await screen.findByLabelText('Effective timezone');
    expect(select).toBeInstanceOf(HTMLSelectElement);
    expect(select).toHaveValue('America/Chicago');
    expect(screen.getByRole('option', { name: 'UTC' })).toBeInTheDocument();
    expect(screen.getAllByRole('option').filter((option) => (option as HTMLOptionElement).value === 'UTC')).toHaveLength(1);
    expect(screen.getByRole('group', { name: 'Africa' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'America' })).toBeInTheDocument();
    fireEvent.change(select, { target: { value: 'America/Denver' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }));
    await waitFor(() => expect(backupApi.saveBackupSettings).toHaveBeenCalledWith(expect.objectContaining({ timezone: 'America/Denver' })));
  });

  it('keeps a saved timezone alias that is absent from zone1970.tab selectable', async () => {
    vi.mocked(backupApi.getBackupSettings).mockResolvedValueOnce({ ...settings, timezone: 'US/Central' });
    render(<AdminBackupPage />);
    const select = await screen.findByLabelText('Effective timezone');
    expect(select).toHaveValue('US/Central');
    expect(screen.getByRole('option', { name: 'US/Central' })).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'US' })).toBeInTheDocument();
  });

  it('displays timezone list load failures', async () => {
    vi.mocked(backupApi.getBackupTimezones).mockRejectedValueOnce(new ApiError('server timezone data is unavailable', 500));
    render(<AdminBackupPage />);
    expect(await screen.findByRole('alert')).toHaveTextContent('server timezone data is unavailable');
    expect(screen.queryByLabelText('Effective timezone')).not.toBeInTheDocument();
  });

  it('disables scheduling, supports advanced cron, and displays server validation errors', async () => {
    vi.mocked(backupApi.saveBackupSettings).mockRejectedValueOnce(new ApiError('cron_expression must be a valid five-field cron expression', 400));
    render(<AdminBackupPage />);
    const toggle = await screen.findByLabelText('Enable automatic database backups');
    fireEvent.click(toggle);
    fireEvent.change(screen.getByLabelText('Schedule preset'), { target: { value: 'advanced' } });
    fireEvent.change(screen.getByLabelText('Advanced cron expression'), { target: { value: 'bad cron' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save settings' }));
    await waitFor(() => expect(backupApi.saveBackupSettings).toHaveBeenCalledWith(expect.objectContaining({ automatic_enabled: false, schedule_preset: 'advanced', cron_expression: 'bad cron' })));
    expect(await screen.findByRole('alert')).toHaveTextContent('valid five-field cron expression');
  });

  it('starts a manual backup and polls its job to completion', async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    render(<AdminBackupPage />);
    fireEvent.click(await screen.findByRole('button', { name: 'Backup database now' }));
    await waitFor(() => expect(backupApi.createDatabaseBackup).toHaveBeenCalled());
    await vi.advanceTimersByTimeAsync(1_600);
    await waitFor(() => expect(backupApi.getBackupJob).toHaveBeenCalledWith('job-1', false));
    expect(await screen.findByText('complete')).toBeInTheDocument();
    vi.useRealTimers();
  });

  it('validates, confirms deletion, and requires typed restore confirmation', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(<AdminBackupPage />);
    await screen.findByText(existing.filename);

    fireEvent.click(screen.getByRole('button', { name: 'Validate' }));
    await waitFor(() => expect(backupApi.validateBackup).toHaveBeenCalledWith(existing.backup_id));
    expect(await screen.findByText('Validation: valid')).toBeInTheDocument();

    fireEvent.click(screen.getByRole('button', { name: 'Restore' }));
    const restoreButton = screen.getByRole('button', { name: 'Restore database' });
    expect(restoreButton).toBeDisabled();
    fireEvent.change(screen.getByLabelText('Restore confirmation'), { target: { value: 'RESTORE FASTSELL' } });
    expect(restoreButton).toBeEnabled();
    fireEvent.click(restoreButton);
    await waitFor(() => expect(backupApi.restoreBackup).toHaveBeenCalledWith(existing.backup_id, 'RESTORE FASTSELL'));

    // Reload for delete because the restore dialog closes asynchronously.
    cleanup();
    render(<AdminBackupPage />);
    await screen.findByText(existing.filename);
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    await waitFor(() => expect(backupApi.deleteBackup).toHaveBeenCalledWith(existing.backup_id));
  });
});
