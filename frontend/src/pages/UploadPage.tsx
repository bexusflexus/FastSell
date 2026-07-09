import { useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { API_BASE_URL, ApiError } from '../api/client';
import { getContainerSummary, listContainers } from '../api/containers';
import { listInventoryGroups } from '../api/inventoryGroups';
import { listLocations } from '../api/locations';
import { clearUploadDraft, restoreUploadDraft, saveUploadDraft } from '../api/uploadDraftStorage';
import { ContainerSummaryPanel } from '../components/ContainerSummaryPanel';
import { getNextUploadItemNumber, getUploadSession, uploadGroupedImages } from '../api/uploads';
import { IntakeContextPanel } from '../components/IntakeContextPanel';
import { ItemGroupCard } from '../components/ItemGroupCard';
import { JsonPreview } from '../components/JsonPreview';
import { Panel } from '../components/Panel';
import { StatusPanel } from '../components/StatusPanel';
import type {
  ContainerOption,
  ContainerSummary,
  ImageDraft,
  ItemGroupDraft,
  NextUploadItemNumberResponse,
  UploadImageResponse,
  PollingState,
  UploadSessionDraft,
  UploadSessionPayload,
  UploadSessionStatusResponse,
  UploadState,
} from '../types/upload';
import type { InventoryGroup } from '../types/inventoryGroups';
import type { LocationOption } from '../types/locations';
import { createFileId, createGroupId } from '../utils/id';

const defaultUploadItemNumber: NextUploadItemNumberResponse = {
  next_number: 10,
  step: 10,
  title_prefix: 'Item',
  suggested_title: 'Item 10',
  scope: 'global',
};

const createInitialGroup = (title = defaultUploadItemNumber.suggested_title): ItemGroupDraft => ({
  clientGroupId: createGroupId(),
  title,
  notes: '',
  images: [],
  autoTitle: true,
});

const createInitialDraft = (title = defaultUploadItemNumber.suggested_title): UploadSessionDraft => ({
  selectedContainerId: null,
  noContainer: false,
  inventoryGroupId: null,
  locationId: null,
  locationDetail: '',
  sessionNotes: '',
  groups: [createInitialGroup(title)],
});

const pollingIntervalMs = 2_000;
const pollingTimeoutMs = 300_000;
const terminalSessionStatuses = new Set(['processed', 'failed', 'completed_with_errors']);

export function UploadPage() {
  const [searchParams] = useSearchParams();
  const [containers, setContainers] = useState<ContainerOption[]>([]);
  const [locations, setLocations] = useState<LocationOption[]>([]);
  const [inventoryGroups, setInventoryGroups] = useState<InventoryGroup[]>([]);
  const [draft, setDraft] = useState<UploadSessionDraft>(createInitialDraft());
  const [nextItemNumberInfo, setNextItemNumberInfo] = useState<NextUploadItemNumberResponse>(defaultUploadItemNumber);
  const [payloadPreview, setPayloadPreview] = useState<UploadSessionPayload | null>(null);
  const [uploadResult, setUploadResult] = useState<UploadImageResponse | null>(null);
  const [sessionStatus, setSessionStatus] = useState<UploadSessionStatusResponse | null>(null);
  const [uploadState, setUploadState] = useState<UploadState>('idle');
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [pollingState, setPollingState] = useState<PollingState>('idle');
  const [pollingError, setPollingError] = useState<string | null>(null);
  const [containerLoadError, setContainerLoadError] = useState<string | null>(null);
  const [locationsLoadError, setLocationsLoadError] = useState<string | null>(null);
  const [inventoryGroupsLoadError, setInventoryGroupsLoadError] = useState<string | null>(null);
  const [containerSummary, setContainerSummary] = useState<ContainerSummary | null>(null);
  const [isLoadingContainerSummary, setIsLoadingContainerSummary] = useState(false);
  const [containerSummaryError, setContainerSummaryError] = useState<string | null>(null);
  const [isLoadingContainers, setIsLoadingContainers] = useState(true);
  const [isLoadingLocations, setIsLoadingLocations] = useState(true);
  const [isLoadingInventoryGroups, setIsLoadingInventoryGroups] = useState(true);
  const [showUploadCompleteModal, setShowUploadCompleteModal] = useState(false);
  const [localDraftHydrated, setLocalDraftHydrated] = useState(false);
  const imagesRef = useRef<ImageDraft[]>([]);
  const draftGroupsRef = useRef<ItemGroupDraft[]>(draft.groups);
  const pollTimerRef = useRef<number | null>(null);
  const pollAbortRef = useRef<AbortController | null>(null);
  const pollStartedAtRef = useRef<number | null>(null);
  const nextNumberRequestRef = useRef(0);

  useEffect(() => {
    let isMounted = true;

    const loadContainers = async () => {
      setIsLoadingContainers(true);
      setContainerLoadError(null);

      try {
        const apiContainers = await listContainers();
        if (isMounted) {
          setContainers(apiContainers);
        }
      } catch (error) {
        if (isMounted) {
          setContainerLoadError(errorMessage(error, 'Failed to load containers from the FastSell API.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingContainers(false);
        }
      }
    };

    void loadContainers();

    const loadLocations = async () => {
      setIsLoadingLocations(true);
      setLocationsLoadError(null);

      try {
        const apiLocations = await listLocations();
        if (isMounted) {
          setLocations(apiLocations.filter((location) => !location.archived));
        }
      } catch (error) {
        if (isMounted) {
          setLocationsLoadError(errorMessage(error, 'Failed to load locations from the FastSell API.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingLocations(false);
        }
      }
    };

    void loadLocations();

    const loadInventoryGroups = async () => {
      setIsLoadingInventoryGroups(true);
      setInventoryGroupsLoadError(null);

      try {
        const apiGroups = await listInventoryGroups();
        if (isMounted) {
          setInventoryGroups(apiGroups.filter((group) => !group.archived));
        }
      } catch (error) {
        if (isMounted) {
          setInventoryGroupsLoadError(errorMessage(error, 'Failed to load inventory groups from the FastSell API.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingInventoryGroups(false);
        }
      }
    };

    void loadInventoryGroups();

    return () => {
      isMounted = false;
    };
  }, []);

  useEffect(() => {
    let isMounted = true;

    const restoreDraft = async () => {
      try {
        const restored = await restoreUploadDraft();
        if (!isMounted) {
          restored.draft?.groups.flatMap((group) => group.images).forEach((image) => URL.revokeObjectURL(image.objectUrl));
          return;
        }

        if (restored.draft && isPersistableDraft(restored.draft)) {
          setDraft(ensureDraftHasGroup(restored.draft));
        }
        if (restored.missingImageCount > 0) {
          setUploadError('Some locally saved images could not be restored and were removed from this draft.');
          setUploadState('error');
        }
      } catch (error) {
        console.warn('Failed to restore local upload draft', error);
        if (isMounted) {
          setUploadError('The local upload draft could not be restored.');
          setUploadState('error');
        }
      } finally {
        if (isMounted) {
          setLocalDraftHydrated(true);
        }
      }
    };

    void restoreDraft();

    return () => {
      isMounted = false;
    };
  }, []);

  useEffect(() => {
    const queryContainerId = searchParams.get('container_id');
    if (!queryContainerId || containers.length === 0) {
      return;
    }

    const containerExists = containers.some((container) => container.id === queryContainerId);
    if (containerExists) {
      setDraft((current) => ({
        ...current,
        selectedContainerId: queryContainerId,
        noContainer: false,
        locationId: null,
        locationDetail: '',
      }));
    }
  }, [containers, searchParams]);

  useEffect(() => {
    if (!draft.selectedContainerId || draft.noContainer) {
      return;
    }

    const stillActive = containers.some((container) => container.id === draft.selectedContainerId);
    if (!isLoadingContainers && !stillActive) {
      setDraft((current) => ({ ...current, selectedContainerId: null }));
    }
  }, [containers, draft.noContainer, draft.selectedContainerId, isLoadingContainers]);

  useEffect(() => {
    let isMounted = true;

    const loadSummary = async () => {
      if (draft.noContainer || !draft.selectedContainerId) {
        setContainerSummary(null);
        setContainerSummaryError(null);
        setIsLoadingContainerSummary(false);
        return;
      }

      setIsLoadingContainerSummary(true);
      setContainerSummaryError(null);

      try {
        const summary = await getContainerSummary(draft.selectedContainerId);
        if (isMounted) {
          setContainerSummary(summary);
        }
      } catch (error) {
        if (isMounted) {
          setContainerSummary(null);
          setContainerSummaryError(errorMessage(error, 'Failed to load selected container summary.'));
        }
      } finally {
        if (isMounted) {
          setIsLoadingContainerSummary(false);
        }
      }
    };

    void loadSummary();

    return () => {
      isMounted = false;
    };
  }, [draft.noContainer, draft.selectedContainerId]);

  useEffect(() => {
    const requestId = nextNumberRequestRef.current + 1;
    nextNumberRequestRef.current = requestId;
    let isMounted = true;

    const loadNextItemNumber = async () => {
      const containerId = draft.noContainer ? undefined : draft.selectedContainerId ?? undefined;

      try {
        const response = await getNextUploadItemNumber(containerId);
        if (!isMounted || nextNumberRequestRef.current != requestId) {
          return;
        }

        const nextInfo = nextItemNumberInfoAfterApplying(response, draftGroupsRef.current);
        setDraft((current) => ({
          ...current,
          groups: applySuggestedTitlesToGroups(current.groups, response),
        }));
        setNextItemNumberInfo(nextInfo);
      } catch (error) {
        console.error('Failed to load next upload item number', error);
        if (!isMounted || nextNumberRequestRef.current != requestId) {
          return;
        }
        const nextInfo = nextItemNumberInfoAfterApplying(defaultUploadItemNumber, draftGroupsRef.current);
        setDraft((current) => ({
          ...current,
          groups: applySuggestedTitlesToGroups(current.groups, defaultUploadItemNumber),
        }));
        setNextItemNumberInfo(nextInfo);
      }
    };

    void loadNextItemNumber();

    return () => {
      isMounted = false;
    };
  }, [draft.noContainer, draft.selectedContainerId]);

  useEffect(() => {
    imagesRef.current = draft.groups.flatMap((group) => group.images);
    draftGroupsRef.current = draft.groups;
  }, [draft.groups]);

  useEffect(() => {
    if (!localDraftHydrated) {
      return;
    }

    const timeoutId = window.setTimeout(() => {
      const persistDraft = async () => {
        try {
          if (isPersistableDraft(draft)) {
            await saveUploadDraft(draft);
          } else {
            await clearUploadDraft();
          }
        } catch (error) {
          console.warn('Failed to persist local upload draft', error);
        }
      };

      void persistDraft();
    }, 250);

    return () => {
      window.clearTimeout(timeoutId);
    };
  }, [draft, localDraftHydrated]);

  useEffect(() => {
    return () => {
      imagesRef.current.forEach((image) => URL.revokeObjectURL(image.objectUrl));
      stopPolling('idle');
    };
  }, []);

  const selectedContainer = useMemo(
    () => containers.find((container) => container.id === draft.selectedContainerId) ?? null,
    [containers, draft.selectedContainerId],
  );

  const hasImages = draft.groups.some((group) => group.images.length > 0);
  const intakeIsValid = draft.noContainer || Boolean(draft.selectedContainerId);
  const inventoryGroupIsValid = Boolean(draft.inventoryGroupId);
  const canUpload = intakeIsValid && inventoryGroupIsValid && draft.groups.length > 0 && hasImages && uploadState !== 'uploading' && pollingState !== 'polling';
  const hasDirtyDraft = useMemo(
    () =>
      Boolean(
        draft.selectedContainerId ||
          draft.noContainer ||
          draft.inventoryGroupId ||
          draft.locationId ||
          draft.locationDetail.trim() ||
          draft.sessionNotes.trim() ||
          uploadResult ||
          sessionStatus ||
          uploadError ||
          pollingError ||
          payloadPreview,
      ) ||
      draft.groups.length !== 1 ||
      draft.groups.some((group) => group.images.length > 0 || group.notes.trim() || !group.autoTitle),
    [draft, payloadPreview, pollingError, sessionStatus, uploadError, uploadResult],
  );

  const readinessMessages = useMemo(() => {
    const messages: string[] = [];

    if (!intakeIsValid) {
      messages.push('Select a container or choose No Container.');
    }
    if (!inventoryGroupIsValid) {
      messages.push('Select an inventory group.');
    }
    if (draft.groups.length === 0) {
      messages.push('Add at least one inventory item.');
    }
    if (!hasImages) {
      messages.push('Add at least one image to any inventory item.');
    }

    return messages;
  }, [draft.groups.length, hasImages, intakeIsValid, inventoryGroupIsValid]);

  const buildPayload = (): UploadSessionPayload => ({
    intake_context: {
      container_id: draft.noContainer ? '' : selectedContainer?.id ?? '',
      container_name: draft.noContainer ? '' : selectedContainer?.name ?? '',
      no_container: draft.noContainer,
      location_id: draft.noContainer ? draft.locationId ?? '' : '',
      location_detail: draft.noContainer ? draft.locationDetail.trim() : '',
    },
    session_notes: draft.sessionNotes.trim(),
    groups: draft.groups.map((group) => ({
      client_group_id: group.clientGroupId,
      inventory_group_id: draft.inventoryGroupId ?? undefined,
      title: group.title.trim() || group.clientGroupId,
      notes: group.notes.trim(),
      files: group.images.map((image) => ({
        client_file_id: image.clientFileId,
        original_filename: image.originalFilename,
        mime_type: image.mimeType,
        size_bytes: image.sizeBytes,
      })),
    })),
  });

  const updateGroup = (clientGroupId: string, patch: Partial<Pick<ItemGroupDraft, 'title' | 'notes'>>) => {
    setDraft((current) => ({
      ...current,
      groups: current.groups.map((group) => {
        if (group.clientGroupId !== clientGroupId) {
          return group;
        }

        return {
          ...group,
          ...patch,
          autoTitle: patch.title === undefined ? group.autoTitle : false,
        };
      }),
    }));
  };

  const addGroup = () => {
    const nextTitle = `${nextItemNumberInfo.title_prefix} ${nextItemNumberInfo.next_number}`;
    setDraft((current) => ({
      ...current,
      groups: [
        ...current.groups,
        {
          clientGroupId: createGroupId(),
          title: nextTitle,
          notes: '',
          images: [],
          autoTitle: true,
        },
      ],
    }));
    setNextItemNumberInfo((current) => ({
      ...current,
      next_number: current.next_number + current.step,
      suggested_title: `${current.title_prefix} ${current.next_number + current.step}`,
    }));
  };

  const removeGroup = (clientGroupId: string) => {
    setDraft((current) => {
      if (current.groups.length <= 1) {
        return current;
      }

      const groupToRemove = current.groups.find((group) => group.clientGroupId === clientGroupId);
      groupToRemove?.images.forEach((image) => URL.revokeObjectURL(image.objectUrl));

      return {
        ...current,
        groups: current.groups.filter((group) => group.clientGroupId !== clientGroupId),
      };
    });
  };

  const addImagesToGroup = (clientGroupId: string, files: FileList) => {
    const imageDrafts = Array.from(files)
      .filter(isSelectableImage)
      .map((file) => ({
        clientFileId: createFileId(),
        originalFilename: file.name,
        sizeBytes: file.size,
        mimeType: file.type || 'image/*',
        objectUrl: URL.createObjectURL(file),
        file,
      }));

    if (imageDrafts.length === 0) {
      return;
    }

    setDraft((current) => ({
      ...current,
      groups: current.groups.map((group) =>
        group.clientGroupId === clientGroupId ? { ...group, images: [...group.images, ...imageDrafts] } : group,
      ),
    }));
  };

  const removeImage = (clientGroupId: string, imageToRemove: ImageDraft) => {
    URL.revokeObjectURL(imageToRemove.objectUrl);
    setDraft((current) => ({
      ...current,
      groups: current.groups.map((group) =>
        group.clientGroupId === clientGroupId
          ? { ...group, images: group.images.filter((image) => image.clientFileId !== imageToRemove.clientFileId) }
          : group,
      ),
    }));
  };

  const stopPolling = (nextState: PollingState = 'stopped') => {
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    pollAbortRef.current?.abort();
    pollAbortRef.current = null;
    pollStartedAtRef.current = null;
    setPollingState((current) => (current === 'polling' ? nextState : current));
  };

  const refreshSelectedContainerSummary = async () => {
    if (draft.noContainer || !draft.selectedContainerId) {
      return;
    }

    try {
      const summary = await getContainerSummary(draft.selectedContainerId);
      setContainerSummary(summary);
      setContainerSummaryError(null);
    } catch (error) {
      setContainerSummaryError(errorMessage(error, 'Failed to refresh selected container summary.'));
    }
  };

  const scheduleStatusPoll = (uploadSessionId: string, delayMs: number) => {
    if (pollTimerRef.current !== null) {
      window.clearTimeout(pollTimerRef.current);
    }
    pollTimerRef.current = window.setTimeout(() => {
      void pollUploadStatus(uploadSessionId);
    }, delayMs);
  };

  const startPolling = (uploadSessionId: string) => {
    stopPolling('idle');
    pollStartedAtRef.current = Date.now();
    setPollingError(null);
    setPollingState('polling');
    scheduleStatusPoll(uploadSessionId, 0);
  };

  const pollUploadStatus = async (uploadSessionId: string) => {
    pollTimerRef.current = null;
    const startedAt = pollStartedAtRef.current;
    if (startedAt === null) {
      return;
    }

    if (Date.now() - startedAt > pollingTimeoutMs) {
      setPollingState('timeout');
      setPollingError('Status polling timed out after 5 minutes. The upload may still be processing in the background. You can check Review or refresh this session later.');
      return;
    }

    const controller = new AbortController();
    pollAbortRef.current = controller;

    try {
      const status = await getUploadSession(uploadSessionId, controller.signal);
      setSessionStatus(status);
      setPollingError(null);

      if (terminalSessionStatuses.has(status.status)) {
        setPollingState('complete');
        pollAbortRef.current = null;
        pollStartedAtRef.current = null;
        void refreshSelectedContainerSummary();
        setShowUploadCompleteModal(true);
        return;
      }

      scheduleStatusPoll(uploadSessionId, pollingIntervalMs);
    } catch (error) {
      if (controller.signal.aborted) {
        return;
      }
      setPollingState('error');
      setPollingError(errorMessage(error, 'Failed to load upload session status.'));
    } finally {
      if (pollAbortRef.current === controller) {
        pollAbortRef.current = null;
      }
    }
  };

  const handleUpload = async () => {
    if (!canUpload) {
      return;
    }

    const payload = buildPayload();
    setPayloadPreview(payload);
    setUploadResult(null);
    setSessionStatus(null);
    setUploadError(null);
    setPollingError(null);
    setShowUploadCompleteModal(false);
    stopPolling('idle');
    setUploadState('uploading');

    try {
      const result = await uploadGroupedImages(payload, draft.groups);
      setUploadResult(result);
      setSessionStatus(uploadResponseToSessionStatus(result));
      setUploadState('success');
      startPolling(result.upload_session_id);
    } catch (error) {
      setUploadError(errorMessage(error, 'Upload failed.'));
      setUploadState('error');
    }
  };

  const resetUploadSession = () => {
    stopPolling('idle');
    imagesRef.current.forEach((image) => URL.revokeObjectURL(image.objectUrl));
    imagesRef.current = [];
    setDraft({
      ...createInitialDraft(nextItemNumberInfo.suggested_title),
    });
    setPayloadPreview(null);
    setUploadResult(null);
    setSessionStatus(null);
    setUploadError(null);
    setPollingError(null);
    setPollingState('idle');
    setUploadState('idle');
    setShowUploadCompleteModal(false);
    void clearUploadDraft();
  };

  const handleResetClick = () => {
    if (!hasDirtyDraft || window.confirm('Clear this upload page and discard selected photos, inventory items, and form data?')) {
      resetUploadSession();
    }
  };

  const handleUploadCompleteOk = () => {
    resetUploadSession();
  };

  return (
    <div className="grid gap-6">
      <div className="flex justify-end">
        <button
          type="button"
          onClick={handleResetClick}
          disabled={uploadState === 'uploading'}
          className="rounded-md border border-rack-steel/45 px-4 py-2 text-sm font-semibold text-stone-200 transition hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Reset
        </button>
      </div>

      <Panel title="Intake context" eyebrow="Session source">
        <IntakeContextPanel
          containers={containers}
          locations={locations}
          inventoryGroups={inventoryGroups}
          isLoadingContainers={isLoadingContainers}
          isLoadingLocations={isLoadingLocations}
          isLoadingInventoryGroups={isLoadingInventoryGroups}
          containerLoadError={containerLoadError}
          locationsLoadError={locationsLoadError}
          inventoryGroupsLoadError={inventoryGroupsLoadError}
          selectedContainerId={draft.selectedContainerId}
          selectedInventoryGroupId={draft.inventoryGroupId}
          selectedLocationId={draft.locationId}
          noContainer={draft.noContainer}
          locationDetail={draft.locationDetail}
          sessionNotes={draft.sessionNotes}
          onSelectContainer={(containerId) =>
            setDraft((current) => ({
              ...current,
              selectedContainerId: containerId,
              noContainer: false,
              locationId: null,
              locationDetail: '',
            }))
          }
          onSelectInventoryGroup={(inventoryGroupId) => setDraft((current) => ({ ...current, inventoryGroupId }))}
          onSetNoContainer={() =>
            setDraft((current) => ({
              ...current,
              selectedContainerId: null,
              noContainer: true,
            }))
          }
          onSelectLocation={(locationId) => setDraft((current) => ({ ...current, locationId }))}
          onLocationDetailChange={(locationDetail) => setDraft((current) => ({ ...current, locationDetail }))}
          onSessionNotesChange={(sessionNotes) => setDraft((current) => ({ ...current, sessionNotes }))}
        />
      </Panel>

      <Panel
        title="Inventory items"
        eyebrow="Inventory item photo cards"
        action={
          <button
            type="button"
            onClick={addGroup}
            className="rounded-md border border-amberline-300/35 bg-[linear-gradient(180deg,#f0522a,#8c3729)] px-4 py-2 text-sm font-semibold text-stone-50 shadow-glow transition hover:border-amberline-300/60 hover:brightness-110"
          >
            Add Inventory Item
          </button>
        }
      >
        <div className="grid gap-4">
          {draft.groups.map((group) => (
            <ItemGroupCard
              key={group.clientGroupId}
              group={group}
              canRemove={draft.groups.length > 1}
              onUpdate={(patch) => updateGroup(group.clientGroupId, patch)}
              onAddImages={(files) => addImagesToGroup(group.clientGroupId, files)}
              onRemoveImage={(image) => removeImage(group.clientGroupId, image)}
              onRemoveGroup={() => removeGroup(group.clientGroupId)}
            />
          ))}
        </div>
      </Panel>

      <section className="grid gap-6 lg:grid-cols-[0.8fr_1.2fr]">
        <Panel title="Upload readiness" eyebrow="Validation">
          {readinessMessages.length === 0 ? (
            <div className="rounded-md border border-signal-green/35 bg-signal-green/10 px-4 py-4 text-sm text-green-100">
              Session is ready to upload.
            </div>
          ) : (
            <ul className="space-y-2 rounded-md border border-rack-trim/70 bg-graphite-950/70 px-4 py-4 text-sm text-stone-300">
              {readinessMessages.map((message) => (
                <li key={message} className="flex gap-2">
                  <span className="mt-1 h-2 w-2 shrink-0 rounded-full bg-signal-red shadow-[0_0_12px_rgba(255,59,36,0.75)]" />
                  <span>{message}</span>
                </li>
              ))}
            </ul>
          )}

          <button
            type="button"
            onClick={handleUpload}
            disabled={!canUpload}
            className="mt-4 w-full rounded-md border border-amberline-300/35 bg-[linear-gradient(180deg,#f0522a,#8c3729)] px-4 py-3 text-sm font-bold text-stone-50 shadow-glow transition enabled:hover:border-amberline-300/60 enabled:hover:brightness-110 disabled:cursor-not-allowed disabled:border-stone-700 disabled:bg-none disabled:bg-stone-700 disabled:text-stone-400 disabled:shadow-none"
          >
            {uploadState === 'uploading' ? 'Uploading...' : 'Upload session'}
          </button>
        </Panel>

        <Panel title="Status" eyebrow="FastSell API">
          {pollingState === 'polling' ? (
            <p className="mb-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 px-3 py-2 text-sm text-stone-300">
              Large uploads can take several minutes to finish processing.
            </p>
          ) : null}
          <StatusPanel
            status={uploadState}
            session={sessionStatus}
            error={uploadError}
            pollingState={pollingState}
            pollingError={pollingError}
            onStopPolling={() => stopPolling('stopped')}
            onReset={handleResetClick}
          />
        </Panel>
      </section>

      {!draft.noContainer && draft.selectedContainerId ? (
        <Panel title="Selected container" eyebrow="Read-only context">
          <ContainerSummaryPanel summary={containerSummary} isLoading={isLoadingContainerSummary} error={containerSummaryError} />
        </Panel>
      ) : null}

      <Panel title="Metadata payload" eyebrow="JSON preview">
        <JsonPreview payload={payloadPreview} emptyText="Upload a session to preview the metadata payload." />
      </Panel>

      <Panel title="API response" eyebrow={API_BASE_URL}>
        <JsonPreview payload={sessionStatus ?? uploadResult} emptyText="Successful upload responses will appear here." />
      </Panel>

      {showUploadCompleteModal ? <UploadCompleteModal onOk={handleUploadCompleteOk} /> : null}
    </div>
  );
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

function isSelectableImage(file: File): boolean {
  if (file.type.startsWith('image/')) {
    return true;
  }

  return /\.(jpe?g|png|heic|heif)$/i.test(file.name);
}

function uploadResponseToSessionStatus(response: UploadImageResponse): UploadSessionStatusResponse {
  return {
    upload_session_id: response.upload_session_id,
    status: response.status,
    groups: response.groups.map((group, groupIndex) => ({
      upload_group_id: group.upload_group_id,
      client_group_id: group.client_group_id,
      inventory_group_id: group.inventory_group_id,
      inventory_group_code: null,
      inventory_group_name: null,
      title: group.title,
      notes: null,
      sort_order: groupIndex,
      files: group.files.map((file, fileIndex) => ({
        image_asset_id: file.image_asset_id,
        client_file_id: file.client_file_id,
        original_filename: file.original_filename,
        stored_filename: file.stored_filename,
        file_path: '',
        mime_type: null,
        file_size_bytes: null,
        upload_order: fileIndex,
        status: file.status,
        error_message: null,
      })),
    })),
  };
}

function nextItemNumberInfoAfterApplying(
  response: NextUploadItemNumberResponse,
  groups: ItemGroupDraft[],
): NextUploadItemNumberResponse {
  const eligibleGroupCount = groups.filter(isEligibleForAutoTitleRefresh).length;
  const nextNumber = response.next_number + eligibleGroupCount * response.step;
  return {
    ...response,
    next_number: nextNumber,
    suggested_title: `${response.title_prefix} ${nextNumber}`,
  };
}

function applySuggestedTitlesToGroups(groups: ItemGroupDraft[], response: NextUploadItemNumberResponse): ItemGroupDraft[] {
  let nextNumber = response.next_number;

  return groups.map((group) => {
    if (!isEligibleForAutoTitleRefresh(group)) {
      return group;
    }

    const updatedGroup = {
      ...group,
      title: `${response.title_prefix} ${nextNumber}`,
    };
    nextNumber += response.step;
    return updatedGroup;
  });
}

function isEligibleForAutoTitleRefresh(group: ItemGroupDraft): boolean {
  return group.autoTitle && group.images.length === 0 && group.notes.trim() === '';
}

function ensureDraftHasGroup(draft: UploadSessionDraft): UploadSessionDraft {
  if (draft.groups.length > 0) {
    return draft;
  }

  return {
    ...draft,
    groups: [createInitialGroup()],
  };
}

function isPersistableDraft(draft: UploadSessionDraft): boolean {
  return (
    Boolean(
      draft.selectedContainerId ||
        draft.noContainer ||
        draft.inventoryGroupId ||
        draft.locationId ||
        draft.locationDetail.trim() ||
        draft.sessionNotes.trim(),
    ) ||
    draft.groups.length !== 1 ||
    draft.groups.some((group) => group.images.length > 0 || group.notes.trim() || !group.autoTitle)
  );
}

function UploadCompleteModal({ onOk }: { onOk: () => void }) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 px-4 py-6" role="dialog" aria-modal="true" aria-labelledby="upload-complete-title">
      <div className="w-full max-w-md rounded-md border border-amberline-300/35 bg-graphite-950 p-5 shadow-glow">
        <div className="flex items-center gap-3">
          <span className="h-2.5 w-2.5 rounded-full bg-signal-green shadow-[0_0_16px_rgba(139,207,139,0.72)]" />
          <h2 id="upload-complete-title" className="text-lg font-semibold text-stone-100">
            Upload Complete
          </h2>
        </div>
        <p className="mt-3 text-sm text-stone-300">
          Upload processing has finished. The page will reset for the next batch.
        </p>
        <div className="mt-5 flex justify-end">
          <button
            type="button"
            onClick={onOk}
            className="rounded-md border border-amberline-300/35 bg-[linear-gradient(180deg,#f0522a,#8c3729)] px-4 py-2 text-sm font-semibold text-stone-50 shadow-glow transition hover:border-amberline-300/60 hover:brightness-110"
          >
            OK
          </button>
        </div>
      </div>
    </div>
  );
}
