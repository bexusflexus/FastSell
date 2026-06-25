import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { Link } from 'react-router-dom';
import { ApiError } from '../api/client';
import { listContainers } from '../api/containers';
import { imageUrl } from '../api/images';
import { listInventoryGroups } from '../api/inventoryGroups';
import { listLocations } from '../api/locations';
import {
  createWholeSceneScan,
  getWholeSceneScan,
  queueWholeSceneAnalysis,
} from '../api/wholeScene';
import { Panel } from '../components/Panel';
import type { InventoryGroup } from '../types/inventoryGroups';
import type { LocationOption } from '../types/locations';
import type { ContainerOption, IntakeContextPayload } from '../types/upload';
import type { WholeSceneScan } from '../types/wholeScene';
import { formatBytes } from '../utils/formatBytes';
import { createFileId } from '../utils/id';
import { WHOLE_SCENE_USER_LABEL } from '../wholeScenePresentation';

interface WholeSceneImageDraft {
  clientFileId: string;
  originalFilename: string;
  sizeBytes: number;
  mimeType: string;
  objectUrl: string;
  file: File;
}

const maxWholeSceneImages = 6;
const pollIntervalMs = 2_500;
const terminalImageStatuses = new Set(['processed', 'failed']);
const activeScanStatuses = new Set(['queued', 'processing']);

