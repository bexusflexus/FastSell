import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '../api/client';
import { imageUrl } from '../api/images';
import { listInventoryGroups } from '../api/inventoryGroups';
import { approveAllReviewUploadGroup, approveReviewUploadGroup, deleteReviewGroup, deleteReviewGroupImage, getReviewGroupDeletePreview, getReviewUploadGroup, listReviewUploadGroups, queueReviewUploadGroupAIAssist, uploadReviewGroupImages } from '../api/review';
import {
  addWholeSceneCandidate,
  assistWholeSceneCandidate,
  approveWholeSceneCandidate,
  deleteWholeSceneCandidateImage,
  deleteWholeSceneScan,
  getWholeSceneScan,
  listWholeSceneReviewScans,
  patchWholeSceneCandidate,
  queueWholeSceneAnalysis,
  rejectWholeSceneCandidate,
  uploadWholeSceneCandidateImages,
} from '../api/wholeScene';
import { Panel } from '../components/Panel';
import { formatBytes } from '../utils/formatBytes';
import type {
  ApproveUploadGroupInput,
  ApproveUploadGroupResponse,
  ReviewGroupDeletePreview,
  ReviewGroupDeleteResponse,
  ReviewImageDeleteResponse,
  ReviewImageAsset,
  ReviewUploadGroup,
} from '../types/review';
import type { InventoryGroup } from '../types/inventoryGroups';
import type { ItemImageUploadEntry } from '../types/items';
import type {
  AddWholeSceneCandidateInput,
  ApproveWholeSceneCandidateInput,
  PatchWholeSceneCandidateInput,
  AssistWholeSceneCandidateInput,
  WholeSceneCandidate,
  WholeSceneCandidateCrop,
  WholeSceneCandidateImageDeleteResponse,
  WholeSceneImageAsset,
  WholeSceneCandidateMutationResponse,
  WholeSceneReviewScanSummary,
  WholeSceneScan,
} from '../types/wholeScene';

type PreviewImageAsset = Pick<ReviewImageAsset, 'image_asset_id' | 'original_filename' | 'stored_filename' | 'mime_type' | 'file_size_bytes' | 'status' | 'upload_order'>
  | Pick<WholeSceneImageAsset, 'image_asset_id' | 'original_filename' | 'stored_filename' | 'mime_type' | 'file_size_bytes' | 'status' | 'upload_order'>;

interface SelectedImage {
  groupId: string;
  groupTitle: string | null;
  groupClientGroupId: string | null;
  containerName: string | null;
  image: PreviewImageAsset;
}

type ReviewGroupDraft = ApproveUploadGroupInput;
type WholeSceneCandidateDraft = ApproveWholeSceneCandidateInput;

const approxValuePattern = /^\d+(\.\d{1,2})?$/;
const AI_POLL_INTERVAL_MS = 2_000;
const AI_POLL_TIMEOUT_MS = 300_000;
const WHOLE_SCENE_POLL_INTERVAL_MS = 2_500;
const wholeSceneTerminalImageStatuses = new Set(['processed', 'failed']);
const wholeSceneActiveStatuses = new Set(['queued', 'processing']);
type ReviewTab = 'uploads' | 'whole_scene';

