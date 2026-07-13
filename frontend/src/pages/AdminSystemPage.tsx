import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { getAdminSystemHealth } from '../api/system';
import { Panel } from '../components/Panel';
import type {
  AdminSystemHealthResponse,
  SystemDockerService,
  SystemHealthAlert,
  SystemHealthStatus,
  SystemPathCheck,
} from '../types/system';

export function AdminSystemPage() {
  const [health, setHealth] = useState<AdminSystemHealthResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let isMounted = true;

    const load = async () => {
      setIsLoading(true);
      setError(null);

      try {
        const response = await getAdminSystemHealth();
        if (isMounted) {
          setHealth(response);
        }
      } catch (loadError) {
        console.error('Failed to load system health', loadError);
        if (isMounted) {
          setError(errorMessage(loadError, 'Failed to load system health.'));
        }
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    };

    void load();
    return () => {
      isMounted = false;
    };
  }, []);

  const handleRefresh = async () => {
    setIsLoading(true);
    setError(null);

    try {
      const response = await getAdminSystemHealth();
      setHealth(response);
    } catch (loadError) {
      console.error('Failed to refresh system health', loadError);
      setError(errorMessage(loadError, 'Failed to refresh system health.'));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel
        title="Admin / System"
        eyebrow="Read-only FastSell runtime health"
        action={(
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
        )}
      >
        <div className="grid gap-3">
          <p className="text-sm text-stone-300">Read-only FastSell runtime health.</p>
          <p className="text-xs text-stone-500">No Docker controls, no log streaming, and no external AI provider calls are performed from this page.</p>
          {health ? (
            <p className="text-xs text-stone-500">
              Generated: {new Date(health.generated_datetime).toLocaleString()}
            </p>
          ) : null}
        </div>
      </Panel>

      {error ? (
        <Panel title="Load Error" eyebrow="System health unavailable">
          <div className="grid gap-3">
            <p className="text-sm text-red-200">{error}</p>
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

      {!health && isLoading ? (
        <Panel title="Loading System Health" eyebrow="Admin / System">
          <p className="text-sm text-stone-300">Loading FastSell runtime health...</p>
        </Panel>
      ) : null}

      {health ? (
        <>
          <Panel title="Overall Status" eyebrow="FastSell runtime">
            <div className="grid gap-4 lg:grid-cols-[auto_1fr] lg:items-center">
              <StatusBadge status={health.overall_status} large />
              <div className="grid gap-2 text-sm text-stone-300">
                <p>Use this page to answer whether FastSell is healthy enough to use right now.</p>
                <div className="flex flex-wrap gap-2 text-xs text-stone-400">
                  <Link className="rounded-md border border-rack-steel/25 px-2 py-1 hover:border-amberline-400/35 hover:text-stone-200" to="/admin/containers">Containers</Link>
                  <Link className="rounded-md border border-rack-steel/25 px-2 py-1 hover:border-amberline-400/35 hover:text-stone-200" to="/admin/metrics">Metrics</Link>
                  <Link className="rounded-md border border-rack-steel/25 px-2 py-1 hover:border-amberline-400/35 hover:text-stone-200" to="/admin/ai">AI</Link>
                </div>
              </div>
            </div>
          </Panel>

          <Panel title="Alerts" eyebrow="Warnings and failures">
            {health.alerts.length === 0 ? (
              <p className="text-sm text-stone-300">No active warnings or failures were reported.</p>
            ) : (
              <div className="grid gap-3">
                {health.alerts.map((alert, index) => (
                  <AlertRow key={`${alert.area}-${alert.message}-${index}`} alert={alert} />
                ))}
              </div>
            )}
          </Panel>

          <div className="grid gap-6 xl:grid-cols-2">
            <Panel title="API" eyebrow="Application runtime">
              <div className="grid gap-3">
                <StatusLine status={health.api.status} />
                <InfoRow label="Uptime" value={formatDuration(health.api.uptime_seconds)} />
                <InfoRow label="Server time" value={formatDateTime(health.api.server_time)} />
                <InfoRow label="Data root" value={health.api.data_root} mono />
                <InfoRow label="Image root" value={health.api.image_root} mono />
                <InfoRow label="Intake dir" value={health.api.intake_dir} mono />
              </div>
            </Panel>

            <Panel title="Database" eyebrow="Reachability and schema state">
              <div className="grid gap-3">
                <StatusLine status={health.database.status} />
                <InfoRow label="Reachable" value={health.database.reachable ? 'Yes' : 'No'} />
                <InfoRow label="Migration version" value={health.database.migration_version == null ? 'Unavailable' : String(health.database.migration_version)} />
                <InfoRow label="Migration dirty" value={health.database.migration_dirty == null ? 'Unavailable' : (health.database.migration_dirty ? 'Yes' : 'No')} />
                <InfoRow label="Database size" value={formatBytes(health.database.database_size_bytes)} />
                <InfoRow label="Containers" value={formatCount(health.database.container_count)} />
                <InfoRow label="Items" value={formatCount(health.database.item_count)} />
                <InfoRow label="Image assets" value={formatCount(health.database.image_asset_count)} />
                <InfoRow label="Upload sessions" value={formatCount(health.database.upload_session_count)} />
                <InfoRow label="Upload groups" value={formatCount(health.database.upload_group_count)} />
              </div>
            </Panel>

            <Panel title="Storage" eyebrow="Mounted data root">
              <div className="grid gap-3">
                <StatusLine status={health.storage.status} />
                <InfoRow label="Path" value={health.storage.path} mono />
                <InfoRow label="Total" value={formatBytes(health.storage.total_bytes)} />
                <InfoRow label="Used" value={formatBytes(health.storage.used_bytes)} />
                <InfoRow label="Free" value={formatBytes(health.storage.free_bytes)} />
                <InfoRow label="Used percent" value={Number.isFinite(health.storage.used_percent) ? `${health.storage.used_percent.toFixed(1)}%` : 'Unavailable'} />
              </div>
            </Panel>

            <Panel title="Paths" eyebrow="Required writable directories">
              <div className="grid gap-3">
                <StatusLine status={health.paths.status} />
                {health.paths.paths.map((entry) => (
                  <PathRow key={entry.path} entry={entry} />
                ))}
              </div>
            </Panel>

            <Panel title="Intake" eyebrow="Image processing state">
              <div className="grid gap-3">
                <StatusLine status={health.intake.status} />
                <InfoRow label="Pending or uploaded" value={formatCount(health.intake.pending_or_uploaded_image_count)} />
                <InfoRow label="Processing" value={formatCount(health.intake.processing_image_count)} />
                <InfoRow label="Processed" value={formatCount(health.intake.processed_image_count)} />
                <InfoRow label="Failed" value={formatCount(health.intake.failed_image_count)} />
                <InfoRow label="Stuck processing" value={formatCount(health.intake.stuck_processing_image_count)} />
                <InfoRow label="Oldest pending" value={formatDateTime(health.intake.oldest_pending_datetime)} />
                <InfoRow label="Latest processed" value={formatDateTime(health.intake.latest_processed_datetime)} />
                <div className="rounded-md border border-rack-steel/18 bg-black/10 p-3">
                  <p className="text-xs font-semibold uppercase tracking-[0.18em] text-rack-glass">Upload session status counts</p>
                  <div className="mt-2 flex flex-wrap gap-2 text-xs text-stone-300">
                    {Object.entries(health.intake.upload_session_status_counts ?? {}).length === 0 ? (
                      <span>No upload session status counts available.</span>
                    ) : (
                      Object.entries(health.intake.upload_session_status_counts ?? {}).map(([status, count]) => (
                        <span key={status} className="rounded-full border border-rack-steel/20 bg-rack-soot/70 px-2 py-1">
                          {status}: {count}
                        </span>
                      ))
                    )}
                  </div>
                </div>
              </div>
            </Panel>

            <Panel title="AI" eyebrow="Saved provider state only">
              <div className="grid gap-3">
                <div className="flex items-center justify-between gap-3">
                  <StatusLine status={health.ai.status} />
                  <Link className="text-xs font-semibold text-amberline-200 hover:text-amberline-100" to="/admin/ai">Open AI config</Link>
                </div>
                <InfoRow label="AI assist enabled" value={health.ai.ai_assist_enabled ? 'Yes' : 'No'} />
                <InfoRow label="Provider" value={health.ai.active_provider_name ?? 'No active provider'} />
                <InfoRow label="Provider type" value={health.ai.active_provider_type ?? 'Unavailable'} />
                <InfoRow label="Model" value={health.ai.active_model_name ?? 'Unavailable'} />
                <InfoRow label="Vision enabled" value={health.ai.vision_enabled == null ? 'Unavailable' : (health.ai.vision_enabled ? 'Yes' : 'No')} />
                <InfoRow label="Last test status" value={health.ai.last_test_status ?? 'Not tested'} />
                <InfoRow label="Last test datetime" value={formatDateTime(health.ai.last_test_datetime)} />
                <InfoRow label="Last error" value={health.ai.last_error_message ?? 'None'} />
              </div>
            </Panel>

            <Panel title="Frontend" eyebrow="Hosting status">
              <div className="grid gap-3">
                <StatusLine status={health.frontend.status} />
                <InfoRow label="Hosting mode" value={health.frontend.hosting_mode ?? 'Unavailable'} />
                <InfoRow label="Public URL" value={health.frontend.public_url ?? 'Unavailable'} />
                <p className="text-sm text-stone-300">{health.frontend.message}</p>
              </div>
            </Panel>

            <Panel title="Docker" eyebrow="Read-only container status">
              <div className="grid gap-3">
                <StatusLine status={health.docker.status} />
                <p className="text-sm text-stone-300">{health.docker.message ?? 'Docker health information is unavailable.'}</p>
                {health.docker.generated_datetime ? (
                  <InfoRow label="Generated" value={formatDateTime(health.docker.generated_datetime)} />
                ) : null}
                {(health.docker.services ?? []).length > 0 ? (
                  <div className="grid gap-3">
                    {(health.docker.services ?? []).map((service) => (
                      <DockerServiceRow key={service.service_name} service={service} />
                    ))}
                  </div>
                ) : null}
                {(health.docker.alerts ?? []).length > 0 ? (
                  <div className="grid gap-3">
                    {(health.docker.alerts ?? []).map((alert, index) => (
                      <AlertRow key={`docker-${alert.area}-${alert.message}-${index}`} alert={alert} />
                    ))}
                  </div>
                ) : null}
              </div>
            </Panel>
          </div>
        </>
      ) : null}
    </div>
  );
}

function AlertRow({ alert }: { alert: SystemHealthAlert }) {
  return (
    <div className={`rounded-md border p-3 ${alert.severity === 'failed' ? 'border-red-500/40 bg-red-950/20' : 'border-amberline-500/30 bg-copper-500/10'}`}>
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={alert.severity} />
        <span className="text-xs font-semibold uppercase tracking-[0.18em] text-stone-400">{alert.area}</span>
      </div>
      <p className="mt-2 text-sm text-stone-200">{alert.message}</p>
    </div>
  );
}

function StatusLine({ status }: { status: SystemHealthStatus }) {
  return <div><StatusBadge status={status} /></div>;
}

function PathRow({ entry }: { entry: SystemPathCheck }) {
  return (
    <div className="rounded-md border border-rack-steel/18 bg-black/10 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={entry.status} />
        <span className="truncate text-xs font-semibold text-stone-200">{entry.path}</span>
      </div>
      <div className="mt-2 grid gap-2 text-xs text-stone-400 sm:grid-cols-4">
        <span>Exists: {entry.exists ? 'Yes' : 'No'}</span>
        <span>Directory: {entry.is_directory ? 'Yes' : 'No'}</span>
        <span>Readable: {entry.readable ? 'Yes' : 'No'}</span>
        <span>Writable: {entry.writable ? 'Yes' : 'No'}</span>
      </div>
      <p className="mt-2 text-xs text-stone-500">{entry.message}</p>
    </div>
  );
}

function DockerServiceRow({ service }: { service: SystemDockerService }) {
  const ports = service.ports ?? [];

  return (
    <div className="rounded-md border border-rack-steel/18 bg-black/10 p-3">
      <div className="flex flex-wrap items-center gap-2">
        <StatusBadge status={service.status} />
        <span className="text-sm font-semibold text-stone-100">{service.service_name}</span>
        <span className="text-xs text-stone-500">{service.container_name || 'No container name'}</span>
      </div>
      <div className="mt-3 grid gap-2 text-xs text-stone-300 sm:grid-cols-2">
        <span>State: {service.state}</span>
        <span>Health: {service.health}</span>
        <span>Restart count: {service.restart_count}</span>
        <span className="min-w-0 [overflow-wrap:anywhere]">Image: {service.image || 'Unavailable'}</span>
        <span>Started: {formatDateTime(service.started_at)}</span>
        <span>Finished: {formatDateTime(service.finished_at)}</span>
      </div>
      <p className="mt-2 text-xs text-stone-500">
        Ports: {ports.length > 0 ? ports.join(', ') : 'No host ports published'}
      </p>
    </div>
  );
}

function StatusBadge({ status, large = false }: { status: SystemHealthStatus; large?: boolean }) {
  const label = status === 'ok' ? 'OK' : status === 'warning' ? 'Warning' : status === 'failed' ? 'Failed' : 'Unknown';
  const className =
    status === 'ok'
      ? 'border-emerald-500/35 bg-emerald-500/12 text-emerald-100'
      : status === 'warning'
        ? 'border-amberline-500/35 bg-copper-500/12 text-amberline-100'
        : status === 'failed'
          ? 'border-red-500/40 bg-red-950/25 text-red-100'
          : 'border-rack-steel/25 bg-rack-soot/70 text-stone-300';

  return (
    <span className={`inline-flex items-center rounded-full border px-3 py-1 font-semibold uppercase tracking-[0.18em] ${large ? 'text-sm' : 'text-[11px]'} ${className}`}>
      {label}
    </span>
  );
}

function InfoRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-1 border-b border-rack-steel/15 pb-2 text-sm last:border-b-0 last:pb-0 sm:flex-row sm:items-start sm:justify-between sm:gap-4">
      <span className="text-stone-400">{label}</span>
      <span className={`${mono ? 'font-mono text-xs' : ''} text-stone-200 sm:text-right`}>{value}</span>
    </div>
  );
}

function formatCount(value: number): string {
  return new Intl.NumberFormat().format(value);
}

function formatBytes(value: number | null | undefined): string {
  if (value == null || Number.isNaN(value)) {
    return 'Unavailable';
  }

  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  let size = value;
  let unitIndex = 0;
  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }

  return `${size.toFixed(size >= 10 || unitIndex === 0 ? 0 : 1)} ${units[unitIndex]}`;
}

function formatDateTime(value: string | null): string {
  if (!value) {
    return 'Unavailable';
  }
  return new Date(value).toLocaleString();
}

function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return 'Unavailable';
  }

  const days = Math.floor(seconds / 86400);
  const hours = Math.floor((seconds % 86400) / 3600);
  const minutes = Math.floor((seconds % 3600) / 60);
  if (days > 0) {
    return `${days}d ${hours}h ${minutes}m`;
  }
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m`;
  }
  return `${Math.floor(seconds)}s`;
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.message || fallback;
  }
  if (error instanceof Error && error.message.trim()) {
    return error.message;
  }
  return fallback;
}
