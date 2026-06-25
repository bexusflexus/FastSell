import { useEffect, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { imageUrl } from '../api/images';
import { getAdminMetrics } from '../api/metrics';
import { Panel } from '../components/Panel';
import type {
  AdminMetricsDuplicateTitleGroup,
  AdminMetricsResponse,
  AdminMetricsTopValueItem,
} from '../types/metrics';

export function AdminMetricsPage() {
  const [metrics, setMetrics] = useState<AdminMetricsResponse | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let isMounted = true;

    const loadMetrics = async () => {
      setIsLoading(true);
      setError(null);

      try {
        const response = await getAdminMetrics();
        if (isMounted) {
          setMetrics(response);
        }
      } catch (loadError) {
        console.error('Failed to load admin metrics', loadError);
        if (isMounted) {
          setError(errorMessage(loadError, 'Failed to load admin metrics.'));
        }
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    };

    void loadMetrics();

    return () => {
      isMounted = false;
    };
  }, []);

  const handleRefresh = async () => {
    setIsLoading(true);
    setError(null);

    try {
      const response = await getAdminMetrics();
      setMetrics(response);
    } catch (loadError) {
      console.error('Failed to refresh admin metrics', loadError);
      setError(errorMessage(loadError, 'Failed to refresh admin metrics.'));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel
        title="Admin / Metrics"
        eyebrow="Read-only inventory summaries"
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
          <p className="text-sm text-stone-300">Read-only inventory summaries.</p>
          <p className="text-xs text-stone-500">
            Current inventory excludes archived items and items marked sold, donated, or disposed.
          </p>
          {metrics ? (
            <p className="text-xs text-stone-500">
              Generated: {new Date(metrics.generated_datetime).toLocaleString()}
            </p>
          ) : null}
        </div>
      </Panel>

      {error ? (
        <Panel title="Load Error" eyebrow="Metrics unavailable">
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

      {!metrics && isLoading ? (
        <Panel title="Loading Metrics" eyebrow="Admin / Metrics">
          <p className="text-sm text-stone-300">Loading inventory metrics...</p>
        </Panel>
      ) : null}

      {metrics ? (
        <>
          <Panel title="Summary" eyebrow="Inventory snapshot">
            <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
              <MetricCard label="Total Current Approximate Value" value={formatCurrency(metrics.summary.total_current_approx_value)} accent />
              <MetricCard label="Current Inventory Item Count" value={formatCount(metrics.summary.total_current_inventory_items)} />
              <MetricCard label="For Sale" value={formatCount(metrics.summary.for_sale_count)} to={inventoryMetricsLink({ dispositionCode: 'for_sale' })} />
              <MetricCard label="In Use" value={formatCount(metrics.summary.in_use_count)} to={inventoryMetricsLink({ dispositionCode: 'in_use' })} />
              <MetricCard label="Sale Pending" value={formatCount(metrics.summary.sale_pending_count)} to={inventoryMetricsLink({ dispositionCode: 'sale_pending' })} />
              <MetricCard label="Sold" value={formatCount(metrics.summary.sold_count)} to={inventoryMetricsLink({ dispositionCode: 'sold', inventoryState: 'former', includeArchived: true })} />
              <MetricCard label="Donated" value={formatCount(metrics.summary.donated_count)} to={inventoryMetricsLink({ dispositionCode: 'donated', inventoryState: 'former', includeArchived: true })} />
              <MetricCard label="Disposed" value={formatCount(metrics.summary.disposed_count)} to={inventoryMetricsLink({ dispositionCode: 'disposed', inventoryState: 'former', includeArchived: true })} />
              <MetricCard label="Archived" value={formatCount(metrics.summary.archived_count)} to={inventoryMetricsLink({ archived: true })} />
              <MetricCard label="AI Enriched" value={formatCount(metrics.summary.ai_enriched_count)} to={inventoryMetricsLink({ aiEnriched: true })} />
              <MetricCard label="Missing Approx Value" value={formatCount(metrics.summary.missing_approx_value_count)} to={inventoryMetricsLink({ missingApproxValue: true })} />
            </div>
          </Panel>

          <Panel title="Top 10 Most Valuable Items" eyebrow="Current inventory">
            {metrics.top_value_items.length === 0 ? (
              <EmptyState message="No current inventory items with approximate values were found." />
            ) : (
              <div className="grid gap-3">
                {metrics.top_value_items.map((item) => (
                  <TopValueRow key={item.id} item={item} />
                ))}
              </div>
            )}
          </Panel>

          <Panel title="Duplicate Title Candidates" eyebrow="Normalized title grouping">
            {metrics.duplicate_title_groups.length === 0 ? (
              <EmptyState message="No duplicate title candidates were found." />
            ) : (
              <div className="grid gap-4">
                {metrics.duplicate_title_groups.map((group) => (
                  <DuplicateGroupCard key={group.normalized_title} group={group} />
                ))}
              </div>
            )}
          </Panel>

        </>
      ) : null}
    </div>
  );
}

function MetricCard({ label, value, accent = false, to }: { label: string; value: string; accent?: boolean; to?: string }) {
  const className = `rounded-md border p-4 ${
    accent
      ? 'border-copper-400/35 bg-[linear-gradient(145deg,rgba(95,43,18,0.3),rgba(25,17,12,0.92))]'
      : 'border-rack-steel/25 bg-rack-soot/60'
  } ${to ? 'cursor-pointer transition hover:border-amberline-400/35 hover:bg-rack-steel/10' : ''}`;

  const content = (
    <>
      <p className="text-xs font-semibold uppercase tracking-[0.2em] text-rack-glass">{label}</p>
      <p className="mt-3 text-2xl font-semibold text-stone-50">{value}</p>
    </>
  );

  if (!to) {
    return <div className={className}>{content}</div>;
  }

  return (
    <Link to={to} className={className}>
      {content}
    </Link>
  );
}

function TopValueRow({ item }: { item: AdminMetricsTopValueItem }) {
  const title = item.title?.trim() || 'Untitled item';

  return (
    <div className="grid gap-3 rounded-md border border-rack-steel/25 bg-rack-soot/60 p-3 lg:grid-cols-[4.5rem_minmax(0,2fr)_10rem_10rem_9rem] lg:items-center">
      <div className="flex h-16 w-16 items-center justify-center overflow-hidden rounded-md border border-rack-steel/20 bg-black/25">
        {item.primary_image_id ? (
          <img src={imageUrl(item.primary_image_id)} alt={title} loading="lazy" className="h-full w-full object-cover" />
        ) : (
          <span className="px-2 text-center text-[11px] text-stone-500">No image</span>
        )}
      </div>
      <div className="grid gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold text-stone-100">{title}</span>
          {item.disposition_code ? <InlineBadge>{item.disposition_code}</InlineBadge> : null}
        </div>
        <p className="text-xs text-stone-400">{item.container_name ?? 'No container'}</p>
        <p className="truncate text-[11px] text-stone-500">{item.id}</p>
      </div>
      <InfoPair label="Approx value" value={formatCurrency(item.approx_value)} />
      <InfoPair label="Container" value={item.container_name ?? 'Unassigned'} />
      <div className="flex lg:justify-end">
        <Link
          to={inventoryLink(item.container_id)}
          className="rounded-md border border-rack-steel/30 bg-rack-soot/80 px-3 py-2 text-sm font-semibold text-stone-100 transition hover:border-amberline-400/35 hover:bg-rack-steel/12"
        >
          Open inventory
        </Link>
      </div>
    </div>
  );
}

function DuplicateGroupCard({ group }: { group: AdminMetricsDuplicateTitleGroup }) {
  return (
    <article className="rounded-md border border-rack-steel/25 bg-rack-soot/60 p-4">
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <p className="text-xs font-semibold uppercase tracking-[0.2em] text-rack-glass">Normalized Title</p>
          <h3 className="mt-1 text-base font-semibold text-stone-100">{group.normalized_title || '(blank title)'}</h3>
        </div>
        <div className="flex flex-wrap gap-2 text-xs text-stone-200">
          <InlineBadge>{formatCount(group.count)} items</InlineBadge>
          <InlineBadge>{formatCurrency(group.total_approx_value)} total</InlineBadge>
        </div>
      </div>
      <div className="mt-4 grid gap-2">
        {group.items.map((item) => (
          <div key={item.id} className="grid gap-2 rounded-md border border-rack-steel/18 bg-black/10 px-3 py-3 sm:grid-cols-[minmax(0,2fr)_9rem_9rem] sm:items-center">
            <div className="grid gap-1">
              <span className="text-sm font-semibold text-stone-100">{item.title?.trim() || 'Untitled item'}</span>
              <p className="text-xs text-stone-400">{item.container_name ?? 'No container'}</p>
              <p className="truncate text-[11px] text-stone-500">{item.id}</p>
            </div>
            <InfoPair label="Approx value" value={formatCurrency(item.approx_value)} />
            <InfoPair label="Disposition" value={item.disposition_code ?? 'Unknown'} />
          </div>
        ))}
      </div>
    </article>
  );
}

function EmptyState({ message }: { message: string }) {
  return <p className="text-sm text-stone-400">{message}</p>;
}

function InfoPair({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-1">
      <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-rack-glass">{label}</p>
      <p className="text-sm text-stone-200">{value}</p>
    </div>
  );
}

function InlineBadge({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex items-center rounded-full border border-copper-500/28 bg-copper-500/10 px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.16em] text-amberline-100">
      {children}
    </span>
  );
}

function formatCurrency(value: number | null): string {
  if (value == null || !Number.isFinite(value)) {
    return 'Not set';
  }

  try {
    return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(value);
  } catch {
    return `$${value.toFixed(2)}`;
  }
}

function formatCount(value: number): string {
  return new Intl.NumberFormat('en-US').format(value);
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

function inventoryLink(containerId: string | null): string {
  if (!containerId) {
    return '/inventory';
  }

  const params = new URLSearchParams({ container_id: containerId });
  return `/inventory?${params.toString()}`;
}

function inventoryMetricsLink(options: {
  dispositionCode?: string;
  inventoryState?: 'current' | 'former' | 'all';
  includeArchived?: boolean;
  archived?: boolean;
  aiEnriched?: boolean;
  missingApproxValue?: boolean;
}): string {
  const params = new URLSearchParams();
  if (options.dispositionCode) {
    params.set('disposition_code', options.dispositionCode);
  }
  if (options.inventoryState && options.inventoryState !== 'current') {
    params.set('inventory_state', options.inventoryState);
  }
  if (options.includeArchived) {
    params.set('include_archived', 'true');
  }
  if (options.archived) {
    params.set('archived', 'true');
  }
  if (options.aiEnriched) {
    params.set('ai_enriched', 'true');
  }
  if (options.missingApproxValue) {
    params.set('missing_approx_value', 'true');
  }
  return `/inventory${params.size > 0 ? `?${params.toString()}` : ''}`;
}
