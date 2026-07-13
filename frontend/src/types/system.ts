export type SystemHealthStatus = 'ok' | 'warning' | 'failed' | 'unknown';

export interface SystemVersionResponse {
  installed_version: string;
  latest_version: string | null;
  update_available: boolean;
}

export interface SystemHealthAlert {
  severity: 'warning' | 'failed';
  area: string;
  message: string;
}

export interface SystemAPIHealth {
  status: SystemHealthStatus;
  uptime_seconds: number;
  server_time: string;
  data_root: string;
  image_root: string;
  intake_dir: string;
}

export interface SystemDatabaseHealth {
  status: SystemHealthStatus;
  reachable: boolean;
  migration_version: number | null;
  migration_dirty: boolean | null;
  database_size_bytes: number | null;
  container_count: number;
  item_count: number;
  image_asset_count: number;
  upload_session_count: number;
  upload_group_count: number;
}

export interface SystemStorageHealth {
  status: SystemHealthStatus;
  path: string;
  total_bytes: number;
  free_bytes: number;
  used_bytes: number;
  used_percent: number;
}

export interface SystemPathCheck {
  path: string;
  status: SystemHealthStatus;
  exists: boolean;
  is_directory: boolean;
  readable: boolean;
  writable: boolean;
  message: string;
}

export interface SystemPathsHealth {
  status: SystemHealthStatus;
  paths: SystemPathCheck[];
}

export interface SystemIntakeHealth {
  status: SystemHealthStatus;
  pending_or_uploaded_image_count: number;
  processing_image_count: number;
  processed_image_count: number;
  failed_image_count: number;
  stuck_processing_image_count: number;
  oldest_pending_datetime: string | null;
  latest_processed_datetime: string | null;
  upload_session_status_counts?: Record<string, number>;
}

export interface SystemAIHealth {
  status: SystemHealthStatus;
  ai_assist_enabled: boolean;
  active_provider_id: string | null;
  active_provider_name: string | null;
  active_provider_type: string | null;
  active_model_name: string | null;
  vision_enabled: boolean | null;
  last_test_status: string | null;
  last_test_datetime: string | null;
  last_error_message: string | null;
}

export interface SystemSimpleHealth {
  status: SystemHealthStatus;
  hosting_mode?: string;
  public_url?: string;
  message: string;
}

export interface SystemDockerService {
  service_name: string;
  container_name: string;
  image: string;
  state: string;
  health: string;
  restart_count: number;
  started_at: string | null;
  finished_at: string | null;
  ports: string[];
  status: SystemHealthStatus;
}

export interface SystemDockerHealth {
  status: SystemHealthStatus;
  message?: string;
  generated_datetime?: string | null;
  services?: SystemDockerService[];
  alerts?: SystemHealthAlert[];
}

export interface AdminSystemHealthResponse {
  overall_status: SystemHealthStatus;
  generated_datetime: string;
  api: SystemAPIHealth;
  database: SystemDatabaseHealth;
  storage: SystemStorageHealth;
  paths: SystemPathsHealth;
  intake: SystemIntakeHealth;
  ai: SystemAIHealth;
  frontend: SystemSimpleHealth;
  docker: SystemDockerHealth;
  alerts: SystemHealthAlert[];
}