export function ReviewPage() {
  const [searchParams] = useSearchParams();
  const requestedTab = useMemo(() => searchParams.get('tab')?.trim() ?? '', [searchParams]);
  const requestedWholeSceneScanId = useMemo(() => searchParams.get('scan_id')?.trim() ?? '', [searchParams]);
  const [activeTab, setActiveTab] = useState<ReviewTab>(requestedTab === 'whole_scene' ? 'whole_scene' : 'uploads');
  const [groups, setGroups] = useState<ReviewUploadGroup[]>([]);
  const [reviewDrafts, setReviewDrafts] = useState<Record<string, ReviewGroupDraft>>({});
  const [isLoading, setIsLoading] = useState(true);
  const [wholeSceneScans, setWholeSceneScans] = useState<WholeSceneReviewScanSummary[]>([]);
  const [wholeSceneCandidateDrafts, setWholeSceneCandidateDrafts] = useState<Record<string, WholeSceneCandidateDraft>>({});
  const [isWholeSceneLoading, setIsWholeSceneLoading] = useState(true);
  const [selectedWholeSceneScan, setSelectedWholeSceneScan] = useState<WholeSceneScan | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [wholeSceneError, setWholeSceneError] = useState<string | null>(null);
  const [wholeSceneCandidateErrors, setWholeSceneCandidateErrors] = useState<Record<string, string>>({});
  const [wholeSceneAIBusyIds, setWholeSceneAIBusyIds] = useState<Set<string>>(() => new Set());
  const [message, setMessage] = useState<string | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<ReviewUploadGroup | null>(null);
  const [deletePreview, setDeletePreview] = useState<ReviewGroupDeletePreview | null>(null);
  const [deletePreviewError, setDeletePreviewError] = useState<string | null>(null);
  const [wholeSceneDeleteTarget, setWholeSceneDeleteTarget] = useState<WholeSceneScan | null>(null);
  const [wholeSceneDeleteError, setWholeSceneDeleteError] = useState<string | null>(null);
  const [selectedImage, setSelectedImage] = useState<SelectedImage | null>(null);
  const [selectedImageFailed, setSelectedImageFailed] = useState(false);
  const [inventoryGroups, setInventoryGroups] = useState<InventoryGroup[]>([]);
  const [inventoryGroupsError, setInventoryGroupsError] = useState<string | null>(null);
  const [isInventoryGroupsLoading, setIsInventoryGroupsLoading] = useState(false);

  const containerId = useMemo(() => {
    const value = searchParams.get('container_id')?.trim() ?? '';
    return value || null;
  }, [searchParams]);

  const handleReviewDraftChange = useCallback((groupId: string, draft: ReviewGroupDraft) => {
    setReviewDrafts((current) => ({
      ...current,
      [groupId]: draft,
    }));
  }, []);

  const handleWholeSceneCandidateDraftChange = useCallback((candidateId: string, draft: WholeSceneCandidateDraft) => {
    setWholeSceneCandidateDrafts((current) => ({
      ...current,
      [candidateId]: draft,
    }));
  }, []);

  useEffect(() => {
    let isMounted = true;

    const loadGroups = async () => {
      setIsLoading(true);
      setError(null);

      try {
        const response = await listReviewUploadGroups({ containerId: containerId ?? undefined });
        if (isMounted) {
          setGroups(response.groups);
          setReviewDrafts({});
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load review queue', err);
          setError(errorMessage(err, 'Failed to load review queue.'));
        }
      } finally {
        if (isMounted) {
          setIsLoading(false);
        }
      }
    };

    void loadGroups();

    return () => {
      isMounted = false;
    };
  }, [containerId]);

  useEffect(() => {
    let isMounted = true;

    const loadScans = async () => {
      setIsWholeSceneLoading(true);
      setWholeSceneError(null);

      try {
        const response = await listWholeSceneReviewScans({ containerId: containerId ?? undefined });
        if (isMounted) {
          setWholeSceneScans(response.scans);
          if (selectedWholeSceneScan && !response.scans.some((scan) => scan.id === selectedWholeSceneScan.id)) {
            setSelectedWholeSceneScan(null);
          }
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load Whole Scene review scans', err);
          setWholeSceneError(errorMessage(err, 'Failed to load Whole Scene scans.'));
        }
      } finally {
        if (isMounted) {
          setIsWholeSceneLoading(false);
        }
      }
    };

    void loadScans();

    return () => {
      isMounted = false;
    };
  }, [containerId, selectedWholeSceneScan?.id]);

  useEffect(() => {
    if (requestedTab === 'whole_scene') {
      setActiveTab('whole_scene');
    }
  }, [requestedTab]);

  useEffect(() => {
    let isMounted = true;

    const loadGroups = async () => {
      setIsInventoryGroupsLoading(true);
      setInventoryGroupsError(null);
      try {
        const response = await listInventoryGroups();
        if (isMounted) {
          setInventoryGroups(response);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load inventory groups', err);
          setInventoryGroupsError(errorMessage(err, 'Failed to load inventory groups.'));
        }
      } finally {
        if (isMounted) {
          setIsInventoryGroupsLoading(false);
        }
      }
    };

    void loadGroups();

    return () => {
      isMounted = false;
    };
  }, []);

  useEffect(() => {
    if (!selectedImage) {
      setSelectedImageFailed(false);
      return;
    }

    setSelectedImageFailed(false);

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setSelectedImage(null);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [selectedImage]);

  useEffect(() => {
    if (!requestedWholeSceneScanId || selectedWholeSceneScan?.id === requestedWholeSceneScanId) {
      return;
    }

    let isMounted = true;
    setActiveTab('whole_scene');
    setBusyAction(`whole-scene-open:${requestedWholeSceneScanId}`);
    setWholeSceneError(null);

    const loadRequestedScan = async () => {
      try {
        const scan = await getWholeSceneScan(requestedWholeSceneScanId);
        if (isMounted) {
          setSelectedWholeSceneScan(scan);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load requested Whole Scene scan', err);
          setWholeSceneError(errorMessage(err, 'Failed to load Whole Scene scan.'));
        }
      } finally {
        if (isMounted) {
          setBusyAction(null);
        }
      }
    };

    void loadRequestedScan();

    return () => {
      isMounted = false;
    };
  }, [requestedWholeSceneScanId, selectedWholeSceneScan?.id]);

  useEffect(() => {
    if (
      activeTab !== 'whole_scene' ||
      !selectedWholeSceneScan ||
      busyAction?.startsWith('whole-scene-delete:') ||
      !shouldPollWholeSceneScan(selectedWholeSceneScan)
    ) {
      return;
    }

    let cancelled = false;
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const [scanListResponse, scan] = await Promise.all([
            listWholeSceneReviewScans({ containerId: containerId ?? undefined }),
            getWholeSceneScan(selectedWholeSceneScan.id),
          ]);
          if (cancelled) {
            return;
          }
          setWholeSceneScans(scanListResponse.scans);
          setSelectedWholeSceneScan(scan);
        } catch (err) {
          if (!cancelled) {
            console.error('Failed to refresh Whole Scene scan', err);
            setWholeSceneError(errorMessage(err, 'Failed to refresh Whole Scene scan.'));
          }
        }
      })();
    }, WHOLE_SCENE_POLL_INTERVAL_MS);

    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [activeTab, busyAction, containerId, selectedWholeSceneScan]);

  const handleApprove = async (groupId: string, input: ApproveUploadGroupInput): Promise<ApproveUploadGroupResponse> => {
    setBusyAction(`approve:${groupId}`);
    try {
      const response = await approveReviewUploadGroup(groupId, input);
      setGroups((current) => current.filter((group) => group.upload_group_id !== groupId));
      setReviewDrafts((current) => {
        const next = { ...current };
        delete next[groupId];
        return next;
      });
      setMessage(`Created item "${response.item.title ?? input.title ?? 'Untitled'}" with ${response.linked_image_count} images.`);
      setError(null);
      if (selectedImage?.groupId === groupId) {
        setSelectedImage(null);
      }
      return response;
    } finally {
      setBusyAction(null);
    }
  };

  const handleApproveAll = async () => {
    if (groups.length === 0) {
      return;
    }

    setBusyAction('approve-all');
    setError(null);
    setMessage(null);
    let approvedCount = 0;
    try {
      for (const group of groups) {
        await approveAllReviewUploadGroup(group.upload_group_id, reviewDrafts[group.upload_group_id] ?? defaultApproveInput(group));
        approvedCount += 1;
      }
      setGroups([]);
      setReviewDrafts({});
      setSelectedImage(null);
      setMessage(`Approved ${approvedCount} upload group${approvedCount === 1 ? '' : 's'} into inventory.`);
    } catch (err) {
      console.error('Failed to approve all review groups', err);
      setError(errorMessage(err, `Approved ${approvedCount} group${approvedCount === 1 ? '' : 's'} before a failure.`));
      const response = await listReviewUploadGroups({ containerId: containerId ?? undefined });
      setGroups(response.groups);
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDelete = async (group: ReviewUploadGroup) => {
    setDeleteTarget(group);
    setDeletePreview(null);
    setDeletePreviewError(null);
    setBusyAction(`delete-preview:${group.upload_group_id}`);
    try {
      const preview = await getReviewGroupDeletePreview(group.upload_group_id);
      setDeletePreview(preview);
    } catch (err) {
      console.error('Failed to load review discard preview', err);
      setDeletePreviewError(errorMessage(err, 'Failed to load review discard preview.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleConfirmDelete = async (): Promise<ReviewGroupDeleteResponse | null> => {
    if (!deleteTarget) {
      return null;
    }

    setBusyAction(`delete:${deleteTarget.upload_group_id}`);
    setDeletePreviewError(null);
    try {
      const response = await deleteReviewGroup(deleteTarget.upload_group_id);
      setGroups((current) => current.filter((group) => group.upload_group_id !== deleteTarget.upload_group_id));
      setDeleteTarget(null);
      setDeletePreview(null);
      const warningNote = response.warnings.length > 0 ? ` Warnings: ${response.warnings.join(' ')}` : '';
      setMessage(`Discarded "${deleteTarget.title ?? deleteTarget.client_group_id ?? 'Untitled group'}". Removed ${response.deleted_image_asset_count} image rows and ${response.deleted_file_count} files.${warningNote}`);
      setError(null);
      if (selectedImage?.groupId === deleteTarget.upload_group_id) {
        setSelectedImage(null);
      }
      return response;
    } catch (err) {
      console.error('Failed to discard review group', err);
      setDeletePreviewError(errorMessage(err, 'Failed to discard review group.'));
      return null;
    } finally {
      setBusyAction(null);
    }
  };

  const handleGroupUpdated = (groupId: string, updatedGroup: ReviewUploadGroup | null) => {
    setGroups((current) => {
      if (updatedGroup == null) {
        return current.filter((group) => group.upload_group_id !== groupId);
      }
      return current.map((group) => (group.upload_group_id === groupId ? updatedGroup : group));
    });
  };

  const refreshWholeSceneScans = async (selectedScanId?: string) => {
    const response = await listWholeSceneReviewScans({ containerId: containerId ?? undefined });
    setWholeSceneScans(response.scans);
    if (selectedScanId) {
      const scan = await getWholeSceneScan(selectedScanId);
      setSelectedWholeSceneScan(scan);
    }
    return response.scans;
  };

  const handleContinueWholeSceneReview = async (scanId: string) => {
    setBusyAction(`whole-scene-open:${scanId}`);
    setWholeSceneError(null);
    try {
      const scan = await getWholeSceneScan(scanId);
      setSelectedWholeSceneScan(scan);
      setActiveTab('whole_scene');
    } catch (err) {
      console.error('Failed to load Whole Scene scan', err);
      setWholeSceneError(errorMessage(err, 'Failed to load Whole Scene scan.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleQueueWholeSceneAnalysis = async (scanId: string) => {
    setBusyAction(`whole-scene-analyze:${scanId}`);
    setWholeSceneError(null);
    try {
      const response = await queueWholeSceneAnalysis(scanId);
      setSelectedWholeSceneScan(response.scan);
      await refreshWholeSceneScans(scanId);
      setMessage(response.queued ? 'Whole Scene analysis queued.' : 'Whole Scene analysis is already queued or processing.');
    } catch (err) {
      console.error('Failed to queue Whole Scene analysis', err);
      setWholeSceneError(errorMessage(err, 'Failed to queue Whole Scene analysis.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDeleteWholeSceneScan = (scan: WholeSceneScan) => {
    setWholeSceneDeleteTarget(scan);
    setWholeSceneDeleteError(null);
  };

  const handleConfirmDeleteWholeSceneScan = async () => {
    if (!wholeSceneDeleteTarget) {
      return;
    }

    const scanId = wholeSceneDeleteTarget.id;
    setBusyAction(`whole-scene-delete:${scanId}`);
    setWholeSceneDeleteError(null);
    setWholeSceneError(null);
    try {
      const response = await deleteWholeSceneScan(scanId);
      setSelectedWholeSceneScan(null);
      setWholeSceneDeleteTarget(null);
      setWholeSceneCandidateErrors({});
      setWholeSceneCandidateDrafts({});
      setWholeSceneAIBusyIds(new Set());
      await refreshWholeSceneScans();
      const cleanup = response.cleanup;
      setMessage(`Deleted Whole Scene scan. Removed ${cleanup.deleted_image_asset_count} temporary image asset${cleanup.deleted_image_asset_count === 1 ? '' : 's'} and ${cleanup.deleted_file_count} file${cleanup.deleted_file_count === 1 ? '' : 's'}.`);
    } catch (err) {
      console.error('Failed to delete Whole Scene scan', err);
      setWholeSceneDeleteError(errorMessage(err, 'Failed to delete Whole Scene scan.'));
    } finally {
      setBusyAction(null);
    }
  };

  const runWholeSceneMutation = async (action: () => Promise<WholeSceneCandidateMutationResponse>, successMessage: string, candidateErrorId?: string) => {
    setWholeSceneError(null);
    if (candidateErrorId) {
      setWholeSceneCandidateErrors((current) => {
        if (!(candidateErrorId in current)) {
          return current;
        }
        const next = { ...current };
        delete next[candidateErrorId];
        return next;
      });
    }
    try {
      const response = await action();
      if (response.cleaned_up) {
        setSelectedWholeSceneScan(null);
        await refreshWholeSceneScans();
        const itemNote = response.approved_item_id ? ` Approved item: ${response.approved_item_id}.` : '';
        setMessage(`Whole Scene scan review complete. Temporary scan staging data was cleaned up.${itemNote}`);
        return;
      }
      if (response.scan) {
        setSelectedWholeSceneScan(response.scan);
        await refreshWholeSceneScans(response.scan.id);
      } else {
        await refreshWholeSceneScans(response.scan_id);
      }
      setMessage(successMessage);
    } catch (err) {
      console.error('Whole Scene review action failed', err);
      const messageValue = errorMessage(err, 'Whole Scene review action failed.');
      if (candidateErrorId) {
        setWholeSceneCandidateErrors((current) => ({
          ...current,
          [candidateErrorId]: messageValue,
        }));
      } else {
        setWholeSceneError(messageValue);
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handlePatchWholeSceneCandidate = (candidateId: string, input: PatchWholeSceneCandidateInput) => {
    if (!selectedWholeSceneScan) {
      return;
    }
    setBusyAction(`whole-scene-patch:${candidateId}`);
    void runWholeSceneMutation(
      () => patchWholeSceneCandidate(selectedWholeSceneScan.id, candidateId, input),
      'Whole Scene candidate updated.',
    );
  };

  const handleRejectWholeSceneCandidate = (candidateId: string) => {
    if (!selectedWholeSceneScan) {
      return;
    }
    setBusyAction(`whole-scene-reject:${candidateId}`);
    void runWholeSceneMutation(
      () => rejectWholeSceneCandidate(selectedWholeSceneScan.id, candidateId),
      'Whole Scene candidate rejected.',
    );
  };

  const handleApproveWholeSceneCandidate = (candidateId: string, input: ApproveWholeSceneCandidateInput) => {
    if (!selectedWholeSceneScan) {
      return;
    }
    setBusyAction(`whole-scene-approve:${candidateId}`);
    void runWholeSceneMutation(
      () => approveWholeSceneCandidate(selectedWholeSceneScan.id, candidateId, input),
      'Whole Scene candidate approved into inventory.',
      candidateId,
    );
    setWholeSceneCandidateDrafts((current) => {
      if (!(candidateId in current)) {
        return current;
      }
      const next = { ...current };
      delete next[candidateId];
      return next;
    });
  };

  const handleApproveAllWholeSceneCandidates = async () => {
    if (!selectedWholeSceneScan) {
      return;
    }

    const scan = selectedWholeSceneScan;
    const candidates = scan.candidates.filter(isPendingWholeSceneCandidate);
    if (candidates.length === 0) {
      return;
    }

    setBusyAction(`whole-scene-approve-all:${scan.id}`);
    setWholeSceneError(null);
    setMessage(null);

    let approvedCount = 0;
    let failedCandidate: WholeSceneCandidate | null = null;
    let latestResponse: WholeSceneCandidateMutationResponse | null = null;

    try {
      for (const candidate of candidates) {
        failedCandidate = candidate;
        const draft = wholeSceneCandidateDrafts[candidate.id];
        latestResponse = await approveWholeSceneCandidate(scan.id, candidate.id, {
          inventory_group_id: draft?.inventory_group_id ?? scan.inventory_group.id,
        });
        approvedCount += 1;
        setWholeSceneCandidateDrafts((current) => {
          if (!(candidate.id in current)) {
            return current;
          }
          const next = { ...current };
          delete next[candidate.id];
          return next;
        });

        if (latestResponse.cleaned_up) {
          break;
        }
      }

      setSelectedImage(null);
      if (latestResponse?.cleaned_up) {
        setSelectedWholeSceneScan(null);
        await refreshWholeSceneScans();
      } else if (latestResponse?.scan) {
        setSelectedWholeSceneScan(latestResponse.scan);
        await refreshWholeSceneScans(latestResponse.scan.id);
      } else {
        await refreshWholeSceneScans(scan.id);
      }
      setMessage(`Accepted ${approvedCount} Whole Scene candidate${approvedCount === 1 ? '' : 's'} into inventory.`);
    } catch (err) {
      console.error('Failed to accept all Whole Scene candidates', err);
      const failedTitle = failedCandidate?.title?.trim() || 'candidate';
      const failureMessage = errorMessage(err, 'Whole Scene Accept All failed.');
      setWholeSceneError(`Accepted ${approvedCount} candidate${approvedCount === 1 ? '' : 's'} before failing on ${failedTitle}. ${failureMessage}`);
      try {
        await refreshWholeSceneScans(scan.id);
      } catch (refreshErr) {
        console.error('Failed to refresh Whole Scene scan after accept all failure', refreshErr);
      }
    } finally {
      setBusyAction(null);
    }
  };

  const handleUploadWholeSceneCandidateImages = async (
    candidateId: string,
    files: File[],
    onProgress?: (entries: ItemImageUploadEntry[]) => void,
  ): Promise<WholeSceneCandidateMutationResponse> => {
    if (!selectedWholeSceneScan) {
      throw new Error('No Whole Scene scan is selected.');
    }
    const response = await uploadWholeSceneCandidateImages(selectedWholeSceneScan.id, candidateId, files, onProgress);
    if (response.scan) {
      setSelectedWholeSceneScan(response.scan);
      await refreshWholeSceneScans(response.scan.id);
    } else {
      await refreshWholeSceneScans(response.scan_id);
    }
    setMessage(`Added ${files.length} image${files.length === 1 ? '' : 's'} to Whole Scene candidate.`);
    return response;
  };

  const handleDeleteWholeSceneCandidateImage = async (candidateId: string, cropId: string): Promise<WholeSceneCandidateImageDeleteResponse> => {
    if (!selectedWholeSceneScan) {
      throw new Error('No Whole Scene scan is selected.');
    }
    const response = await deleteWholeSceneCandidateImage(selectedWholeSceneScan.id, candidateId, cropId);
    if (response.scan) {
      setSelectedWholeSceneScan(response.scan);
      await refreshWholeSceneScans(response.scan.id);
    } else {
      await refreshWholeSceneScans(response.scan_id);
    }
    setMessage('Removed image from Whole Scene candidate.');
    return response;
  };

  const pollWholeSceneCandidateAIAssist = async (scanId: string, candidateId: string) => {
    const startedAt = Date.now();
    for (;;) {
      if (Date.now() - startedAt >= AI_POLL_TIMEOUT_MS) {
        setWholeSceneCandidateErrors((current) => ({
          ...current,
          [candidateId]: 'AI Assist timed out after 5 minutes. The background job may still finish if you refresh the scan.',
        }));
        break;
      }

      await delay(AI_POLL_INTERVAL_MS);
      const scan = await getWholeSceneScan(scanId);
      setSelectedWholeSceneScan(scan);
      const candidate = scan.candidates.find((entry) => entry.id === candidateId);
      if (!candidate) {
        setWholeSceneCandidateErrors((current) => ({
          ...current,
          [candidateId]: 'AI Assist candidate was no longer found in this scan.',
        }));
        break;
      }

      if (candidate.ai_assist_status === 'succeeded') {
        setWholeSceneCandidateErrors((current) => {
          if (!(candidateId in current)) {
            return current;
          }
          const next = { ...current };
          delete next[candidateId];
          return next;
        });
        await refreshWholeSceneScans(scanId);
        setMessage('Whole Scene candidate updated by AI Assist.');
        break;
      }

      if (candidate.ai_assist_status === 'failed') {
        setWholeSceneCandidateErrors((current) => ({
          ...current,
          [candidateId]: candidate.ai_assist_error_message || 'AI Assist failed.',
        }));
        await refreshWholeSceneScans(scanId);
        break;
      }
    }
  };

  const handleAssistWholeSceneCandidate = (candidateId: string, input: AssistWholeSceneCandidateInput) => {
    if (!selectedWholeSceneScan) {
      return;
    }
    const scanId = selectedWholeSceneScan.id;
    setWholeSceneAIBusyIds((current) => new Set(current).add(candidateId));
    setWholeSceneCandidateErrors((current) => {
      if (!(candidateId in current)) {
        return current;
      }
      const next = { ...current };
      delete next[candidateId];
      return next;
    });
    setWholeSceneError(null);

    void (async () => {
      try {
        const response = await assistWholeSceneCandidate(scanId, candidateId, input);
        if (response.scan) {
          setSelectedWholeSceneScan(response.scan);
        }
        setMessage('Whole Scene candidate AI Assist queued.');
        await pollWholeSceneCandidateAIAssist(scanId, candidateId);
      } catch (err) {
        console.error('Whole Scene candidate AI Assist failed', err);
        setWholeSceneCandidateErrors((current) => ({
          ...current,
          [candidateId]: errorMessage(err, 'Failed to queue Whole Scene candidate AI Assist.'),
        }));
      } finally {
        setWholeSceneAIBusyIds((current) => {
          const next = new Set(current);
          next.delete(candidateId);
          return next;
        });
      }
    })();
  };

  const handleAddWholeSceneCandidate = (input: AddWholeSceneCandidateInput) => {
    if (!selectedWholeSceneScan) {
      return;
    }
    setBusyAction('whole-scene-add');
    void runWholeSceneMutation(
      () => addWholeSceneCandidate(selectedWholeSceneScan.id, input),
      'Manual Whole Scene candidate added.',
    );
  };

  const totalCount = groups.length;
  const wholeScenePendingCount = wholeSceneScans.reduce((sum, scan) => sum + scan.candidate_counts.pending, 0);

  return (
    <div className="grid gap-6">
      <Panel title="Review" eyebrow="Processed upload groups">
        <div className="grid gap-3">
          <p className="text-sm text-stone-300">
            Review processed upload groups and approve them into inventory items.
          </p>
          {containerId ? (
            <p className="text-sm text-amberline-100">Reviewing uploads for container_id: {containerId}</p>
          ) : null}
          <div className="flex flex-wrap items-center gap-3">
            <span className="rounded-md border border-rack-steel/30 bg-rack-soot/75 px-3 py-2 text-sm text-stone-300">
              Upload groups: {totalCount}
            </span>
            <span className="rounded-md border border-rack-steel/30 bg-rack-soot/75 px-3 py-2 text-sm text-stone-300">
              Whole Scene pending: {wholeScenePendingCount}
            </span>
            {activeTab === 'uploads' && groups.length > 0 ? (
              <button
                type="button"
                onClick={() => void handleApproveAll()}
                disabled={busyAction === 'approve-all'}
                className="rounded-md border border-signal-green/35 px-4 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busyAction === 'approve-all' ? 'Approving All...' : 'Approve All'}
              </button>
            ) : null}
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={() => setActiveTab('uploads')}
              className={`rounded-md border px-4 py-2 text-sm font-semibold ${activeTab === 'uploads' ? 'border-amberline-400/60 bg-amberline-500/10 text-amberline-100' : 'border-rack-steel/30 text-stone-300 hover:bg-rack-steel/12'}`}
            >
              Normal Review
            </button>
            <button
              type="button"
              onClick={() => setActiveTab('whole_scene')}
              className={`rounded-md border px-4 py-2 text-sm font-semibold ${activeTab === 'whole_scene' ? 'border-amberline-400/60 bg-amberline-500/10 text-amberline-100' : 'border-rack-steel/30 text-stone-300 hover:bg-rack-steel/12'}`}
            >
              Whole Scene Review
            </button>
          </div>
          {message ? <p className="rounded-md border border-signal-green/35 bg-signal-green/10 px-3 py-2 text-sm text-green-100">{message}</p> : null}
          {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          {wholeSceneError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{wholeSceneError}</p> : null}
        </div>
      </Panel>

      {activeTab === 'uploads' ? (
        <>
          {isLoading ? <p className="text-sm text-stone-400">Loading review queue...</p> : null}

          {!isLoading && groups.length === 0 ? (
            <Panel title="Review queue" eyebrow="Empty">
              <p className="text-sm text-stone-300">No upload groups are ready for review.</p>
            </Panel>
          ) : null}

          <div className="grid gap-4">
            {groups.map((group, index) => (
              <ReviewGroupCard
                key={group.upload_group_id}
                group={group}
                shortcutsEnabled={index === 0}
                inventoryGroups={inventoryGroups}
                inventoryGroupsLoading={isInventoryGroupsLoading}
                inventoryGroupsError={inventoryGroupsError}
                busyAction={busyAction}
                onApprove={handleApprove}
                onRequestDelete={handleRequestDelete}
                onGroupUpdated={handleGroupUpdated}
                onDraftChange={handleReviewDraftChange}
                onPreviewImage={(image) => setSelectedImage({ ...image })}
              />
            ))}
          </div>
        </>
      ) : (
        <WholeSceneReviewSection
          scans={wholeSceneScans}
          selectedScan={selectedWholeSceneScan}
          isLoading={isWholeSceneLoading}
          busyAction={busyAction}
          candidateErrors={wholeSceneCandidateErrors}
          candidateDrafts={wholeSceneCandidateDrafts}
          aiBusyIds={wholeSceneAIBusyIds}
          inventoryGroups={inventoryGroups}
          inventoryGroupsLoading={isInventoryGroupsLoading}
          inventoryGroupsError={inventoryGroupsError}
          onContinueReview={(scanId) => void handleContinueWholeSceneReview(scanId)}
          onQueueAnalysis={(scanId) => void handleQueueWholeSceneAnalysis(scanId)}
          onRequestDeleteScan={handleRequestDeleteWholeSceneScan}
          onPreviewImage={(image) => setSelectedImage({ ...image })}
          onPatchCandidate={handlePatchWholeSceneCandidate}
          onRejectCandidate={handleRejectWholeSceneCandidate}
          onApproveCandidate={handleApproveWholeSceneCandidate}
          onApproveAllCandidates={() => void handleApproveAllWholeSceneCandidates()}
          onCandidateDraftChange={handleWholeSceneCandidateDraftChange}
          onAssistCandidate={handleAssistWholeSceneCandidate}
          onAddCandidate={handleAddWholeSceneCandidate}
          onUploadCandidateImages={handleUploadWholeSceneCandidateImages}
          onDeleteCandidateImage={handleDeleteWholeSceneCandidateImage}
        />
      )}

      {deleteTarget ? (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6">
          <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto">
            <ReviewDeleteConfirmationPanel
              group={deleteTarget}
              preview={deletePreview}
              error={deletePreviewError}
              isLoading={busyAction === `delete-preview:${deleteTarget.upload_group_id}`}
              isDeleting={busyAction === `delete:${deleteTarget.upload_group_id}`}
              onCancel={() => {
                setDeleteTarget(null);
                setDeletePreview(null);
                setDeletePreviewError(null);
              }}
              onConfirm={() => void handleConfirmDelete()}
            />
          </div>
        </div>
      ) : null}

      {wholeSceneDeleteTarget ? (
        <WholeSceneDeleteConfirmationPanel
          scan={wholeSceneDeleteTarget}
          error={wholeSceneDeleteError}
          isDeleting={busyAction === `whole-scene-delete:${wholeSceneDeleteTarget.id}`}
          onCancel={() => {
            setWholeSceneDeleteTarget(null);
            setWholeSceneDeleteError(null);
          }}
          onConfirm={() => void handleConfirmDeleteWholeSceneScan()}
        />
      ) : null}

      {selectedImage ? (
        <ImageModal
          imageUrlValue={imageUrl(selectedImage.image.image_asset_id)}
          image={selectedImage.image}
          groupTitle={selectedImage.groupTitle}
          groupClientGroupId={selectedImage.groupClientGroupId}
          containerName={selectedImage.containerName}
          failed={selectedImageFailed}
          onClose={() => setSelectedImage(null)}
          onImageError={() => setSelectedImageFailed(true)}
          onBackdropClick={() => setSelectedImage(null)}
        />
      ) : null}
    </div>
  );
}

function WholeSceneReviewSection({
  scans,
  selectedScan,
  isLoading,
  busyAction,
  candidateErrors,
  candidateDrafts,
  aiBusyIds,
  inventoryGroups,
  inventoryGroupsLoading,
  inventoryGroupsError,
  onContinueReview,
  onQueueAnalysis,
  onRequestDeleteScan,
  onPreviewImage,
  onPatchCandidate,
  onRejectCandidate,
  onApproveCandidate,
  onApproveAllCandidates,
  onCandidateDraftChange,
  onAssistCandidate,
  onAddCandidate,
  onUploadCandidateImages,
  onDeleteCandidateImage,
}: {
  scans: WholeSceneReviewScanSummary[];
  selectedScan: WholeSceneScan | null;
  isLoading: boolean;
  busyAction: string | null;
  candidateErrors: Record<string, string>;
  candidateDrafts: Record<string, WholeSceneCandidateDraft>;
  aiBusyIds: Set<string>;
  inventoryGroups: InventoryGroup[];
  inventoryGroupsLoading: boolean;
  inventoryGroupsError: string | null;
  onContinueReview: (scanId: string) => void;
  onQueueAnalysis: (scanId: string) => void;
  onRequestDeleteScan: (scan: WholeSceneScan) => void;
  onPreviewImage: (payload: SelectedImage) => void;
  onPatchCandidate: (candidateId: string, input: PatchWholeSceneCandidateInput) => void;
  onRejectCandidate: (candidateId: string) => void;
  onApproveCandidate: (candidateId: string, input: ApproveWholeSceneCandidateInput) => void;
  onApproveAllCandidates: () => void;
  onCandidateDraftChange: (candidateId: string, draft: WholeSceneCandidateDraft) => void;
  onAssistCandidate: (candidateId: string, input: AssistWholeSceneCandidateInput) => void;
  onAddCandidate: (input: AddWholeSceneCandidateInput) => void;
  onUploadCandidateImages: (candidateId: string, files: File[], onProgress?: (entries: ItemImageUploadEntry[]) => void) => Promise<WholeSceneCandidateMutationResponse>;
  onDeleteCandidateImage: (candidateId: string, cropId: string) => Promise<WholeSceneCandidateImageDeleteResponse>;
}) {
  return (
    <div className="grid gap-4">
      {isLoading ? <p className="text-sm text-stone-400">Loading Whole Scene scans...</p> : null}
      {!isLoading && scans.length === 0 ? (
        <Panel title="Whole Scene scans" eyebrow="Empty">
          <p className="text-sm text-stone-300">No Whole Scene scans found.</p>
        </Panel>
      ) : null}
      {scans.length > 0 ? (
        <div className="grid gap-3 md:grid-cols-2">
          {scans.map((scan) => (
            <WholeSceneReviewScanCard
              key={scan.id}
              scan={scan}
              selected={selectedScan?.id === scan.id}
              busy={busyAction === `whole-scene-open:${scan.id}`}
              onContinueReview={() => onContinueReview(scan.id)}
            />
          ))}
        </div>
      ) : null}
      {selectedScan ? (
        <WholeSceneCandidateReviewPanel
          scan={selectedScan}
          busyAction={busyAction}
          candidateErrors={candidateErrors}
          candidateDrafts={candidateDrafts}
          aiBusyIds={aiBusyIds}
          inventoryGroups={inventoryGroups}
          inventoryGroupsLoading={inventoryGroupsLoading}
          inventoryGroupsError={inventoryGroupsError}
          onQueueAnalysis={onQueueAnalysis}
          onRequestDeleteScan={onRequestDeleteScan}
          onPreviewImage={onPreviewImage}
          onPatchCandidate={onPatchCandidate}
          onRejectCandidate={onRejectCandidate}
          onApproveCandidate={onApproveCandidate}
          onApproveAllCandidates={onApproveAllCandidates}
          onCandidateDraftChange={onCandidateDraftChange}
          onAssistCandidate={onAssistCandidate}
          onAddCandidate={onAddCandidate}
          onUploadCandidateImages={onUploadCandidateImages}
          onDeleteCandidateImage={onDeleteCandidateImage}
        />
      ) : null}
    </div>
  );
}

function WholeSceneReviewScanCard({
  scan,
  selected,
  busy,
  onContinueReview,
}: {
  scan: WholeSceneReviewScanSummary;
  selected: boolean;
  busy: boolean;
  onContinueReview: () => void;
}) {
  const context = scan.container?.name ?? scan.location_name ?? scan.location_detail ?? 'No container';
  const latestStatus = scan.latest_analysis_run?.status ?? 'not analyzed';
  return (
    <Panel title={context} eyebrow="Whole Scene">
      <div className="grid gap-3">
        {scan.hint ? <p className="line-clamp-2 text-sm text-stone-300">{scan.hint}</p> : null}
        <div className="flex flex-wrap gap-2">
          <Badge>{scan.status}</Badge>
          <Badge>{`AI ${latestStatus}`}</Badge>
          <Badge>{`${scan.image_count} images`}</Badge>
          <Badge>{`${scan.candidate_counts.pending} pending`}</Badge>
          <Badge>{`${scan.candidate_counts.approved} approved`}</Badge>
          <Badge>{`${scan.candidate_counts.rejected} rejected`}</Badge>
        </div>
        {scan.images.length > 0 ? (
          <div className="flex flex-wrap gap-2">
            {scan.images.map((sourceImage) => (
              <WholeSceneReviewSourceThumb key={sourceImage.id} image={sourceImage.image} />
            ))}
          </div>
        ) : null}
        <div className="grid gap-1 text-xs text-stone-400">
          <span>Inventory Group: {scan.inventory_group.name}</span>
          <span>Created: {formatDateTime(scan.created_datetime)}</span>
        </div>
        <button
          type="button"
          onClick={onContinueReview}
          disabled={busy}
          className={`rounded-md border px-4 py-2 text-sm font-semibold disabled:cursor-not-allowed disabled:opacity-50 ${selected ? 'border-amberline-400/55 bg-amberline-500/10 text-amberline-100' : 'border-signal-green/35 text-green-100 hover:bg-signal-green/10'}`}
        >
          {busy ? 'Loading...' : selected ? 'Reviewing' : 'Continue Review'}
        </button>
      </div>
    </Panel>
  );
}

function WholeSceneReviewSourceThumb({ image }: { image: WholeSceneImageAsset }) {
  const [failed, setFailed] = useState(false);
  const variant = image.thumbnail_available ? 'thumbnail' : image.normalized_available ? 'normalized' : 'original';
  const label = image.original_filename ?? image.stored_filename ?? 'Source image';

  if (image.status !== 'processed' || failed) {
    return (
      <div className="flex h-16 w-16 items-center justify-center rounded-md border border-rack-steel/25 bg-black/25 px-2 text-center text-[0.65rem] text-stone-500">
        {image.status === 'failed' ? 'Failed' : 'Image'}
      </div>
    );
  }

  return (
    <div className="h-16 w-16 overflow-hidden rounded-md border border-rack-steel/25 bg-black/25">
      <img
        src={imageUrl(image.image_asset_id, variant)}
        alt={label}
        title={label}
        loading="lazy"
        onError={() => setFailed(true)}
        className="h-full w-full object-cover"
      />
    </div>
  );
}

function WholeSceneCandidateReviewPanel({
  scan,
  busyAction,
  candidateErrors,
  candidateDrafts,
  aiBusyIds,
  inventoryGroups,
  inventoryGroupsLoading,
  inventoryGroupsError,
  onQueueAnalysis,
  onRequestDeleteScan,
  onPreviewImage,
  onPatchCandidate,
  onRejectCandidate,
  onApproveCandidate,
  onApproveAllCandidates,
  onCandidateDraftChange,
  onAssistCandidate,
  onAddCandidate,
  onUploadCandidateImages,
  onDeleteCandidateImage,
}: {
  scan: WholeSceneScan;
  busyAction: string | null;
  candidateErrors: Record<string, string>;
  candidateDrafts: Record<string, WholeSceneCandidateDraft>;
  aiBusyIds: Set<string>;
  inventoryGroups: InventoryGroup[];
  inventoryGroupsLoading: boolean;
  inventoryGroupsError: string | null;
  onQueueAnalysis: (scanId: string) => void;
  onRequestDeleteScan: (scan: WholeSceneScan) => void;
  onPreviewImage: (payload: SelectedImage) => void;
  onPatchCandidate: (candidateId: string, input: PatchWholeSceneCandidateInput) => void;
  onRejectCandidate: (candidateId: string) => void;
  onApproveCandidate: (candidateId: string, input: ApproveWholeSceneCandidateInput) => void;
  onApproveAllCandidates: () => void;
  onCandidateDraftChange: (candidateId: string, draft: WholeSceneCandidateDraft) => void;
  onAssistCandidate: (candidateId: string, input: AssistWholeSceneCandidateInput) => void;
  onAddCandidate: (input: AddWholeSceneCandidateInput) => void;
  onUploadCandidateImages: (candidateId: string, files: File[], onProgress?: (entries: ItemImageUploadEntry[]) => void) => Promise<WholeSceneCandidateMutationResponse>;
  onDeleteCandidateImage: (candidateId: string, cropId: string) => Promise<WholeSceneCandidateImageDeleteResponse>;
}) {
  const candidates = useMemo(() => {
    const sorted = [...scan.candidates].sort((left, right) => candidateSortWeight(left) - candidateSortWeight(right));
    return sorted.filter(isPendingWholeSceneCandidate);
  }, [scan.candidates]);
  const latestAnalysisStatus = scan.latest_analysis_run?.status ?? 'not_analyzed';
  const sourceImagesTerminal = scan.images.length > 0 && scan.images.every((image) => wholeSceneTerminalImageStatuses.has(image.image.status));
  const processedSourceImages = scan.images.filter((image) => image.image.status === 'processed').length;
  const analysisBusy = wholeSceneActiveStatuses.has(scan.status) || wholeSceneActiveStatuses.has(latestAnalysisStatus);
  const analysisComplete = latestAnalysisStatus === 'succeeded' || latestAnalysisStatus === 'partial' || scan.status === 'succeeded' || scan.status === 'partial';
  const canRunAnalysis = sourceImagesTerminal && processedSourceImages > 0 && !analysisBusy && !analysisComplete;
  const deleteBusy = busyAction === `whole-scene-delete:${scan.id}`;
  const acceptAllBusy = busyAction === `whole-scene-approve-all:${scan.id}`;

  return (
    <Panel title="Whole Scene candidates" eyebrow={scan.container?.name ?? scan.location_name ?? 'Review scan'}>
      <div className="grid gap-4">
        <div className="grid gap-3 rounded-md border border-rack-steel/25 bg-rack-soot/55 p-3">
          <div className="flex flex-wrap items-center justify-between gap-3">
            <div className="grid gap-1 text-sm text-stone-300">
              <span>Analysis: {scan.latest_analysis_run?.status ?? 'not started'}</span>
              <span>Source images: {processedSourceImages}/{scan.images.length} processed</span>
            </div>
            {!analysisComplete ? (
              <button
                type="button"
                onClick={() => onQueueAnalysis(scan.id)}
                disabled={!canRunAnalysis || busyAction === `whole-scene-analyze:${scan.id}` || deleteBusy || acceptAllBusy}
                className="rounded-md border border-amberline-400/35 px-3 py-2 text-sm font-semibold text-amberline-100 hover:bg-amberline-500/10 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {busyAction === `whole-scene-analyze:${scan.id}` ? 'Queueing...' : latestAnalysisStatus === 'failed' ? 'Retry Analysis' : 'Run Analysis'}
              </button>
            ) : null}
            {candidates.length > 0 ? (
              <button
                type="button"
                onClick={onApproveAllCandidates}
                disabled={acceptAllBusy || deleteBusy}
                className="rounded-md border border-signal-green/35 px-3 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {acceptAllBusy ? 'Accepting All...' : 'Accept All'}
              </button>
            ) : null}
            <button
              type="button"
              onClick={() => onRequestDeleteScan(scan)}
              disabled={deleteBusy || acceptAllBusy}
              className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {deleteBusy ? 'Deleting...' : 'Delete Scan'}
            </button>
          </div>
          {scan.latest_analysis_run?.error_message ? <p className="text-sm text-red-100">Analysis failed: {scan.latest_analysis_run.error_message}</p> : null}
          {!sourceImagesTerminal ? <p className="text-sm text-stone-400">Scan created. Source images are still processing. Review refreshes automatically.</p> : null}
          {sourceImagesTerminal && processedSourceImages === 0 ? <p className="text-sm text-stone-400">All source images failed processing, so analysis cannot run.</p> : null}
          {sourceImagesTerminal && processedSourceImages > 0 && !analysisComplete && !analysisBusy && latestAnalysisStatus !== 'failed' ? (
            <p className="text-sm text-stone-400">Scan created. Analysis has not been run yet.</p>
          ) : null}
          {analysisBusy ? <p className="text-sm text-stone-400">Analysis is queued or processing. Review refreshes automatically.</p> : null}
          {analysisComplete && candidates.length === 0 ? <p className="text-sm text-stone-400">Analysis completed with no pending candidates. You can still add a manual candidate below.</p> : null}
        </div>
        <WholeSceneManualCandidateForm busy={busyAction === 'whole-scene-add' || acceptAllBusy} onAddCandidate={onAddCandidate} />
        {candidates.length > 0 ? (
          <div className="grid gap-3">
            {candidates.map((candidate, index) => (
              <WholeSceneCandidateReviewCard
                key={candidate.id}
                candidate={candidate}
                defaultInventoryGroupId={scan.inventory_group.id}
                defaultInventoryGroupName={scan.inventory_group.name}
                draft={candidateDrafts[candidate.id]}
                inventoryGroups={inventoryGroups}
                inventoryGroupsLoading={inventoryGroupsLoading}
                inventoryGroupsError={inventoryGroupsError}
                contextName={scan.container?.name ?? scan.location_name ?? scan.location_detail ?? null}
                shortcutsEnabled={index === 0}
                busyAction={busyAction}
                aiBusy={aiBusyIds.has(candidate.id)}
                mutationError={candidateErrors[candidate.id] ?? null}
                onPreviewImage={onPreviewImage}
                onPatchCandidate={onPatchCandidate}
                onRejectCandidate={onRejectCandidate}
                onApproveCandidate={onApproveCandidate}
                onDraftChange={onCandidateDraftChange}
                onAssistCandidate={onAssistCandidate}
                onUploadCandidateImages={onUploadCandidateImages}
                onDeleteCandidateImage={onDeleteCandidateImage}
              />
            ))}
          </div>
        ) : null}
      </div>
    </Panel>
  );
}

function WholeSceneManualCandidateForm({
  busy,
  onAddCandidate,
}: {
  busy: boolean;
  onAddCandidate: (input: AddWholeSceneCandidateInput) => void;
}) {
  const [title, setTitle] = useState('');
  const [approxValue, setApproxValue] = useState('');
  const [error, setError] = useState<string | null>(null);

  const handleSubmit = () => {
    const trimmedTitle = title.trim();
    const trimmedApproxValue = approxValue.trim();
    if (!trimmedTitle) {
      setError('Manual candidate title is required.');
      return;
    }
    if (trimmedApproxValue && !approxValuePattern.test(trimmedApproxValue)) {
      setError('Approx Value must be a non-negative decimal with up to two decimal places.');
      return;
    }
    setError(null);
    onAddCandidate({
      title: trimmedTitle,
      approx_value: trimmedApproxValue || undefined,
      confidence_label: 'unknown',
    });
    setTitle('');
    setApproxValue('');
  };

  return (
    <div className="grid gap-2 rounded-md border border-rack-steel/25 bg-rack-soot/55 p-3 sm:grid-cols-[minmax(0,1fr)_9rem_auto] sm:items-end">
      <label className="grid gap-1 text-sm text-stone-200">
        Manual candidate
        <input
          value={title}
          onChange={(event) => setTitle(event.target.value)}
          placeholder="Missed item title"
          className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
        />
      </label>
      <label className="grid gap-1 text-sm text-stone-200">
        Approx Value
        <input
          value={approxValue}
          onChange={(event) => setApproxValue(event.target.value)}
          inputMode="decimal"
          placeholder="0.00"
          className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
        />
      </label>
      <button
        type="button"
        onClick={handleSubmit}
        disabled={busy}
        className="rounded-md border border-signal-green/35 px-4 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
      >
        {busy ? 'Adding...' : 'Add'}
      </button>
      {error ? <p className="text-sm text-red-100 sm:col-span-3">{error}</p> : null}
    </div>
  );
}

function WholeSceneCandidateReviewCard({
  candidate,
  defaultInventoryGroupId,
  defaultInventoryGroupName,
  draft,
  inventoryGroups,
  inventoryGroupsLoading,
  inventoryGroupsError,
  contextName,
  shortcutsEnabled,
  busyAction,
  aiBusy,
  mutationError,
  onPreviewImage,
  onPatchCandidate,
  onRejectCandidate,
  onApproveCandidate,
  onDraftChange,
  onAssistCandidate,
  onUploadCandidateImages,
  onDeleteCandidateImage,
}: {
  candidate: WholeSceneCandidate;
  defaultInventoryGroupId: string;
  defaultInventoryGroupName: string;
  draft: WholeSceneCandidateDraft | undefined;
  inventoryGroups: InventoryGroup[];
  inventoryGroupsLoading: boolean;
  inventoryGroupsError: string | null;
  contextName: string | null;
  shortcutsEnabled: boolean;
  busyAction: string | null;
  aiBusy: boolean;
  mutationError: string | null;
  onPreviewImage: (payload: SelectedImage) => void;
  onPatchCandidate: (candidateId: string, input: PatchWholeSceneCandidateInput) => void;
  onRejectCandidate: (candidateId: string) => void;
  onApproveCandidate: (candidateId: string, input: ApproveWholeSceneCandidateInput) => void;
  onDraftChange: (candidateId: string, draft: WholeSceneCandidateDraft) => void;
  onAssistCandidate: (candidateId: string, input: AssistWholeSceneCandidateInput) => void;
  onUploadCandidateImages: (candidateId: string, files: File[], onProgress?: (entries: ItemImageUploadEntry[]) => void) => Promise<WholeSceneCandidateMutationResponse>;
  onDeleteCandidateImage: (candidateId: string, cropId: string) => Promise<WholeSceneCandidateImageDeleteResponse>;
}) {
  const [title, setTitle] = useState(candidate.title ?? '');
  const [description, setDescription] = useState(candidate.description ?? '');
  const [approxValue, setApproxValue] = useState(candidate.approx_value ?? '');
  const [inventoryGroupId, setInventoryGroupId] = useState(draft?.inventory_group_id ?? defaultInventoryGroupId);
  const [error, setError] = useState<string | null>(null);
  const [isUploadingImages, setIsUploadingImages] = useState(false);
  const [imageUploadEntries, setImageUploadEntries] = useState<ItemImageUploadEntry[]>([]);
  const [imageUploadError, setImageUploadError] = useState<string | null>(null);
  const [imageDeleteTarget, setImageDeleteTarget] = useState<WholeSceneCandidateCrop | null>(null);
  const [imageDeleteError, setImageDeleteError] = useState<string | null>(null);
  const [isDeletingImage, setIsDeletingImage] = useState(false);
  const [giveAIHint, setGiveAIHint] = useState(false);
  const [aiHint, setAIHint] = useState('');
  const imageInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    setTitle(candidate.title ?? '');
    setDescription(candidate.description ?? '');
    setApproxValue(candidate.approx_value ?? '');
    setInventoryGroupId(draft?.inventory_group_id ?? defaultInventoryGroupId);
    setError(null);
    setImageUploadEntries([]);
    setImageUploadError(null);
    setImageDeleteTarget(null);
    setImageDeleteError(null);
    setGiveAIHint(false);
    setAIHint('');
  }, [candidate.id, candidate.title, candidate.description, candidate.approx_value, defaultInventoryGroupId, draft?.inventory_group_id]);

  const isApproved = candidate.status === 'approved';
  const isRejected = candidate.status === 'rejected';
  const aiAssistBusy = aiBusy || candidate.ai_assist_status === 'queued' || candidate.ai_assist_status === 'processing';
  const isBusy = aiAssistBusy || isUploadingImages || isDeletingImage || (busyAction?.startsWith('whole-scene-approve-all:') ?? false) || (busyAction?.endsWith(candidate.id) ?? false);
  const candidateImages = candidate.crops.filter((entry) => entry.crop_image_asset_id);
  const selectedInventoryGroupOption = inventoryGroups.find((inventoryGroup) => inventoryGroup.id === inventoryGroupId) ?? null;

  const handleSave = () => {
    const trimmedApproxValue = approxValue.trim();
    if (trimmedApproxValue && !approxValuePattern.test(trimmedApproxValue)) {
      setError('Approx Value must be a non-negative decimal with up to two decimal places.');
      return;
    }
    setError(null);
    onPatchCandidate(candidate.id, {
      title: title.trim() || null,
      description: description.trim() || null,
      approx_value: trimmedApproxValue || null,
    });
  };

  const handleAssist = () => {
    const trimmedApproxValue = approxValue.trim();
    if (trimmedApproxValue && !approxValuePattern.test(trimmedApproxValue)) {
      setError('Approx Value must be a non-negative decimal with up to two decimal places.');
      return;
    }
    setError(null);
    const trimmedHint = giveAIHint ? aiHint.trim() : '';
    onAssistCandidate(candidate.id, {
      title: title.trim() || null,
      description: description.trim() || null,
      approx_value: trimmedApproxValue || null,
      user_hint: trimmedHint || null,
    });
  };

  const handleApprove = () => {
    if (!inventoryGroupId) {
      setError('Inventory Group is required.');
      return;
    }
    setError(null);
    onApproveCandidate(candidate.id, {
      inventory_group_id: inventoryGroupId,
    });
  };

  const handleSelectImages = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) {
      return;
    }
    setIsUploadingImages(true);
    setImageUploadError(null);
    setImageDeleteError(null);

    try {
      const files = Array.from(fileList);
      await onUploadCandidateImages(candidate.id, files, (entries) => setImageUploadEntries(entries));
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'complete', progress_percent: 100, loaded_bytes: entry.total_bytes })));
    } catch (err) {
      console.error('Failed to upload Whole Scene candidate images', err);
      setImageUploadError(errorMessage(err, 'Failed to upload candidate images.'));
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'failed' })));
    } finally {
      setIsUploadingImages(false);
      if (imageInputRef.current) {
        imageInputRef.current.value = '';
      }
    }
  };

  const handleDeleteImage = async () => {
    if (!imageDeleteTarget) {
      return null;
    }
    setIsDeletingImage(true);
    setImageDeleteError(null);
    setImageUploadError(null);

    try {
      const response = await onDeleteCandidateImage(candidate.id, imageDeleteTarget.id);
      setImageDeleteTarget(null);
      return response;
    } catch (err) {
      console.error('Failed to delete Whole Scene candidate image', err);
      setImageDeleteError(errorMessage(err, 'Failed to delete candidate image.'));
      return null;
    } finally {
      setIsDeletingImage(false);
    }
  };

  useEffect(() => {
    if (!shortcutsEnabled || isApproved || isRejected) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (!event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || isEditableTarget(event.target)) {
        return;
      }
      const key = event.key.toLowerCase();
      if (key === 's') {
        event.preventDefault();
        handleSave();
      } else if (key === 'r') {
        event.preventDefault();
        onRejectCandidate(candidate.id);
      } else if (key === 'a') {
        event.preventDefault();
        handleApprove();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [approxValue, candidate.id, description, handleApprove, isApproved, isRejected, onRejectCandidate, shortcutsEnabled, title]);

  useEffect(() => {
    onDraftChange(candidate.id, {
      inventory_group_id: inventoryGroupId || undefined,
    });
  }, [candidate.id, inventoryGroupId, onDraftChange]);

  return (
    <div className={`grid gap-3 rounded-md border p-3 ${isApproved ? 'border-signal-green/35 bg-signal-green/10' : isRejected ? 'border-rack-steel/25 bg-rack-soot/45 opacity-80' : 'border-rack-steel/28 bg-rack-soot/70'}`}>
      <div className="grid gap-3 md:grid-cols-[12rem_minmax(0,1fr)] md:items-start">
        <div className="grid gap-2 md:max-w-[12rem]">
          {candidateImages.length > 0 ? (
            <div className="grid grid-cols-2 gap-2 md:grid-cols-1">
              {candidateImages.map((crop) => (
                <WholeSceneCandidateImageTile
                  key={crop.id}
                  candidate={candidate}
                  crop={crop}
                  contextName={contextName}
                  deleteDisabled={isApproved || isRejected || isBusy}
                  onDelete={() => {
                    setImageDeleteError(null);
                    setImageDeleteTarget(crop);
                  }}
                  onPreviewImage={onPreviewImage}
                />
              ))}
            </div>
          ) : (
            <div className="flex aspect-square items-center justify-center rounded-md border border-rack-steel/24 bg-black/25 px-3 text-center text-xs text-stone-400">No crop</div>
          )}
          <input
            ref={imageInputRef}
            type="file"
            accept="image/*"
            multiple
            className="hidden"
            onChange={(event) => void handleSelectImages(event.target.files)}
          />
          <button
            type="button"
            disabled={isApproved || isRejected || isBusy}
            onClick={() => imageInputRef.current?.click()}
            className="rounded-md border border-rack-steel/35 px-2.5 py-1.5 text-xs font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isUploadingImages ? 'Uploading...' : 'Add images'}
          </button>
        </div>
        <div className="grid gap-2">
          <input
            value={title}
            onChange={(event) => setTitle(event.target.value)}
            disabled={isApproved || isRejected || isBusy}
            className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none focus:border-amberline-400 disabled:opacity-70"
          />
          <textarea
            value={description}
            onChange={(event) => setDescription(event.target.value)}
            rows={2}
            disabled={isApproved || isRejected || isBusy}
            className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none focus:border-amberline-400 disabled:opacity-70"
          />
          <input
            value={approxValue}
            onChange={(event) => setApproxValue(event.target.value)}
            inputMode="decimal"
            placeholder="0.00"
            disabled={isApproved || isRejected || isBusy}
            className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 disabled:opacity-70"
          />
          <label className="grid gap-1 text-sm text-stone-200">
            Inventory Group
            <select
              value={inventoryGroupId}
              onChange={(event) => setInventoryGroupId(event.target.value)}
              disabled={isApproved || isRejected || isBusy || inventoryGroupsLoading}
              className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-70"
            >
              <option value="">Select inventory group</option>
              {!selectedInventoryGroupOption && inventoryGroupId ? (
                <option value={inventoryGroupId}>{inventoryGroupId === defaultInventoryGroupId ? defaultInventoryGroupName : 'Current inventory group'}</option>
              ) : null}
              {inventoryGroups.map((inventoryGroup) => (
                <option key={inventoryGroup.id} value={inventoryGroup.id}>
                  {inventoryGroup.name}
                </option>
              ))}
              {inventoryGroups.length === 0 && inventoryGroupsLoading ? <option value={inventoryGroupId}>Loading inventory groups...</option> : null}
            </select>
          </label>
          {error ? <p className="text-sm text-red-100">{error}</p> : null}
          {inventoryGroupsError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{inventoryGroupsError}</p> : null}
          {imageUploadError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageUploadError}</p> : null}
          {imageDeleteError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageDeleteError}</p> : null}
          {imageUploadEntries.length > 0 ? (
            <div className="grid gap-2 rounded-md border border-rack-steel/25 bg-rack-soot/70 p-3">
              {imageUploadEntries.map((entry) => (
                <div key={entry.file_name} className="grid gap-1 text-xs text-stone-300">
                  <div className="flex items-center justify-between gap-3">
                    <span className="truncate">{entry.file_name}</span>
                    <span>{entry.status === 'complete' ? '100%' : `${entry.progress_percent}%`}</span>
                  </div>
                  <div className="h-1.5 overflow-hidden rounded-full bg-black/35">
                    <div
                      className={`h-full rounded-full transition-all ${entry.status === 'failed' ? 'bg-red-400' : 'bg-amberline-400'}`}
                      style={{ width: `${Math.max(4, entry.progress_percent)}%` }}
                    />
                  </div>
                </div>
              ))}
            </div>
          ) : null}
          {mutationError || (candidate.ai_assist_status === 'failed' && candidate.ai_assist_error_message) ? (
            <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{mutationError ?? candidate.ai_assist_error_message}</p>
          ) : null}
          <div className="grid gap-2 rounded-md border border-rack-steel/25 bg-rack-soot/55 p-3">
            <label className="flex items-center gap-2 text-sm font-medium text-stone-200">
              <input
                type="checkbox"
                checked={giveAIHint}
                onChange={(event) => {
                  const checked = event.target.checked;
                  setGiveAIHint(checked);
                  if (!checked) {
                    setAIHint('');
                  }
                }}
                disabled={isApproved || isRejected || isBusy}
              />
              Give AI a hint
            </label>
            {giveAIHint ? (
              <textarea
                value={aiHint}
                onChange={(event) => setAIHint(event.target.value)}
                rows={3}
                disabled={isApproved || isRejected || isBusy}
                placeholder="Optional clue for AI Assist: brand, era, what you think it is, where it came from, or what to focus on."
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-sm text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
              />
            ) : null}
          </div>
          <div className="flex flex-wrap gap-2">
            <button
              type="button"
              onClick={handleSave}
              disabled={isApproved || isRejected || isBusy}
              className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm font-semibold text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busyAction === `whole-scene-patch:${candidate.id}` ? 'Saving...' : 'Save (Ctrl+S)'}
            </button>
            <button
              type="button"
              onClick={handleAssist}
              disabled={isApproved || isRejected || isBusy}
              className="rounded-md border border-amberline-400/35 px-3 py-2 text-sm font-semibold text-amberline-100 hover:bg-amberline-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {candidate.ai_assist_status === 'queued' ? 'AI queued...' : candidate.ai_assist_status === 'processing' || aiBusy ? 'AI working...' : 'AI Assist'}
            </button>
            <button
              type="button"
              onClick={() => onRejectCandidate(candidate.id)}
              disabled={isApproved || isRejected || isBusy}
              className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {busyAction === `whole-scene-reject:${candidate.id}` ? 'Rejecting...' : 'Reject (Ctrl+R)'}
            </button>
            <button
              type="button"
              onClick={handleApprove}
              disabled={isApproved || isRejected || isBusy}
              className="rounded-md border border-signal-green/35 px-3 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isApproved ? 'Approved' : busyAction === `whole-scene-approve:${candidate.id}` ? 'Approving...' : 'Approve (Ctrl+A)'}
            </button>
          </div>
        </div>
      </div>
      {imageDeleteTarget ? (
        <WholeSceneCandidateImageDeleteConfirmationModal
          crop={imageDeleteTarget}
          error={imageDeleteError}
          isDeleting={isDeletingImage}
          onCancel={() => {
            if (!isDeletingImage) {
              setImageDeleteTarget(null);
              setImageDeleteError(null);
            }
          }}
          onConfirm={() => void handleDeleteImage()}
        />
      ) : null}
    </div>
  );
}

function WholeSceneCandidateImageTile({
  candidate,
  crop,
  contextName,
  deleteDisabled,
  onDelete,
  onPreviewImage,
}: {
  candidate: WholeSceneCandidate;
  crop: WholeSceneCandidateCrop;
  contextName: string | null;
  deleteDisabled: boolean;
  onDelete: () => void;
  onPreviewImage: (payload: SelectedImage) => void;
}) {
  if (!crop.crop_image_asset_id) {
    return null;
  }

  const image = crop.crop_image ?? {
    image_asset_id: crop.crop_image_asset_id,
    original_filename: null,
    stored_filename: null,
    mime_type: null,
    file_size_bytes: null,
    status: crop.status,
    upload_order: 0,
  };

  return (
    <div className="grid gap-1 rounded-md border border-rack-steel/24 bg-black/20 p-1.5">
      <button
        type="button"
        onClick={() =>
          onPreviewImage({
            groupId: candidate.id,
            groupTitle: candidate.title ?? 'Whole Scene candidate',
            groupClientGroupId: null,
            containerName: contextName,
            image,
          })
        }
        className="aspect-square overflow-hidden rounded-md border border-rack-steel/24 bg-black/25 cursor-zoom-in transition hover:opacity-95 focus:outline-none focus:ring-2 focus:ring-amberline-400/70"
      >
        <img src={imageUrl(crop.crop_image_asset_id)} alt={candidate.title ?? 'Whole Scene crop'} className="h-full w-full object-cover" loading="lazy" />
      </button>
      <button
        type="button"
        disabled={deleteDisabled}
        onClick={onDelete}
        className="rounded-md border border-red-400/35 px-2 py-1 text-xs font-medium text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
      >
        Delete
      </button>
    </div>
  );
}

function WholeSceneCandidateImageDeleteConfirmationModal({
  crop,
  error,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  crop: WholeSceneCandidateCrop;
  error: string | null;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const imageName = crop.crop_image?.original_filename ?? crop.crop_image?.stored_filename ?? 'selected image';

  return (
    <div
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/80 px-4 py-6"
      onClick={() => {
        if (!isDeleting) {
          onCancel();
        }
      }}
      role="dialog"
      aria-modal="true"
      aria-label={`Remove image ${imageName}`}
    >
      <div className="max-h-[92vh] w-full max-w-lg overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel
          title="Remove image?"
          eyebrow="Whole Scene / Image delete"
          action={(
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={onConfirm}
                disabled={isDeleting}
                className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isDeleting ? 'Removing...' : 'Remove image'}
              </button>
              <button
                type="button"
                onClick={onCancel}
                disabled={isDeleting}
                className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          )}
        >
          <div className="grid gap-4">
            <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
              <p className="text-sm leading-6 text-stone-200">
                Permanently delete <span className="font-semibold text-red-100">{imageName}</span> from this Whole Scene candidate?
              </p>
              <p className="mt-3 text-sm leading-6 text-stone-200">
                This deletes only the selected candidate image and its managed image files. Source scan images and approved inventory item images are preserved.
              </p>
              <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
            </div>

            {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function ReviewGroupCard({
  group,
  shortcutsEnabled,
  inventoryGroups,
  inventoryGroupsLoading,
  inventoryGroupsError,
  busyAction,
  onApprove,
  onRequestDelete,
  onGroupUpdated,
  onDraftChange,
  onPreviewImage,
}: {
  group: ReviewUploadGroup;
  shortcutsEnabled: boolean;
  inventoryGroups: InventoryGroup[];
  inventoryGroupsLoading: boolean;
  inventoryGroupsError: string | null;
  busyAction: string | null;
  onApprove: (groupId: string, input: ApproveUploadGroupInput) => Promise<ApproveUploadGroupResponse>;
  onRequestDelete: (group: ReviewUploadGroup) => Promise<void>;
  onGroupUpdated: (groupId: string, updatedGroup: ReviewUploadGroup | null) => void;
  onDraftChange: (groupId: string, draft: ReviewGroupDraft) => void;
  onPreviewImage: (payload: SelectedImage) => void;
}) {
  const [title, setTitle] = useState(preferredGroupTitle(group));
  const [description, setDescription] = useState(preferredGroupDescription(group));
  const [approxValue, setApproxValue] = useState(preferredGroupApproxValue(group));
  const [soldDate, setSoldDate] = useState('');
  const [notes, setNotes] = useState('');
  const [inventoryGroupId, setInventoryGroupId] = useState(group.inventory_group_id ?? '');
  const [error, setError] = useState<string | null>(null);
  const [statusNote, setStatusNote] = useState<string | null>(null);
  const [isAIBusy, setIsAIBusy] = useState(group.ai_assist_status === 'queued' || group.ai_assist_status === 'processing');
  const [giveAIHint, setGiveAIHint] = useState(false);
  const [aiHint, setAIHint] = useState('');
  const [hasRunAIAssist, setHasRunAIAssist] = useState(false);
  const [isUploadingImages, setIsUploadingImages] = useState(false);
  const [imageUploadEntries, setImageUploadEntries] = useState<ItemImageUploadEntry[]>([]);
  const [imageUploadError, setImageUploadError] = useState<string | null>(null);
  const [imageDeleteTarget, setImageDeleteTarget] = useState<ReviewImageAsset | null>(null);
  const [imageDeleteError, setImageDeleteError] = useState<string | null>(null);
  const [isDeletingImage, setIsDeletingImage] = useState(false);
  const imageInputRef = useRef<HTMLInputElement | null>(null);

  useEffect(() => {
    setTitle(preferredGroupTitle(group));
    setDescription(preferredGroupDescription(group));
    setApproxValue(preferredGroupApproxValue(group));
    setSoldDate('');
    setNotes('');
    setInventoryGroupId(group.inventory_group_id ?? '');
    setError(null);
    setImageUploadEntries([]);
    setImageUploadError(null);
    setImageDeleteTarget(null);
    setImageDeleteError(null);
  }, [group.upload_group_id]);

  useEffect(() => {
    setIsAIBusy(group.ai_assist_status === 'queued' || group.ai_assist_status === 'processing');
    if (group.ai_assist_status === 'succeeded') {
      setStatusNote('AI suggestions loaded.');
    } else if (group.ai_assist_status === 'failed') {
      setStatusNote(group.ai_assist_error_message ? `AI failed: ${group.ai_assist_error_message}` : 'AI failed.');
    } else if (group.ai_assist_status === 'queued' || group.ai_assist_status === 'processing') {
      setStatusNote('AI working...');
    } else {
      setStatusNote(null);
    }
  }, [group.ai_assist_error_message, group.ai_assist_status]);

  const processedBadge = `${group.processed_image_count} processed`;
  const failedBadge = `${group.failed_image_count} failed`;
  const aiBusy = isAIBusy || group.ai_assist_status === 'queued' || group.ai_assist_status === 'processing';
  const isApproving = busyAction === `approve:${group.upload_group_id}`;
  const isDeletePreviewing = busyAction === `delete-preview:${group.upload_group_id}`;
  const isDeleting = busyAction === `delete:${group.upload_group_id}`;
  const isBusy = isApproving || isDeletePreviewing || isDeleting || isUploadingImages || isDeletingImage;

  const handleApprove = async () => {
    try {
      setError(null);
      const trimmedApproxValue = approxValue.trim();
      if (trimmedApproxValue && !approxValuePattern.test(trimmedApproxValue)) {
        throw new Error('Approx Value must be a non-negative decimal with up to two decimal places.');
      }
      if (!inventoryGroupId) {
        throw new Error('Inventory Group is required.');
      }
      await onApprove(group.upload_group_id, {
        title: title.trim() || undefined,
        description: description.trim() || undefined,
        approx_value: trimmedApproxValue || undefined,
        sold_date: soldDate.trim() || null,
        notes: notes.trim(),
        inventory_group_id: inventoryGroupId,
      });
    } catch (err) {
      console.error('Failed to approve review group', err);
      setError(errorMessage(err, 'Failed to approve upload group.'));
    }
  };

  const handleAIAssist = async () => {
    setError(null);
    setStatusNote('AI working...');
    setIsAIBusy(true);
    setHasRunAIAssist(true);

    try {
      const trimmedHint = giveAIHint ? aiHint.trim() : '';
      const queued = await queueReviewUploadGroupAIAssist(group.upload_group_id, trimmedHint ? { user_hint: trimmedHint } : undefined);
      onGroupUpdated(group.upload_group_id, {
        ...group,
        ai_assist_status: queued.ai_assist_status,
        ai_assist_error_message: queued.ai_assist_error_message,
        ai_assist_requested_datetime: queued.ai_assist_requested_datetime,
        ai_assist_started_datetime: queued.ai_assist_started_datetime,
        ai_assist_completed_datetime: queued.ai_assist_completed_datetime,
        ai_suggested_title: queued.ai_suggested_title,
        ai_suggested_description: queued.ai_suggested_description,
        ai_suggested_approx_value: queued.ai_suggested_approx_value,
      });

      const startedAt = Date.now();
      for (;;) {
        if (Date.now() - startedAt >= AI_POLL_TIMEOUT_MS) {
          setStatusNote('AI Assist timed out after 5 minutes. The background job may still finish if you refresh the review queue later.');
          break;
        }

        await delay(AI_POLL_INTERVAL_MS);
        const response = await getReviewUploadGroup(group.upload_group_id);
        onGroupUpdated(group.upload_group_id, response.group);

        if (response.group.ai_assist_status === 'succeeded') {
          setTitle(preferredGroupTitle(response.group));
          setDescription(preferredGroupDescription(response.group));
          setApproxValue(preferredGroupApproxValue(response.group));
          setStatusNote('AI suggestions loaded.');
          break;
        }

        if (response.group.ai_assist_status === 'failed') {
          setStatusNote(response.group.ai_assist_error_message ? `AI failed: ${response.group.ai_assist_error_message}` : 'AI failed.');
          break;
        }
      }
    } catch (err) {
      console.error('Failed to queue review AI assist', err);
      setError(errorMessage(err, 'Failed to run AI Assist.'));
    } finally {
      setIsAIBusy(false);
    }
  };

  const handleSelectImages = async (fileList: FileList | null) => {
    if (!fileList || fileList.length === 0) {
      return;
    }

    setIsUploadingImages(true);
    setImageUploadError(null);
    setImageDeleteError(null);
    setStatusNote(null);

    try {
      const files = Array.from(fileList);
      const response = await uploadReviewGroupImages(group.upload_group_id, files, (entries) => setImageUploadEntries(entries));
      onGroupUpdated(group.upload_group_id, response.group);
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'complete', progress_percent: 100, loaded_bytes: entry.total_bytes })));
      setStatusNote(`Uploaded ${files.length} image${files.length === 1 ? '' : 's'}.`);
    } catch (err) {
      console.error('Failed to upload review images', err);
      setImageUploadError(errorMessage(err, 'Failed to upload review images.'));
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'failed' })));
    } finally {
      setIsUploadingImages(false);
      if (imageInputRef.current) {
        imageInputRef.current.value = '';
      }
    }
  };

  const handleDeleteImage = async (): Promise<ReviewImageDeleteResponse | null> => {
    if (!imageDeleteTarget) {
      return null;
    }

    setIsDeletingImage(true);
    setImageDeleteError(null);
    setImageUploadError(null);
    setStatusNote(null);

    try {
      const response = await deleteReviewGroupImage(group.upload_group_id, imageDeleteTarget.image_asset_id);
      onGroupUpdated(group.upload_group_id, response.group);
      const removedName = imageDeleteTarget.original_filename ?? imageDeleteTarget.stored_filename ?? imageDeleteTarget.image_asset_id;
      setImageDeleteTarget(null);
      const warningNote = response.warnings.length > 0 ? ` ${response.warnings.join(' ')}` : '';
      setStatusNote(`Removed image "${removedName}" and deleted ${response.deleted_file_count} file${response.deleted_file_count === 1 ? '' : 's'}.${warningNote}`);
      return response;
    } catch (err) {
      console.error('Failed to delete review image', err);
      setImageDeleteError(errorMessage(err, 'Failed to delete review image.'));
      return null;
    } finally {
      setIsDeletingImage(false);
    }
  };

  const hasExistingAIResult = group.ai_assist_status !== 'not_requested';
  const aiButtonLabel = aiBusy
    ? 'AI working...'
    : hasRunAIAssist || hasExistingAIResult
      ? 'Re-run AI'
      : 'AI Assist';
  const selectedInventoryGroupOption = inventoryGroups.find((inventoryGroup) => inventoryGroup.id === inventoryGroupId) ?? null;

  useEffect(() => {
    onDraftChange(group.upload_group_id, {
      title: title.trim() || undefined,
      description: description.trim() || undefined,
      approx_value: approxValue.trim() || undefined,
      sold_date: soldDate.trim() || null,
      notes: notes.trim(),
      inventory_group_id: inventoryGroupId || undefined,
    });
  }, [approxValue, description, group.upload_group_id, inventoryGroupId, notes, onDraftChange, soldDate, title]);

  useEffect(() => {
    if (!shortcutsEnabled) {
      return;
    }
    const handleKeyDown = (event: KeyboardEvent) => {
      if (!event.ctrlKey || event.altKey || event.metaKey || event.shiftKey || isEditableTarget(event.target)) {
        return;
      }
      const key = event.key.toLowerCase();
      if (key === 'r') {
        event.preventDefault();
        void onRequestDelete(group);
      } else if (key === 'a') {
        event.preventDefault();
        void handleApprove();
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [approxValue, description, group, inventoryGroupId, notes, onRequestDelete, shortcutsEnabled, soldDate, title]);

  return (
    <Panel title={group.title ?? group.client_group_id ?? 'Untitled group'} eyebrow="Review item">
      <div className="grid gap-4">
        <div className="grid gap-3 lg:grid-cols-[1fr_auto] lg:items-start">
          <div className="grid gap-3">
            <label className="grid gap-2 text-sm text-stone-200">
              Title
              <input
                value={title}
                onChange={(event) => setTitle(event.target.value)}
                disabled={aiBusy || isBusy}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
              />
            </label>
            <label className="grid gap-2 text-sm text-stone-200">
              Description
              <textarea
                value={description}
                onChange={(event) => setDescription(event.target.value)}
                rows={3}
                disabled={aiBusy || isBusy}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
              />
            </label>
            <label className="grid gap-2 text-sm text-stone-200">
              Approx Value
              <input
                value={approxValue}
                onChange={(event) => setApproxValue(event.target.value)}
                inputMode="decimal"
                placeholder="0.00"
                disabled={aiBusy || isBusy}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
              />
            </label>
            <label className="grid gap-2 text-sm text-stone-200">
              Sold Date
              <input
                type="date"
                value={soldDate}
                onChange={(event) => setSoldDate(event.target.value)}
                disabled={aiBusy || isBusy}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
              />
            </label>
            <label className="grid gap-2 text-sm text-stone-200">
              Notes
              <textarea
                value={notes}
                onChange={(event) => setNotes(event.target.value)}
                rows={3}
                disabled={aiBusy || isBusy}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
              />
            </label>
            <label className="grid gap-2 text-sm text-stone-200">
              Inventory Group
              <select
                value={inventoryGroupId}
                onChange={(event) => setInventoryGroupId(event.target.value)}
                disabled={aiBusy || isBusy || inventoryGroupsLoading}
                className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
              >
                <option value="">Select inventory group</option>
                {!selectedInventoryGroupOption && group.inventory_group_id ? (
                  <option value={group.inventory_group_id}>
                    {group.inventory_group_name ?? group.inventory_group_code ?? 'Current inventory group'}
                  </option>
                ) : null}
                {inventoryGroups.map((inventoryGroup) => (
                  <option key={inventoryGroup.id} value={inventoryGroup.id}>
                    {inventoryGroup.name}
                  </option>
                ))}
                {inventoryGroups.length === 0 && inventoryGroupsLoading ? <option value={inventoryGroupId}>Loading inventory groups...</option> : null}
              </select>
            </label>
            {aiBusy ? (
              <p className="text-xs text-stone-400">AI Assist is analyzing the processed images. Editing is temporarily disabled.</p>
            ) : null}
          </div>

          <div className="grid gap-2 text-sm text-stone-300">
            <p>Session: {group.upload_session_id}</p>
            <p>Container: {group.container?.name ?? 'No Container'}</p>
            <p>Inventory Group: {group.inventory_group_name ?? group.inventory_group_code ?? 'Not set'}</p>
            {group.container?.location_description ? <p>Location: {group.container.location_description}</p> : null}
            <div className="flex flex-wrap gap-2 pt-1">
              <Badge>{`${group.image_count} images`}</Badge>
              <Badge>{processedBadge}</Badge>
              <Badge>{failedBadge}</Badge>
              <Badge>{group.status}</Badge>
              <Badge>{`AI ${group.ai_assist_status}`}</Badge>
            </div>
          </div>
        </div>

        <div className="grid gap-3">
          <div className="flex flex-wrap items-center gap-2">
            <input
              ref={imageInputRef}
              type="file"
              accept="image/*"
              multiple
              className="hidden"
              onChange={(event) => void handleSelectImages(event.target.files)}
            />
            <button
              type="button"
              disabled={isBusy || aiBusy}
              onClick={() => imageInputRef.current?.click()}
              className="rounded-md border border-rack-steel/35 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
            >
              {isUploadingImages ? 'Uploading images...' : 'Add images'}
            </button>
          </div>

          <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
          {group.images.map((image) => (
            <ReviewImageTile
              key={image.image_asset_id}
              image={image}
              isDeleteDisabled={isBusy || aiBusy}
              onDelete={() => {
                setImageDeleteError(null);
                setImageDeleteTarget(image);
              }}
              onOpen={() =>
                onPreviewImage({
                  groupId: group.upload_group_id,
                  groupTitle: group.title ?? group.client_group_id ?? null,
                  groupClientGroupId: group.client_group_id,
                  containerName: group.container?.name ?? null,
                  image,
                })
              }
            />
          ))}
          </div>
        </div>

        {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
        {imageUploadError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageUploadError}</p> : null}
        {imageDeleteError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageDeleteError}</p> : null}
        {inventoryGroupsError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{inventoryGroupsError}</p> : null}
        {statusNote ? <p className="rounded-md border border-rack-steel/25 bg-rack-soot/75 px-3 py-2 text-sm text-stone-300">{statusNote}</p> : null}
        {imageUploadEntries.length > 0 ? (
          <div className="grid gap-2 rounded-md border border-rack-steel/25 bg-rack-soot/70 p-3">
            {imageUploadEntries.map((entry) => (
              <div key={entry.file_name} className="grid gap-1 text-xs text-stone-300">
                <div className="flex items-center justify-between gap-3">
                  <span className="truncate">{entry.file_name}</span>
                  <span>{entry.status === 'complete' ? '100%' : `${entry.progress_percent}%`}</span>
                </div>
                <div className="h-1.5 overflow-hidden rounded-full bg-black/35">
                  <div
                    className={`h-full rounded-full transition-all ${entry.status === 'failed' ? 'bg-red-400' : 'bg-amberline-400'}`}
                    style={{ width: `${Math.max(4, entry.progress_percent)}%` }}
                  />
                </div>
              </div>
            ))}
          </div>
        ) : null}

        <div className="grid gap-3 rounded-md border border-rack-steel/25 bg-rack-soot/55 p-3">
          <label className="flex items-center gap-2 text-sm font-medium text-stone-200">
            <input
              type="checkbox"
              checked={giveAIHint}
              onChange={(event) => {
                const checked = event.target.checked;
                setGiveAIHint(checked);
                if (!checked) {
                  setAIHint('');
                }
              }}
            />
            Give AI a hint
          </label>
          {giveAIHint ? (
            <textarea
              value={aiHint}
              onChange={(event) => setAIHint(event.target.value)}
              rows={3}
              disabled={aiBusy || isBusy}
              placeholder="Optional clue for AI Assist: brand, era, what you think it is, where it came from, or what to focus on."
              className="rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-sm text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
            />
          ) : null}
        </div>

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            disabled={isBusy || aiBusy}
            onClick={() => void handleAIAssist()}
            className="rounded-md border border-amberline-400/35 px-4 py-2 text-sm font-semibold text-amberline-100 hover:bg-amberline-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {aiButtonLabel}
          </button>
          <button
            type="button"
            disabled={isBusy || aiBusy}
            onClick={() => void handleApprove()}
            className="rounded-md border border-signal-green/35 px-4 py-2 text-sm font-semibold text-green-100 hover:bg-signal-green/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isApproving ? 'Approving...' : 'Approve (Ctrl+A)'}
          </button>
          <button
            type="button"
            disabled={isBusy || aiBusy}
            onClick={() => void onRequestDelete(group)}
            className="rounded-md border border-red-400/45 px-4 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isDeletePreviewing ? 'Preparing reject...' : isDeleting ? 'Rejecting...' : 'Reject (Ctrl+R)'}
          </button>
        </div>
      </div>
      {imageDeleteTarget ? (
        <ReviewImageDeleteConfirmationModal
          image={imageDeleteTarget}
          isLastImage={group.images.length === 1}
          error={imageDeleteError}
          isDeleting={isDeletingImage}
          onCancel={() => {
            if (!isDeletingImage) {
              setImageDeleteTarget(null);
              setImageDeleteError(null);
            }
          }}
          onConfirm={() => void handleDeleteImage()}
        />
      ) : null}
    </Panel>
  );
}

function ReviewImageTile({
  image,
  isDeleteDisabled,
  onDelete,
  onOpen,
}: {
  image: ReviewImageAsset;
  isDeleteDisabled: boolean;
  onDelete: () => void;
  onOpen: () => void;
}) {
  const [failed, setFailed] = useState(false);
  const [loaded, setLoaded] = useState(false);

  useEffect(() => {
    setFailed(false);
    setLoaded(false);
  }, [image.image_asset_id]);

  const src = imageUrl(image.image_asset_id);
  const title = image.original_filename ?? image.stored_filename ?? 'Untitled file';

  return (
    <div className="grid gap-2 rounded-md border border-rack-steel/28 bg-rack-soot/75 p-3 transition hover:border-amberline-500/40 hover:bg-rack-soot/90">
      <div className="flex justify-end">
        <button
          type="button"
          disabled={isDeleteDisabled}
          onClick={onDelete}
          className="rounded-md border border-red-400/35 px-2.5 py-1 text-xs font-medium text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
        >
          Delete
        </button>
      </div>
      <button
        type="button"
        onClick={onOpen}
        className="grid gap-2 text-left"
      >
      <div className="relative aspect-square overflow-hidden rounded-md border border-rack-steel/24 bg-black/25">
        {failed ? (
          <div className="flex h-full items-center justify-center px-4 text-center text-xs text-stone-400">
            Image preview unavailable
          </div>
        ) : (
          <>
            {!loaded ? (
              <div className="absolute inset-0 flex items-center justify-center bg-black/40 text-xs text-stone-300">
                Loading preview...
              </div>
            ) : null}
            <img
              src={src}
              alt={title}
              loading="lazy"
              onLoad={() => setLoaded(true)}
              onError={() => {
                setLoaded(false);
                setFailed(true);
              }}
              className={`h-full w-full object-cover transition duration-200 ${loaded ? 'opacity-100' : 'opacity-0'}`}
            />
          </>
        )}
      </div>

      <div className="grid gap-1">
        <p className="truncate text-sm font-medium text-stone-100">{title}</p>
        <p className="truncate text-xs text-stone-400">{image.stored_filename ?? 'n/a'}</p>
        <div className="flex flex-wrap items-center gap-2 text-xs text-stone-400">
          <span className="rounded-full border border-rack-steel/22 bg-rack-soot/60 px-2 py-0.5">{image.status}</span>
          {image.file_size_bytes != null ? <span>{formatFileSize(image.file_size_bytes)}</span> : null}
        </div>
      </div>
      </button>
    </div>
  );
}

function ReviewImageDeleteConfirmationModal({
  image,
  isLastImage,
  error,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  image: ReviewImageAsset;
  isLastImage: boolean;
  error: string | null;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const imageName = image.original_filename ?? image.stored_filename ?? 'selected image';

  return (
    <div
      className="fixed inset-0 z-[70] flex items-center justify-center bg-black/80 px-4 py-6"
      onClick={() => {
        if (!isDeleting) {
          onCancel();
        }
      }}
      role="dialog"
      aria-modal="true"
      aria-label={`Remove image ${imageName}`}
    >
      <div className="max-h-[92vh] w-full max-w-lg overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel
          title="Remove image?"
          eyebrow="Review / Image delete"
          action={(
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                onClick={onConfirm}
                disabled={isDeleting}
                className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isDeleting ? 'Removing...' : 'Remove image'}
              </button>
              <button
                type="button"
                onClick={onCancel}
                disabled={isDeleting}
                className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          )}
        >
          <div className="grid gap-4">
            <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
              <p className="text-sm leading-6 text-stone-200">
                Permanently delete <span className="font-semibold text-red-100">{imageName}</span> from this review group?
              </p>
              <p className="mt-3 text-sm leading-6 text-stone-200">
                This deletes the selected image asset row plus its original, thumbnail, and normalized files. The review group and remaining images stay unchanged.
              </p>
              {isLastImage ? (
                <p className="mt-3 text-sm leading-6 text-amberline-100">
                  This is the last image for this review item. Deleting it will remove this item from the review queue unless another image is added first.
                </p>
              ) : null}
              <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
            </div>

            {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function ReviewDeleteConfirmationPanel({
  group,
  preview,
  error,
  isLoading,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  group: ReviewUploadGroup;
  preview: ReviewGroupDeletePreview | null;
  error: string | null;
  isLoading: boolean;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <Panel title="Discard this review group?" eyebrow="Destructive action">
      <div className="grid gap-4">
        <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
          <p className="text-base font-semibold text-red-100">{group.title ?? group.client_group_id ?? 'Untitled group'}</p>
          <p className="mt-3 text-sm leading-6 text-stone-200">
            This permanently deletes the unapproved upload group, linked image metadata, and DB-referenced image files from disk.
          </p>
          <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
        </div>

        {isLoading ? <p className="text-sm text-stone-300">Loading discard preview...</p> : null}
        {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}

        {preview ? (
          <>
            <dl className="grid gap-2 rounded-md border border-rack-steel/30 bg-rack-soot/75 p-4 text-sm sm:grid-cols-2">
              <DeleteCount label="Images" value={preview.image_count} />
              <DeleteCount label="Files" value={preview.file_count} />
              <DeleteCount label="File size" value={formatBytes(preview.total_file_size_bytes)} />
            </dl>
            {preview.warnings.map((warning) => (
              <p key={warning} className="rounded-md border border-rack-steel/25 bg-rack-soot/70 px-3 py-2 text-sm text-stone-300">
                {warning}
              </p>
            ))}
          </>
        ) : null}

        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            disabled={!preview || isLoading || isDeleting}
            onClick={onConfirm}
            className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
          >
            {isDeleting ? 'Discarding...' : 'Discard permanently'}
          </button>
          <button
            type="button"
            disabled={isDeleting}
            onClick={onCancel}
            className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
          >
            Cancel
          </button>
        </div>
      </div>
    </Panel>
  );
}

function WholeSceneDeleteConfirmationPanel({
  scan,
  error,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  scan: WholeSceneScan;
  error: string | null;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const context = scan.container?.name ?? scan.location_name ?? scan.location_detail ?? 'No container';

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/70 px-4 py-6 backdrop-blur-sm">
      <div className="max-h-[90vh] w-full max-w-2xl overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel title="Delete this Whole Scene scan?" eyebrow="Destructive action">
          <div className="grid gap-4">
            <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
              <p className="text-base font-semibold text-red-100">{context}</p>
              <p className="mt-3 break-all font-mono text-xs text-stone-300">{scan.id}</p>
              <p className="mt-3 text-sm leading-6 text-stone-200">
                This permanently deletes the Whole Scene scan, source image staging records, analysis runs, candidates, appearances, crop records, and unused source/crop files.
              </p>
              <p className="mt-3 text-sm leading-6 text-stone-200">
                Approved inventory items and their attached item images are preserved.
              </p>
              <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
            </div>

            {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}

            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                disabled={isDeleting}
                onClick={onConfirm}
                className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isDeleting ? 'Deleting...' : 'Delete scan permanently'}
              </button>
              <button
                type="button"
                disabled={isDeleting}
                onClick={onCancel}
                className="rounded-md border border-rack-steel/35 px-4 py-3 text-sm text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
              >
                Cancel
              </button>
            </div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function ImageModal({
  imageUrlValue,
  image,
  groupTitle,
  groupClientGroupId,
  containerName,
  failed,
  onClose,
  onImageError,
  onBackdropClick,
}: {
  imageUrlValue: string;
  image: PreviewImageAsset;
  groupTitle: string | null;
  groupClientGroupId: string | null;
  containerName: string | null;
  failed: boolean;
  onClose: () => void;
  onImageError: () => void;
  onBackdropClick: () => void;
}) {
  const [loaded, setLoaded] = useState(false);
  useEffect(() => {
    setLoaded(false);
  }, [image.image_asset_id]);

  const title = image.original_filename ?? image.stored_filename ?? 'Untitled file';

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 px-4 py-6"
      onClick={onBackdropClick}
    >
      <div className="max-h-[90vh] w-full max-w-5xl overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel title="Container image" eyebrow="Full-size preview">
          <div className="grid gap-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="grid gap-1">
                <h3 className="text-lg font-semibold text-stone-100">{title}</h3>
                <p className="text-sm text-stone-400">
                  {containerName ?? 'No Container'}
                  {groupTitle ? ` • ${groupTitle}` : ''}
                  {groupClientGroupId ? ` • ${groupClientGroupId}` : ''}
                </p>
              </div>
              <button
                type="button"
                onClick={onClose}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
              >
                Close
              </button>
            </div>

            <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
              <div className="flex min-h-[18rem] items-center justify-center rounded-md border border-rack-steel/28 bg-black/25 p-3">
                {failed ? (
                  <div className="text-sm text-stone-300">Image preview unavailable</div>
                ) : (
                  <div className="relative flex w-full items-center justify-center">
                    {!loaded ? (
                      <div className="absolute inset-0 flex items-center justify-center text-sm text-stone-400">
                        Loading full-size preview...
                      </div>
                    ) : null}
                    <img
                      src={imageUrlValue}
                      alt={title}
                      onLoad={() => setLoaded(true)}
                      onError={onImageError}
                      className={`max-h-[78vh] w-full object-contain ${loaded ? 'opacity-100' : 'opacity-0'}`}
                    />
                  </div>
                )}
              </div>

              <div className="grid gap-3 text-sm text-stone-300">
                <InfoRow label="Status" value={image.status} />
                <InfoRow label="Original" value={image.original_filename ?? 'n/a'} />
                <InfoRow label="Stored" value={image.stored_filename ?? 'n/a'} />
                <InfoRow label="File size" value={image.file_size_bytes != null ? formatFileSize(image.file_size_bytes) : 'n/a'} />
                <InfoRow label="Upload order" value={String(image.upload_order)} />
                <InfoRow label="MIME type" value={image.mime_type ?? 'n/a'} />
                <InfoRow label="Asset ID" value={image.image_asset_id} monospace />
              </div>
            </div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function InfoRow({ label, value, monospace = false }: { label: string; value: string; monospace?: boolean }) {
  return (
    <div className="rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-2">
      <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">{label}</div>
      <div className={`mt-1 ${monospace ? 'font-mono text-xs break-all' : 'text-sm'}`}>{value}</div>
    </div>
  );
}

function Badge({ children }: { children: string }) {
  return <span className="rounded-full border border-rack-steel/24 bg-rack-soot/75 px-2.5 py-1 text-xs text-stone-300">{children}</span>;
}

function DeleteCount({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-stone-400">{label}</dt>
      <dd className="font-semibold text-stone-100">{value}</dd>
    </div>
  );
}

function formatFileSize(bytes: number): string {
  if (bytes < 1024) {
    return `${bytes} B`;
  }

  const kib = bytes / 1024;
  if (kib < 1024) {
    return `${kib.toFixed(kib >= 10 ? 0 : 1)} KB`;
  }

  const mib = kib / 1024;
  return `${mib.toFixed(mib >= 10 ? 0 : 1)} MB`;
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

function preferredGroupTitle(group: ReviewUploadGroup): string {
  return (group.ai_suggested_title ?? group.title ?? group.client_group_id ?? '').trim();
}

function preferredGroupDescription(group: ReviewUploadGroup): string {
  return (group.ai_suggested_description ?? group.notes ?? '').trim();
}

function preferredGroupApproxValue(group: ReviewUploadGroup): string {
  return (group.ai_suggested_approx_value ?? '').trim();
}

function defaultApproveInput(group: ReviewUploadGroup): ApproveUploadGroupInput {
  return {
    title: preferredGroupTitle(group) || undefined,
    description: preferredGroupDescription(group) || undefined,
    approx_value: preferredGroupApproxValue(group) || undefined,
    inventory_group_id: group.inventory_group_id ?? undefined,
  };
}

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  const tagName = target.tagName.toLowerCase();
  return tagName === 'input' || tagName === 'textarea' || tagName === 'select' || target.isContentEditable;
}

function shouldPollWholeSceneScan(scan: WholeSceneScan): boolean {
  if (wholeSceneActiveStatuses.has(scan.status) || wholeSceneActiveStatuses.has(scan.latest_analysis_run?.status ?? '')) {
    return true;
  }
  return scan.images.some((image) => !wholeSceneTerminalImageStatuses.has(image.image.status));
}

function isPendingWholeSceneCandidate(candidate: WholeSceneCandidate): boolean {
  return candidate.status === 'proposed' || candidate.status === 'edited';
}

function candidateSortWeight(candidate: WholeSceneCandidate): number {
  if (isPendingWholeSceneCandidate(candidate)) {
    return 0;
  }
  if (candidate.status === 'approved') {
    return 1;
  }
  if (candidate.status === 'rejected') {
    return 2;
  }
  return 3;
}

function formatDateTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}
