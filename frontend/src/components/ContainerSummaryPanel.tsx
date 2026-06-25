import type { ContainerSummary } from '../types/upload';

interface ContainerSummaryPanelProps {
  summary: ContainerSummary | null;
  isLoading: boolean;
  error: string | null;
}

export function ContainerSummaryPanel({ summary, isLoading, error }: ContainerSummaryPanelProps) {
  if (isLoading) {
    return (
      <div className="rounded-md border border-copper-400/30 bg-copper-950/20 px-4 py-4 text-sm text-amberline-100">
        Loading selected container summary...
      </div>
    );
  }

  if (error) {
    return <div className="rounded-md border border-red-400/35 bg-red-950/30 px-4 py-4 text-sm text-red-100">{error}</div>;
  }

  if (!summary) {
    return (
      <div className="rounded-md border border-rack-steel/28 bg-rack-soot/70 px-4 py-4 text-sm text-stone-400">
        Select a container to see existing contents.
      </div>
    );
  }

  const inFlightCount =
    summary.summary.pending_image_count + summary.summary.uploaded_image_count + summary.summary.processing_image_count;
  const typeLabel = summary.container.container_type_name ?? summary.container.type;
  const locationLabel = summary.container.location_name ?? summary.container.location_description;

  return (
    <div className="rounded-md border border-rack-steel/30 bg-[linear-gradient(180deg,rgba(22,26,24,0.92),rgba(8,9,8,0.95))] p-4 shadow-panel">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-rack-glass">Existing contents</p>
          <h3 className="mt-2 text-lg font-semibold text-stone-100">{summary.container.name}</h3>
          <div className="mt-2 grid gap-1 text-sm text-stone-300">
            {typeLabel ? <p>Type: {typeLabel}</p> : null}
            {summary.container.container_type_name && summary.container.type ? <p className="text-xs text-stone-500">Legacy type: {summary.container.type}</p> : null}
            {locationLabel ? <p>Location: {locationLabel}</p> : null}
            {summary.container.location_name && summary.container.location_description ? (
              <p className="text-xs text-stone-500">Legacy location: {summary.container.location_description}</p>
            ) : null}
            {summary.container.notes ? <p>Notes: {summary.container.notes}</p> : null}
          </div>
        </div>
        <a
          href={`/inventory?container_id=${encodeURIComponent(summary.container.id)}`}
          className="inline-flex items-center justify-center rounded-md border border-copper-500/35 px-3 py-2 text-sm font-medium text-amberline-100 transition hover:bg-copper-500/12"
        >
          View container inventory
        </a>
      </div>

      <dl className="mt-4 grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <SummaryMetric label="Groups" value={summary.summary.upload_group_count} />
        <SummaryMetric label="Images" value={summary.summary.image_count} />
        <SummaryMetric label="In flight" value={inFlightCount} />
        <SummaryMetric label="Processed" value={summary.summary.processed_image_count} />
        <SummaryMetric label="Failed" value={summary.summary.failed_image_count} />
        <SummaryMetric label="Sessions" value={summary.summary.upload_session_count} />
      </dl>

      {summary.summary.latest_upload_datetime ? (
        <p className="mt-4 text-sm text-stone-400">Latest upload: {new Date(summary.summary.latest_upload_datetime).toLocaleString()}</p>
      ) : (
        <p className="mt-4 text-sm text-stone-400">No uploads recorded for this container yet.</p>
      )}

      <p className="mt-3 text-sm text-amberline-100">Add more photos below.</p>
    </div>
  );
}

function SummaryMetric({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded border border-rack-steel/24 bg-rack-soot/70 px-3 py-3">
      <dt className="text-xs uppercase tracking-[0.16em] text-rack-glass">{label}</dt>
      <dd className="mt-1 text-lg font-semibold text-stone-100">{value}</dd>
    </div>
  );
}