export function WholeScenePage() {
  const [containers, setContainers] = useState<ContainerOption[]>([]);
  const [locations, setLocations] = useState<LocationOption[]>([]);
  const [inventoryGroups, setInventoryGroups] = useState<InventoryGroup[]>([]);
  const [selectedContainerId, setSelectedContainerId] = useState<string | null>(null);
  const [noContainer, setNoContainer] = useState(false);
  const [selectedLocationId, setSelectedLocationId] = useState<string | null>(null);
  const [locationDetail, setLocationDetail] = useState('');
  const [inventoryGroupId, setInventoryGroupId] = useState<string | null>(null);
  const [hint, setHint] = useState('');
  const [images, setImages] = useState<WholeSceneImageDraft[]>([]);
  const [scan, setScan] = useState<WholeSceneScan | null>(null);
  const [isLoadingContext, setIsLoadingContext] = useState(true);
  const [contextError, setContextError] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const pollTimerRef = useRef<number | null>(null);
  const pollAbortRef = useRef<AbortController | null>(null);
  const imagesRef = useRef<WholeSceneImageDraft[]>([]);
  const createInFlightRef = useRef(false);

  const selectedContainer = useMemo(
    () => containers.find((container) => container.id === selectedContainerId) ?? null,
    [containers, selectedContainerId],
  );

  const sourceImagesTerminal = scan ? scan.images.length > 0 && scan.images.every((image) => terminalImageStatuses.has(image.image.status)) : false;
  const usableSourceImages = scan?.images.filter((image) => image.image.status === 'processed').length ?? 0;
  const canCreateScan = (noContainer || Boolean(selectedContainerId)) && images.length >= 1 && images.length <= maxWholeSceneImages && busyAction !== 'create';
  const canAnalyze = Boolean(scan) && sourceImagesTerminal && usableSourceImages > 0 && !activeScanStatuses.has(scan?.status ?? '') && busyAction !== 'analyze';

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    pollAbortRef.current?.abort();
    pollAbortRef.current = null;
  }, []);

  const refreshScan = useCallback(async (scanId = scan?.id, fromPoll = false) => {
    if (!scanId) {
      return;
    }
    if (!fromPoll) {
      setBusyAction('refresh');
      setError(null);
    }
    pollTimerRef.current = null;
    const controller = new AbortController();
    pollAbortRef.current = controller;
    try {
      const nextScan = await getWholeSceneScan(scanId, controller.signal);
      setScan(nextScan);
      if (!fromPoll) {
        setMessage('Scan refreshed.');
      }
    } catch (err) {
      if (!controller.signal.aborted) {
        setError(errorMessage(err, 'Failed to refresh Whole Scene scan.'));
      }
    } finally {
      if (pollAbortRef.current === controller) {
        pollAbortRef.current = null;
      }
      if (!fromPoll) {
        setBusyAction(null);
      }
    }
  }, [scan?.id]);

  const schedulePoll = useCallback((scanId: string) => {
    if (pollTimerRef.current !== null) {
      return;
    }
    pollTimerRef.current = window.setTimeout(() => {
      void refreshScan(scanId, true);
    }, pollIntervalMs);
  }, [refreshScan]);

  useEffect(() => {
    let isMounted = true;

    const loadContext = async () => {
      setIsLoadingContext(true);
      setContextError(null);
      try {
        const [containerList, locationList, groupList] = await Promise.all([
          listContainers(),
          listLocations(),
          listInventoryGroups(),
        ]);
        if (!isMounted) {
          return;
        }
        setContainers(containerList);
        setLocations(locationList.filter((location) => !location.archived));
        setInventoryGroups(groupList.filter((group) => !group.archived));
      } catch (err) {
        if (isMounted) {
          setContextError(errorMessage(err, 'Failed to load Whole Scene context.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingContext(false);
        }
      }
    };

    void loadContext();

    return () => {
      isMounted = false;
    };
  }, []);

  useEffect(() => {
    imagesRef.current = images;
  }, [images]);

  useEffect(() => {
    return () => {
      imagesRef.current.forEach((image) => URL.revokeObjectURL(image.objectUrl));
      stopPolling();
    };
  }, [stopPolling]);

  useEffect(() => {
    if (!scan || !shouldPollScan(scan)) {
      stopPolling();
      return;
    }
    schedulePoll(scan.id);
  }, [scan, schedulePoll, stopPolling]);

  const handleSelectFiles = (fileList: FileList | null) => {
    if (!fileList) {
      return;
    }
    const availableSlots = maxWholeSceneImages - images.length;
    const selected = Array.from(fileList)
      .filter(isSelectableImage)
      .slice(0, availableSlots)
      .map((file) => ({
        clientFileId: createFileId(),
        originalFilename: file.name,
        sizeBytes: file.size,
        mimeType: file.type || 'image/*',
        objectUrl: URL.createObjectURL(file),
        file,
      }));
    if (selected.length === 0) {
      return;
    }
    setImages((current) => [...current, ...selected]);
  };

  const removeImage = (clientFileId: string) => {
    setImages((current) => {
      const image = current.find((entry) => entry.clientFileId === clientFileId);
      if (image) {
        URL.revokeObjectURL(image.objectUrl);
      }
      return current.filter((entry) => entry.clientFileId !== clientFileId);
    });
  };

  const buildIntakeContext = (): IntakeContextPayload => ({
    container_id: noContainer ? '' : selectedContainer?.id ?? '',
    container_name: noContainer ? '' : selectedContainer?.name ?? '',
    no_container: noContainer,
    location_id: noContainer ? selectedLocationId ?? '' : '',
    location_detail: noContainer ? locationDetail.trim() : '',
  });

  const handleCreateScan = async () => {
    if (!canCreateScan || createInFlightRef.current) {
      return;
    }
    createInFlightRef.current = true;
    setBusyAction('create');
    setError(null);
    setMessage(null);
    stopPolling();
    try {
      const nextScan = await createWholeSceneScan(
        {
          intake_context: buildIntakeContext(),
          hint: hint.trim() || null,
          inventory_group_id: inventoryGroupId || null,
          files: images.map((image) => ({
            client_file_id: image.clientFileId,
            original_filename: image.originalFilename,
            mime_type: image.mimeType,
            size_bytes: image.sizeBytes,
          })),
        },
        images.map((image) => image.file),
      );
      setScan(nextScan);
      setMessage('Whole Scene scan created.');
    } catch (err) {
      setError(errorMessage(err, 'Failed to create Whole Scene scan.'));
    } finally {
      createInFlightRef.current = false;
      setBusyAction(null);
    }
  };

  const handleAnalyze = async () => {
    if (!scan || !canAnalyze) {
      return;
    }
    setBusyAction('analyze');
    setError(null);
    setMessage(null);
    try {
      const response = await queueWholeSceneAnalysis(scan.id);
      setScan(response.scan);
      setMessage(response.queued ? 'Analysis queued.' : 'Analysis is already queued or processing.');
    } catch (err) {
      setError(errorMessage(err, 'Failed to queue Whole Scene analysis.'));
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel title={WHOLE_SCENE_USER_LABEL} eyebrow="Whole Scene">
        <div className="grid gap-4 lg:grid-cols-[0.95fr_1.05fr]">
          <div className="grid gap-4">
            {contextError ? <Alert tone="error">{contextError}</Alert> : null}
            <ContextFields
              containers={containers}
              locations={locations}
              inventoryGroups={inventoryGroups}
              isLoading={isLoadingContext}
              selectedContainerId={selectedContainerId}
              noContainer={noContainer}
              selectedLocationId={selectedLocationId}
              locationDetail={locationDetail}
              inventoryGroupId={inventoryGroupId}
              hint={hint}
              onContainerChange={(containerId) => {
                setSelectedContainerId(containerId);
                setNoContainer(false);
                setSelectedLocationId(null);
                setLocationDetail('');
              }}
              onNoContainer={() => {
                setSelectedContainerId(null);
                setNoContainer(true);
              }}
              onLocationChange={setSelectedLocationId}
              onLocationDetailChange={setLocationDetail}
              onInventoryGroupChange={setInventoryGroupId}
              onHintChange={setHint}
            />
            <SceneImagePicker images={images} onSelectFiles={handleSelectFiles} onRemoveImage={removeImage} />
            <div className="flex flex-wrap gap-3">
              <button type="button" className="primary-button" disabled={!canCreateScan} onClick={handleCreateScan}>
                {busyAction === 'create' ? 'Creating...' : 'Create Scan'}
              </button>
              <button type="button" className="secondary-button" disabled={!scan || busyAction === 'refresh'} onClick={() => void refreshScan()}>
                {busyAction === 'refresh' ? 'Refreshing...' : 'Refresh'}
              </button>
              <button type="button" className="primary-button" disabled={!canAnalyze} onClick={handleAnalyze}>
                {busyAction === 'analyze' ? 'Queueing...' : 'Run Analysis'}
              </button>
            </div>
            {!canCreateScan && !scan ? (
              <p className="text-sm text-stone-400">Select a context and 1-6 scene images before creating a scan.</p>
            ) : null}
            {!canAnalyze && scan ? (
              <p className="text-sm text-stone-400">
                Analysis is available after source images finish intake and at least one image is processed.
              </p>
            ) : null}
          </div>
          <ScanStatusPanel scan={scan} />
        </div>
      </Panel>

      {message ? <Alert tone="success">{message}</Alert> : null}
      {error ? <Alert tone="error">{error}</Alert> : null}

      {scan ? (
        <section className="grid gap-6 lg:grid-cols-[0.82fr_1.18fr]">
          <Panel title="Source Images" eyebrow="Scan provenance">
            <SourceImageGrid scan={scan} />
          </Panel>
          <Panel title="Review handoff" eyebrow="Next step">
            <WholeSceneReviewHandoff scan={scan} />
          </Panel>
        </section>
      ) : null}
    </div>
  );
}

interface ContextFieldsProps {
  containers: ContainerOption[];
  locations: LocationOption[];
  inventoryGroups: InventoryGroup[];
  isLoading: boolean;
  selectedContainerId: string | null;
  noContainer: boolean;
  selectedLocationId: string | null;
  locationDetail: string;
  inventoryGroupId: string | null;
  hint: string;
  onContainerChange: (containerId: string | null) => void;
  onNoContainer: () => void;
  onLocationChange: (locationId: string | null) => void;
  onLocationDetailChange: (value: string) => void;
  onInventoryGroupChange: (groupId: string | null) => void;
  onHintChange: (value: string) => void;
}

function ContextFields({
  containers,
  locations,
  inventoryGroups,
  isLoading,
  selectedContainerId,
  noContainer,
  selectedLocationId,
  locationDetail,
  inventoryGroupId,
  hint,
  onContainerChange,
  onNoContainer,
  onLocationChange,
  onLocationDetailChange,
  onInventoryGroupChange,
  onHintChange,
}: ContextFieldsProps) {
  return (
    <div className="grid gap-4">
      <FieldLabel label="Container">
        <select
          value={noContainer ? 'no-container' : selectedContainerId ?? ''}
          disabled={isLoading}
          onChange={(event) => {
            if (event.target.value === 'no-container') {
              onNoContainer();
              return;
            }
            onContainerChange(event.target.value || null);
          }}
          className="field-input"
        >
          <option value="">{isLoading ? 'Loading containers...' : 'Select a container'}</option>
          {containers.map((container) => (
            <option key={container.id} value={container.id}>
              {container.name}
              {container.containerTypeName ? ` - ${container.containerTypeName}` : container.type ? ` - ${container.type}` : ''}
              {container.locationName ? ` - ${container.locationName}` : container.locationDescription ? ` - ${container.locationDescription}` : ''}
            </option>
          ))}
          <option value="no-container">No Container</option>
        </select>
      </FieldLabel>

      {noContainer ? (
        <div className="grid gap-4 sm:grid-cols-2">
          <FieldLabel label="Location">
            <select
              value={selectedLocationId ?? ''}
              disabled={isLoading}
              onChange={(event) => onLocationChange(event.target.value || null)}
              className="field-input"
            >
              <option value="">No location</option>
              {locations.map((location) => (
                <option key={location.id} value={location.id}>
                  {location.name}
                </option>
              ))}
            </select>
          </FieldLabel>
          <FieldLabel label="Location Detail">
            <input value={locationDetail} onChange={(event) => onLocationDetailChange(event.target.value)} className="field-input" />
          </FieldLabel>
        </div>
      ) : null}

      <FieldLabel label="Inventory Group">
        <select
          value={inventoryGroupId ?? ''}
          disabled={isLoading}
          onChange={(event) => onInventoryGroupChange(event.target.value || null)}
          className="field-input"
        >
          <option value="">Select inventory group</option>
          {inventoryGroups.map((group) => (
            <option key={group.id} value={group.id}>
              {group.name}
            </option>
          ))}
        </select>
      </FieldLabel>

      <FieldLabel label="Hint">
        <textarea
          value={hint}
          onChange={(event) => onHintChange(event.target.value)}
          rows={3}
          className="field-input min-h-24"
        />
      </FieldLabel>
    </div>
  );
}

function SceneImagePicker({
  images,
  onSelectFiles,
  onRemoveImage,
}: {
  images: WholeSceneImageDraft[];
  onSelectFiles: (files: FileList | null) => void;
  onRemoveImage: (clientFileId: string) => void;
}) {
  return (
    <div className="rounded-md border border-rack-steel/28 bg-rack-soot/60 p-3">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p className="text-sm font-semibold text-stone-100">Scene images: {images.length}/{maxWholeSceneImages}</p>
        <label className="secondary-button cursor-pointer">
          Select Images
          <input
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={(event) => {
              onSelectFiles(event.target.files);
              event.target.value = '';
            }}
          />
        </label>
      </div>
      {images.length > 0 ? (
        <div className="mt-3 grid grid-cols-2 gap-3 sm:grid-cols-3">
          {images.map((image) => (
            <div key={image.clientFileId} className="rounded-md border border-rack-steel/25 bg-graphite-950/70 p-2">
              <img src={image.objectUrl} alt="" className="h-28 w-full rounded object-cover" />
              <p className="mt-2 truncate text-xs text-stone-300">{image.originalFilename}</p>
              <p className="text-xs text-stone-500">{formatBytes(image.sizeBytes)}</p>
              <button type="button" className="mt-2 text-xs font-semibold text-red-200 hover:text-red-100" onClick={() => onRemoveImage(image.clientFileId)}>
                Remove
              </button>
            </div>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function ScanStatusPanel({ scan }: { scan: WholeSceneScan | null }) {
  if (!scan) {
    return (
      <div className="rounded-md border border-rack-steel/28 bg-rack-soot/60 p-4 text-sm text-stone-400">
        No Whole Scene scan created yet.
      </div>
    );
  }

  return (
    <div className="grid gap-3 rounded-md border border-rack-steel/28 bg-rack-soot/60 p-4 text-sm">
      <InfoRow label="Scan" value={scan.id} mono />
      <InfoRow label="Status" value={scan.status} />
      <InfoRow label="Upload session" value={scan.upload_session_id} mono />
      <InfoRow label="Context" value={scan.container?.name ?? scan.location_name ?? scan.location_detail ?? 'No container'} />
      <InfoRow label="Inventory group" value={scan.inventory_group.name} />
      <InfoRow label="Latest analysis" value={scan.latest_analysis_run ? `${scan.latest_analysis_run.status} run ${scan.latest_analysis_run.run_number}` : 'None'} />
      {scan.latest_analysis_run?.error_message ? <p className="text-xs text-amberline-100">{scan.latest_analysis_run.error_message}</p> : null}
    </div>
  );
}

function SourceImageGrid({ scan }: { scan: WholeSceneScan }) {
  if (scan.images.length === 0) {
    return <p className="text-sm text-stone-400">No source images recorded.</p>;
  }

  return (
    <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-1">
      {scan.images.map((scanImage) => (
        <div key={scanImage.id} className="rounded-md border border-rack-steel/25 bg-graphite-950/55 p-2">
          {scanImage.image.status === 'processed' ? (
            <img src={imageUrl(scanImage.image_asset_id, 'thumbnail')} alt="" className="h-32 w-full rounded object-cover" />
          ) : (
            <div className="flex h-32 items-center justify-center rounded bg-rack-soot text-sm text-stone-500">{scanImage.image.status}</div>
          )}
          <p className="mt-2 truncate text-xs text-stone-300">{scanImage.image.original_filename ?? scanImage.image_asset_id}</p>
          <p className="text-xs text-stone-500">
            {scanImage.image.status}
            {scanImage.image.file_size_bytes ? ` - ${formatBytes(scanImage.image.file_size_bytes)}` : ''}
          </p>
          {scanImage.image.error_message ? <p className="mt-1 text-xs text-red-200">{scanImage.image.error_message}</p> : null}
        </div>
      ))}
    </div>
  );
}

function WholeSceneReviewHandoff({ scan }: { scan: WholeSceneScan }) {
  const latestStatus = scan.latest_analysis_run?.status ?? 'not requested';
  const candidateCount = scan.candidates.length;
  const pendingCount = scan.candidates.filter((candidate) => candidate.status === 'proposed' || candidate.status === 'edited').length;
  const analysisComplete = scan.latest_analysis_run?.status === 'succeeded' || scan.latest_analysis_run?.status === 'partial' || scan.status === 'succeeded' || scan.status === 'partial';
  const reviewHref = `/review?tab=whole_scene&scan_id=${encodeURIComponent(scan.id)}`;

  return (
    <div className="grid gap-4 text-sm text-stone-300">
      <div className="grid gap-2 rounded-md border border-rack-steel/25 bg-rack-soot/55 p-3">
        <InfoRow label="Analysis" value={latestStatus} />
        <InfoRow label="Candidates" value={analysisComplete ? String(candidateCount) : 'Waiting for analysis'} />
        {analysisComplete ? <InfoRow label="Pending review" value={String(pendingCount)} /> : null}
      </div>
      {scan.latest_analysis_run?.error_message ? <Alert tone="error">{scan.latest_analysis_run.error_message}</Alert> : null}
      <div className="grid gap-3">
        <Link className="primary-button w-fit" to={reviewHref}>
          Go to Review
        </Link>
        {analysisComplete ? (
          <p>Analysis is complete. Continue in Review to edit, reject, manually add, and approve candidates into inventory.</p>
        ) : (
          <p className="text-stone-400">Review can monitor this scan immediately and run analysis as soon as the source images are ready.</p>
        )}
      </div>
    </div>
  );
}

function FieldLabel({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="grid gap-2 text-sm font-medium text-stone-200">
      <span>{label}</span>
      {children}
    </label>
  );
}

function InfoRow({ label, value, mono = false }: { label: string; value: string; mono?: boolean }) {
  return (
    <div className="grid gap-1 sm:grid-cols-[130px_1fr]">
      <dt className="text-stone-500">{label}</dt>
      <dd className={mono ? 'break-all font-mono text-xs text-stone-200' : 'text-stone-200'}>{value}</dd>
    </div>
  );
}

function Alert({ tone, children }: { tone: 'success' | 'error'; children: ReactNode }) {
  const classes =
    tone === 'success'
      ? 'border-green-400/30 bg-green-950/20 text-green-100'
      : 'border-red-400/35 bg-red-950/30 text-red-100';
  return <div className={`rounded-md border px-3 py-2 text-sm ${classes}`}>{children}</div>;
}

function shouldPollScan(scan: WholeSceneScan): boolean {
  if (activeScanStatuses.has(scan.status)) {
    return true;
  }
  return scan.images.some((image) => !terminalImageStatuses.has(image.image.status));
}

function isSelectableImage(file: File): boolean {
  if (file.type.startsWith('image/')) {
    return true;
  }
  return /\.(jpe?g|png|heic|heif)$/i.test(file.name);
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
