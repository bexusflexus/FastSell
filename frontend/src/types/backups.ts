export type BackupSchedulePreset = 'daily' | 'weekly' | 'advanced';
export type BackupJobState = 'queued' | 'running' | 'succeeded' | 'failed';

export interface BackupSettings {
  automatic_enabled: boolean;
  schedule_preset: BackupSchedulePreset;
  cron_expression: string;
  timezone: string;
  retention_count: number;
  host_location: string;
  last_attempt: string | null;
  last_success: string | null;
  last_failure: string | null;
  last_failure_message: string | null;
}

export interface DatabaseBackup {
  backup_id: string;
  filename: string;
  created_time: string;
  fastsell_version: string;
  postgresql_version: number;
  schema_version: number;
  size: number;
  validation_status: string;
  source: 'manual' | 'scheduled' | 'pre_restore';
}

export interface BackupJob {
  job_id: string;
  kind: 'database_backup' | 'database_restore' | 'media_archive';
  state: BackupJobState;
  phase: string;
  source?: string;
  backup_id?: string;
  pre_restore_backup_id?: string;
  error_message?: string;
  recovery_message?: string;
  created_at: string;
  started_at?: string;
  completed_at?: string;
}
