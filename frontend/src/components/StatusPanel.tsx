import type { PollingState, UploadSessionStatusResponse, UploadState } from '../types/upload';

interface StatusPanelProps {
  status: UploadState;
  session: UploadSessionStatusResponse | null;
  error: string | null;
  pollingState: PollingState;
  pollingError: string | null;
  onStopPolling: () => void;
  onReset: () => void;
}

const terminalStatuses = new Set(['processed', 'failed', 'completed_with_errors']);

export function StatusPanel({ status, session, error, pollingState, pollingError, onStopPolling, onReset }: StatusPanelProps) {
  if (status === 'uploading') {
    return (
      <div className="rounded-md border border-copper-400/35 bg-copper-950/20 px-4 py-4 text-sm text-amberline-100">
        Uploading files to the FastSell API...
      </div>
    );
  }

  if (status === 'error') {
    return (
      <div className="rounded-md border border-red-400/35 bg-red-950/30 px-4 py-4 text-sm text-red-100">
        {error ?? 'Upload failed.'}
      </div>
    );
  }

  if (!session) {
    return (
      <div className="rounded-md border border-rack-steel/28 bg-rack-soot/70 px-4 py-4 text-sm text-stone-400">
        Waiting for a real upload.
      </div>
    );
  }

  const fileCount = session.groups.reduce((count, group) => count + group.files.length, 0);
  const tone = toneForStatus(session.status);
  const isTerminal = terminalStatuses.has(session.status);

  return (
    <div className={`rounded-md border px-4 py-4 ${tone.container}`}>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-center gap-2">
          <span className={`h-2.5 w-2.5 rounded-full ${tone.light}`} />
          <p className={`font-semibold ${tone.text}`}>{labelForStatus(session.status)}</p>
        </div>
        <div className="flex flex-wrap gap-2">
          {pollingState === 'polling' ? (
            <button
              type="button"
              onClick={onStopPolling}
              className="rounded-md border border-rack-steel/35 px-3 py-2 text-xs font-medium text-stone-200 transition hover:bg-rack-steel/12"
            >
              Stop polling
            </button>
          ) : null}
          {isTerminal ? (
            <button
              type="button"
              onClick={onReset}
              className="rounded-md border border-copper-500/35 px-3 py-2 text-xs font-medium text-amberline-100 transition hover:bg-copper-500/12"
            >
              New upload
            </button>
          ) : null}
        </div>
      </div>

      <dl className="mt-3 grid gap-2 text-sm text-stone-300 sm:grid-cols-2">
        <div>
          <dt className="text-stone-500">Session</dt>
          <dd className="break-all">{session.upload_session_id}</dd>
        </div>
        <div>
          <dt className="text-stone-500">Status</dt>
          <dd>{session.status}</dd>
        </div>
        <div>
          <dt className="text-stone-500">Groups</dt>
          <dd>{session.groups.length}</dd>
        </div>
        <div>
          <dt className="text-stone-500">Files</dt>
          <dd>{fileCount}</dd>
        </div>
      </dl>

      {pollingState === 'polling' ? <p className="mt-3 text-sm text-amberline-100">Polling worker status every 2 seconds.</p> : null}
      {pollingState === 'stopped' ? <p className="mt-3 text-sm text-stone-300">Status polling stopped.</p> : null}
      {pollingState === 'timeout' ? <p className="mt-3 text-sm text-amberline-100">Status polling timed out. Last known status is shown.</p> : null}
      {pollingError ? <p className="mt-3 text-sm text-red-100">{pollingError}</p> : null}

      <div className="mt-4 grid gap-3">
        {session.groups.map((group) => (
          <div key={group.upload_group_id} className="rounded-md border border-rack-steel/28 bg-rack-soot/70 p-3">
            <div className="flex flex-col gap-1 sm:flex-row sm:items-center sm:justify-between">
              <p className="font-semibold text-stone-100">{group.title || group.client_group_id || 'Untitled group'}</p>
              <p className="text-xs text-stone-500">{group.client_group_id}</p>
            </div>
            <div className="mt-3 grid gap-2">
              {group.files.map((file) => {
                const fileTone = toneForStatus(file.status);
                return (
                  <div key={file.image_asset_id} className="rounded border border-rack-steel/24 bg-graphite-950/60 p-3">
                    <div className="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
                      <div className="min-w-0">
                        <p className="truncate text-sm font-medium text-stone-100" title={file.original_filename ?? ''}>
                          {file.original_filename || 'Unnamed image'}
                        </p>
                        <p className="mt-1 break-all text-xs text-stone-400">{file.stored_filename || 'Stored filename pending'}</p>
                      </div>
                      <div className="flex items-center gap-2">
                        <span className={`h-2 w-2 rounded-full ${fileTone.light}`} />
                        <span className={`text-xs font-semibold ${fileTone.text}`}>{file.status}</span>
                      </div>
                    </div>
                    {file.error_message ? <p className="mt-2 text-sm text-red-100">{file.error_message}</p> : null}
                  </div>
                );
              })}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function toneForStatus(status: string) {
  switch (status) {
    case 'processed':
      return {
        container: 'border-signal-green/30 bg-signal-green/10',
        light: 'bg-signal-green shadow-[0_0_16px_rgba(139,207,139,0.72)]',
        text: 'text-green-100',
      };
    case 'failed':
      return {
        container: 'border-red-400/35 bg-red-950/30',
        light: 'bg-signal-red shadow-[0_0_14px_rgba(255,59,36,0.75)]',
        text: 'text-red-100',
      };
    case 'completed_with_errors':
      return {
        container: 'border-amberline-300/40 bg-copper-950/25',
        light: 'bg-amberline-300 shadow-[0_0_14px_rgba(255,189,89,0.75)]',
        text: 'text-amberline-100',
      };
    default:
      return {
        container: 'border-copper-400/35 bg-copper-950/20',
        light: 'bg-amberline-300 shadow-[0_0_14px_rgba(255,189,89,0.7)]',
        text: 'text-amberline-100',
      };
  }
}

function labelForStatus(status: string) {
  switch (status) {
    case 'processed':
      return 'Processing complete';
    case 'failed':
      return 'Processing failed';
    case 'completed_with_errors':
      return 'Completed with errors';
    case 'processing':
      return 'Processing images';
    case 'pending':
      return 'Upload accepted';
    default:
      return `Status: ${status}`;
  }
}
