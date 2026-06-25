import { useDeferredValue, useEffect, useEffectEvent, useMemo, useRef, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { ApiError } from '../api/client';
import { listContainers } from '../api/containers';
import { imageUrl } from '../api/images';
import { getInventoryContainerSummaries } from '../api/inventory';
import { listInventoryGroups } from '../api/inventoryGroups';
import { archiveItem, deleteItem, deleteItemImage, getItem, getItemDeletePreview, listItemDispositionHistory, listItemDispositions, listItems, patchItem, unarchiveItem, uploadItemImages } from '../api/items';
import { createOrOpenListingDraft, deleteListingDraft, listItemListingDrafts, prepareListingDraftPhotos, updateListingDraft } from '../api/listingDrafts';
import { listLocations } from '../api/locations';
import { listEnabledSellProviders } from '../api/sell';
import { Panel } from '../components/Panel';
import { formatBytes } from '../utils/formatBytes';
import type { InventoryContainerSummary } from '../types/inventory';
import type { InventoryGroup } from '../types/inventoryGroups';
import type { ListingDraft, ListingDraftStatus } from '../types/listingDrafts';
import type {
  ItemDeletePreview,
  ItemDeleteResponse,
  ItemImageDeleteResponse,
  ItemImageUploadEntry,
  ItemDispositionHistoryEntry,
  InventoryImage,
  InventoryItemDetail,
  InventoryItemSummary,
  ItemDisposition,
  PatchItemInput,
} from '../types/items';
import type { PublicSellProvider } from '../types/sell';
import type { LocationOption } from '../types/locations';
import type { ContainerOption } from '../types/upload';

const pageLimit = 50;
const maxItemsPerContainerFetch = 100;
const moneyPattern = /^\d+(\.\d{1,2})?$/;
const looseBucketId = 'loose';
const looseContainerQueryValue = 'none';
const looseContainerSelectValue = '__loose__';
const listingDraftStatuses: ListingDraftStatus[] = ['draft', 'ready', 'listed', 'archived'];
const sortOptions = [
  { value: 'default', label: 'Best match / newest' },
  { value: 'newest', label: 'Newest first' },
  { value: 'oldest', label: 'Oldest first' },
  { value: 'title_asc', label: 'Title A-Z' },
  { value: 'title_desc', label: 'Title Z-A' },
  { value: 'approx_value_desc', label: 'Approx value high-low' },
  { value: 'approx_value_asc', label: 'Approx value low-high' },
];
const formerDispositionCodes = new Set(['sold', 'donated', 'disposed']);
const currentDispositionCodes = new Set(['for_sale', 'in_use', 'sale_pending']);
type InventoryState = 'current' | 'former' | 'all';

interface SelectedImage {
  itemTitle: string | null;
  containerName: string | null;
  image: InventoryImage;
}

interface ContainerItemCacheEntry {
  items: InventoryItemSummary[];
  isLoading: boolean;
  loaded: boolean;
  error: string | null;
}

export function InventoryPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const itemListRequestSeq = useRef(0);
  const [items, setItems] = useState<InventoryItemSummary[]>([]);
  const [containerSummaries, setContainerSummaries] = useState<InventoryContainerSummary[]>([]);
  const [containerItemsById, setContainerItemsById] = useState<Record<string, ContainerItemCacheEntry>>({});
  const [expandedContainerIds, setExpandedContainerIds] = useState<string[]>([]);
  const [totalCount, setTotalCount] = useState(0);
  const [limit, setLimit] = useState(pageLimit);
  const [offset, setOffset] = useState(0);
  const [isLoading, setIsLoading] = useState(false);
  const [isContainersLoading, setIsContainersLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [containersError, setContainersError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [searchInput, setSearchInput] = useState(() => searchParams.get('search')?.trim() ?? '');
  const [dispositionFilter, setDispositionFilter] = useState(() => searchParams.get('disposition_code')?.trim() || 'all');
  const [inventoryGroupFilter, setInventoryGroupFilter] = useState(() => searchParams.get('inventory_group_id')?.trim() || 'all');
  const [inventoryState, setInventoryState] = useState<InventoryState>(() => readInventoryStateSearchParam(searchParams));
  const [sort, setSort] = useState('default');
  const [showArchived, setShowArchived] = useState(() => readBooleanSearchParam(searchParams, 'include_archived') || readBooleanSearchParam(searchParams, 'archived'));
  const [reloadKey, setReloadKey] = useState(0);
  const [dispositions, setDispositions] = useState<ItemDisposition[]>([]);
  const [dispositionsError, setDispositionsError] = useState<string | null>(null);
  const [inventoryGroups, setInventoryGroups] = useState<InventoryGroup[]>([]);
  const [inventoryGroupsError, setInventoryGroupsError] = useState<string | null>(null);
  const [isInventoryGroupsLoading, setIsInventoryGroupsLoading] = useState(false);
  const [selectedItemId, setSelectedItemId] = useState<string | null>(null);
  const [detailItem, setDetailItem] = useState<InventoryItemDetail | null>(null);
  const [isDetailLoading, setIsDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState<string | null>(null);
  const [isSavingDetail, setIsSavingDetail] = useState(false);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [deletePreview, setDeletePreview] = useState<ItemDeletePreview | null>(null);
  const [deletePreviewError, setDeletePreviewError] = useState<string | null>(null);
  const [selectedImage, setSelectedImage] = useState<SelectedImage | null>(null);
  const [selectedImageFailed, setSelectedImageFailed] = useState(false);
  const [moveTargetContainers, setMoveTargetContainers] = useState<ContainerOption[]>([]);
  const [moveTargetContainersLoaded, setMoveTargetContainersLoaded] = useState(false);
  const [isMoveTargetContainersLoading, setIsMoveTargetContainersLoading] = useState(false);
  const [moveTargetContainersError, setMoveTargetContainersError] = useState<string | null>(null);
  const [itemLocations, setItemLocations] = useState<LocationOption[]>([]);
  const [itemLocationsLoaded, setItemLocationsLoaded] = useState(false);
  const [isItemLocationsLoading, setIsItemLocationsLoading] = useState(false);
  const [itemLocationsError, setItemLocationsError] = useState<string | null>(null);

  const containerId = useMemo(() => {
    const value = searchParams.get('container_id')?.trim() ?? '';
    return value || null;
  }, [searchParams]);
  const selectedTreeContainerId = containerId === looseContainerQueryValue ? looseBucketId : containerId;
  const archivedOnly = readBooleanSearchParam(searchParams, 'archived');
  const aiEnrichedOnly = readBooleanSearchParam(searchParams, 'ai_enriched');
  const missingApproxValueOnly = readBooleanSearchParam(searchParams, 'missing_approx_value');

  const deferredSearch = useDeferredValue(searchInput.trim());
  const normalizedDisposition = dispositionFilter === 'all' ? '' : dispositionFilter;
  const normalizedInventoryGroup = inventoryGroupFilter === 'all' ? '' : inventoryGroupFilter;
  const selectedInventoryGroupFilter = inventoryGroups.find((group) => group.id === normalizedInventoryGroup) ?? null;
  const hasItemFilters = normalizedDisposition !== '' || normalizedInventoryGroup !== '' || inventoryState !== 'current' || archivedOnly || aiEnrichedOnly || missingApproxValueOnly;
  const isFilteredListMode = deferredSearch.length > 0 || hasItemFilters;
  const activeFilterLabels = [
    inventoryState === 'former' ? 'Former inventory' : null,
    inventoryState === 'all' ? 'All inventory' : null,
    normalizedDisposition ? dispositionLabelForCode(dispositions, normalizedDisposition) : null,
    selectedInventoryGroupFilter ? `Inventory Group: ${selectedInventoryGroupFilter.name}` : null,
    archivedOnly ? 'Archived only' : null,
    aiEnrichedOnly ? 'AI Enriched' : null,
    missingApproxValueOnly ? 'Missing Approx Value' : null,
  ].filter((value): value is string => Boolean(value));

  useEffect(() => {
    if (!normalizedDisposition) {
      return;
    }

    if (formerDispositionCodes.has(normalizedDisposition) && inventoryState !== 'former') {
      setInventoryState('former');
      return;
    }

    if (currentDispositionCodes.has(normalizedDisposition) && inventoryState !== 'current') {
      setInventoryState('current');
    }
  }, [inventoryState, normalizedDisposition]);

  const filteredContainerSummaries = useMemo(() => {
    if (!containerId) {
      return containerSummaries;
    }
    return containerSummaries.filter((summary) => summary.container_id === selectedTreeContainerId);
  }, [containerSummaries, containerId, selectedTreeContainerId]);

  useEffect(() => {
    setSearchInput(searchParams.get('search')?.trim() ?? '');
    setDispositionFilter(searchParams.get('disposition_code')?.trim() || 'all');
    setInventoryGroupFilter(searchParams.get('inventory_group_id')?.trim() || 'all');
    setInventoryState(readInventoryStateSearchParam(searchParams));
    setShowArchived(readBooleanSearchParam(searchParams, 'include_archived') || readBooleanSearchParam(searchParams, 'archived'));
    const sortValue = searchParams.get('sort')?.trim() ?? '';
    if (sortValue && sortOptions.some((option) => option.value === sortValue)) {
      setSort(sortValue);
      return;
    }
    setSort('default');
  }, [searchParams]);

  useEffect(() => {
    const nextParams = new URLSearchParams(searchParams);

    if (searchInput.trim()) {
      nextParams.set('search', searchInput.trim());
    } else {
      nextParams.delete('search');
    }

    if (dispositionFilter !== 'all') {
      nextParams.set('disposition_code', dispositionFilter);
    } else {
      nextParams.delete('disposition_code');
    }

    if (inventoryGroupFilter !== 'all') {
      nextParams.set('inventory_group_id', inventoryGroupFilter);
    } else {
      nextParams.delete('inventory_group_id');
    }

    if (inventoryState !== 'current') {
      nextParams.set('inventory_state', inventoryState);
    } else {
      nextParams.delete('inventory_state');
    }

    if (isFilteredListMode && sort !== 'default') {
      nextParams.set('sort', sort);
    } else {
      nextParams.delete('sort');
    }

    if (!archivedOnly && showArchived) {
      nextParams.set('include_archived', 'true');
    } else {
      nextParams.delete('include_archived');
    }

    const currentQuery = searchParams.toString();
    const nextQuery = nextParams.toString();
    if (currentQuery !== nextQuery) {
      setSearchParams(nextParams, { replace: true });
    }
  }, [archivedOnly, dispositionFilter, inventoryGroupFilter, inventoryState, isFilteredListMode, searchInput, searchParams, setSearchParams, showArchived, sort]);

  useEffect(() => {
    let isMounted = true;

    const loadDispositions = async () => {
      try {
        const response = await listItemDispositions();
        if (isMounted) {
          setDispositions(response.dispositions);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load item dispositions', err);
          setDispositionsError(errorMessage(err, 'Failed to load item dispositions.'));
        }
      }
    };

    void loadDispositions();

    return () => {
      isMounted = false;
    };
  }, []);

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
    setOffset(0);
  }, [aiEnrichedOnly, archivedOnly, containerId, deferredSearch, inventoryState, missingApproxValueOnly, normalizedDisposition, normalizedInventoryGroup, showArchived, sort]);

  useEffect(() => {
    setContainerItemsById({});
  }, [aiEnrichedOnly, archivedOnly, containerId, inventoryState, missingApproxValueOnly, normalizedDisposition, normalizedInventoryGroup, showArchived]);

  useEffect(() => {
    if (isFilteredListMode) {
      setIsContainersLoading(false);
      setContainersError(null);
      return;
    }

    let isMounted = true;

    const loadContainerSummaries = async () => {
      setIsContainersLoading(true);
      setContainersError(null);

      try {
        const response = await getInventoryContainerSummaries({
          dispositionCode: normalizedDisposition || undefined,
          aiEnriched: aiEnrichedOnly || undefined,
          missingApproxValue: missingApproxValueOnly || undefined,
          archived: archivedOnly ? true : undefined,
          includeArchived: archivedOnly ? false : showArchived,
        });

        if (!isMounted) {
          return;
        }

        setContainerSummaries(response);
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load inventory container summaries', err);
          setContainersError(errorMessage(err, 'Failed to load inventory container summaries.'));
        }
      } finally {
        if (isMounted) {
          setIsContainersLoading(false);
        }
      }
    };

    void loadContainerSummaries();

    return () => {
      isMounted = false;
    };
  }, [aiEnrichedOnly, archivedOnly, isFilteredListMode, missingApproxValueOnly, normalizedDisposition, reloadKey, showArchived]);

  useEffect(() => {
    const requestSeq = itemListRequestSeq.current + 1;
    itemListRequestSeq.current = requestSeq;

    if (!isFilteredListMode) {
      setIsLoading(false);
      setError(null);
      setItems([]);
      setTotalCount(0);
      setLimit(pageLimit);
      return;
    }

    let isMounted = true;

    const loadItems = async () => {
      setIsLoading(true);
      setError(null);
      setItems([]);
      setTotalCount(0);

      try {
        const response = await listItems({
          containerId: containerId ?? undefined,
          search: deferredSearch || undefined,
          dispositionCode: normalizedDisposition || undefined,
          aiEnriched: aiEnrichedOnly || undefined,
          missingApproxValue: missingApproxValueOnly || undefined,
          archived: archivedOnly ? true : undefined,
          includeArchived: archivedOnly ? false : showArchived,
          inventoryState,
          inventoryGroupId: normalizedInventoryGroup || undefined,
          limit: pageLimit,
          offset,
          sort,
        });

        if (!isMounted || itemListRequestSeq.current !== requestSeq) {
          return;
        }

        setItems(response.items);
        setTotalCount(response.total_count);
        setLimit(response.limit);
      } catch (err) {
        if (isMounted && itemListRequestSeq.current === requestSeq) {
          console.error('Failed to load inventory items', err);
          setError(errorMessage(err, 'Failed to load inventory items.'));
        }
      } finally {
        if (isMounted && itemListRequestSeq.current === requestSeq) {
          setIsLoading(false);
        }
      }
    };

    void loadItems();

    return () => {
      isMounted = false;
    };
  }, [aiEnrichedOnly, archivedOnly, containerId, deferredSearch, inventoryState, isFilteredListMode, missingApproxValueOnly, normalizedDisposition, normalizedInventoryGroup, offset, reloadKey, showArchived, sort]);

  useEffect(() => {
    if (!selectedTreeContainerId || isFilteredListMode) {
      return;
    }
    setExpandedContainerIds((current) => (current.includes(selectedTreeContainerId) ? current : [...current, selectedTreeContainerId]));
  }, [isFilteredListMode, selectedTreeContainerId]);

  useEffect(() => {
    if (!selectedItemId) {
      setDetailItem(null);
      setDetailError(null);
      setIsDetailLoading(false);
      return;
    }

    let isMounted = true;

    const loadItem = async () => {
      setIsDetailLoading(true);
      setDetailError(null);

      try {
        const response = await getItem(selectedItemId);
        if (isMounted) {
          setDetailItem(response.item);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load inventory item detail', err);
          setDetailError(errorMessage(err, 'Failed to load inventory item detail.'));
        }
      } finally {
        if (isMounted) {
          setIsDetailLoading(false);
        }
      }
    };

    void loadItem();

    return () => {
      isMounted = false;
    };
  }, [selectedItemId]);

  useEffect(() => {
    if (!selectedItemId) {
      return;
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        if (deletePreview || deletePreviewError || busyAction === `delete-preview:${selectedItemId}` || busyAction === `delete:${selectedItemId}`) {
          setDeletePreview(null);
          setDeletePreviewError(null);
          return;
        }
        setSelectedItemId(null);
      }
    };

    window.addEventListener('keydown', handleKeyDown);
    return () => {
      window.removeEventListener('keydown', handleKeyDown);
    };
  }, [busyAction, deletePreview, deletePreviewError, selectedItemId]);

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

  const loadMoveTargetContainers = useEffectEvent(async (force = false) => {
    if (isMoveTargetContainersLoading) {
      return;
    }
    if (!force && moveTargetContainersLoaded && moveTargetContainers.length > 0) {
      return;
    }

    setIsMoveTargetContainersLoading(true);
    setMoveTargetContainersError(null);

    try {
      const response = await listContainers();
      setMoveTargetContainers(response.filter((container) => !container.archived));
      setMoveTargetContainersLoaded(true);
    } catch (err) {
      console.error('Failed to load move target containers', err);
      setMoveTargetContainers([]);
      setMoveTargetContainersLoaded(false);
      setMoveTargetContainersError(errorMessage(err, 'Failed to load containers.'));
    } finally {
      setIsMoveTargetContainersLoading(false);
    }
  });

  const loadItemLocations = useEffectEvent(async (force = false) => {
    if (isItemLocationsLoading) {
      return;
    }
    if (!force && itemLocationsLoaded && itemLocations.length > 0) {
      return;
    }

    setIsItemLocationsLoading(true);
    setItemLocationsError(null);

    try {
      const response = await listLocations();
      setItemLocations(response.filter((location) => !location.archived));
      setItemLocationsLoaded(true);
    } catch (err) {
      console.error('Failed to load item locations', err);
      setItemLocations([]);
      setItemLocationsLoaded(false);
      setItemLocationsError(errorMessage(err, 'Failed to load locations.'));
    } finally {
      setIsItemLocationsLoading(false);
    }
  });

  useEffect(() => {
    if (!selectedItemId || moveTargetContainers.length > 0) {
      return;
    }

    void loadMoveTargetContainers();
  }, [loadMoveTargetContainers, moveTargetContainers.length, selectedItemId]);

  useEffect(() => {
    if (!selectedItemId || itemLocations.length > 0) {
      return;
    }

    void loadItemLocations();
  }, [itemLocations.length, loadItemLocations, selectedItemId]);

  const showingStart = totalCount === 0 ? 0 : offset + 1;
  const showingEnd = totalCount === 0 ? 0 : offset + items.length;
  const canGoPrevious = offset > 0;
  const canGoNext = offset + items.length < totalCount;

  const invalidateContainerCache = (containerID: string | null | undefined) => {
    const cacheKey = inventoryBucketKey(containerID);
    if (!cacheKey) {
      return;
    }
    setContainerItemsById((current) => {
      if (!(cacheKey in current)) {
        return current;
      }
      const next = { ...current };
      delete next[cacheKey];
      return next;
    });
  };

  const removeItemFromContainerCache = (containerID: string | null | undefined, itemID: string) => {
    const cacheKey = inventoryBucketKey(containerID);
    if (!cacheKey) {
      return;
    }
    setContainerItemsById((current) => {
      const entry = current[cacheKey];
      if (!entry) {
        return current;
      }
      const nextItems = entry.items.filter((item) => item.id !== itemID);
      if (nextItems.length === entry.items.length) {
        return current;
      }
      return {
        ...current,
        [cacheKey]: {
          ...entry,
          items: nextItems,
        },
      };
    });
  };

  const replaceItemInContainerCache = (containerID: string | null | undefined, item: InventoryItemDetail) => {
    const cacheKey = inventoryBucketKey(containerID);
    if (!cacheKey) {
      return;
    }
    setContainerItemsById((current) => {
      const entry = current[cacheKey];
      if (!entry) {
        return current;
      }
      const existing = entry.items.find((candidate) => candidate.id === item.id);
      if (!existing) {
        return current;
      }
      return {
        ...current,
        [cacheKey]: {
          ...entry,
          items: entry.items.map((candidate) => (candidate.id === item.id ? detailToSummary(candidate, item) : candidate)),
        },
      };
    });
  };

  const loadContainerItems = async (targetContainerId: string, force = false) => {
    const existing = containerItemsById[targetContainerId];
    if (!force && existing && (existing.isLoading || existing.loaded)) {
      return;
    }

    setContainerItemsById((current) => ({
      ...current,
      [targetContainerId]: {
        items: current[targetContainerId]?.items ?? [],
        isLoading: true,
        loaded: false,
        error: null,
      },
    }));

    try {
      const loadedItems: InventoryItemSummary[] = [];
      let nextOffset = 0;
      let expectedTotal = 0;

      do {
        const response = await listItems({
          containerId: targetContainerId === looseBucketId ? looseContainerQueryValue : targetContainerId,
          dispositionCode: normalizedDisposition || undefined,
          aiEnriched: aiEnrichedOnly || undefined,
          missingApproxValue: missingApproxValueOnly || undefined,
          archived: archivedOnly ? true : undefined,
          includeArchived: archivedOnly ? false : showArchived,
          inventoryState,
          limit: maxItemsPerContainerFetch,
          offset: nextOffset,
          sort: 'default',
        });

        loadedItems.push(...response.items);
        expectedTotal = response.total_count;
        nextOffset += response.items.length;

        if (response.items.length === 0) {
          break;
        }
      } while (nextOffset < expectedTotal);

      setContainerItemsById((current) => ({
        ...current,
        [targetContainerId]: {
          items: loadedItems,
          isLoading: false,
          loaded: true,
          error: null,
        },
      }));
    } catch (err) {
      console.error(`Failed to load items for container ${targetContainerId}`, err);
      setContainerItemsById((current) => ({
        ...current,
        [targetContainerId]: {
          items: current[targetContainerId]?.items ?? [],
          isLoading: false,
          loaded: false,
          error: errorMessage(err, 'Failed to load container items.'),
        },
      }));
    }
  };

  const handleSaveItem = async (itemId: string, input: PatchItemInput) => {
    setIsSavingDetail(true);
    try {
      const previousItem = detailItem && detailItem.id === itemId ? detailItem : null;
      const previousContainerId = previousItem?.container?.id ?? null;
      const response = await patchItem(itemId, input);
      const nextContainerId = response.item.container?.id ?? null;
      const movedContainers = previousContainerId !== nextContainerId;
      const previousBucketId = inventoryBucketKey(previousContainerId);
      const nextBucketId = inventoryBucketKey(nextContainerId);

      setDetailItem(response.item);
      setMessage(`Saved inventory item "${response.item.title ?? 'Untitled item'}".`);
      setError(null);
      setItems((current) => {
        if (inventoryState === 'current' && !response.item.current_inventory) {
          return current.filter((item) => item.id !== response.item.id);
        }
        if (inventoryState === 'former' && response.item.current_inventory) {
          return current.filter((item) => item.id !== response.item.id);
        }
        if (movedContainers && containerId && previousBucketId === selectedTreeContainerId && nextBucketId !== selectedTreeContainerId) {
          return current.filter((item) => item.id !== response.item.id);
        }
        return current.map((item) => (item.id === response.item.id ? detailToSummary(item, response.item) : item));
      });

      if (movedContainers) {
        removeItemFromContainerCache(previousContainerId, response.item.id);
        invalidateContainerCache(previousContainerId);
        invalidateContainerCache(nextContainerId);
        if (nextBucketId && expandedContainerIds.includes(nextBucketId)) {
          void loadContainerItems(nextBucketId, true);
        }
        setReloadKey((value) => value + 1);
      } else {
        replaceItemInContainerCache(nextContainerId, response.item);
      }

      return response.item;
    } finally {
      setIsSavingDetail(false);
    }
  };

  const handleArchiveToggle = async (item: InventoryItemDetail) => {
    const nextArchived = !item.archived;
    const confirmed = window.confirm(nextArchived ? 'Archive this item and hide it from the default inventory list?' : 'Unarchive this item and restore it to normal inventory?');
    if (!confirmed) {
      return;
    }

    setBusyAction(`${nextArchived ? 'archive' : 'unarchive'}:${item.id}`);
    try {
      const response = nextArchived ? await archiveItem(item.id) : await unarchiveItem(item.id);
      const updated = response.item;
      setDetailItem(updated);
      setItems((current) => {
        if (updated.archived && !showArchived) {
          return current.filter((entry) => entry.id !== updated.id);
        }
        return current.map((entry) => (entry.id === updated.id ? detailToSummary(entry, updated) : entry));
      });
      invalidateContainerCache(updated.container?.id);
      if (updated.archived && !showArchived) {
        setSelectedItemId(null);
        setDetailItem(null);
      }
      setMessage(updated.archived ? `Archived "${updated.title ?? 'Untitled item'}".` : `Unarchived "${updated.title ?? 'Untitled item'}".`);
      setError(null);
      setReloadKey((value) => value + 1);
    } catch (err) {
      console.error(updatedArchiveLabel(nextArchived), err);
      setDetailError(errorMessage(err, nextArchived ? 'Failed to archive item.' : 'Failed to unarchive item.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleRequestDelete = async (item: InventoryItemDetail) => {
    setDeletePreview(null);
    setDeletePreviewError(null);
    setBusyAction(`delete-preview:${item.id}`);
    try {
      const preview = await getItemDeletePreview(item.id);
      setDeletePreview(preview);
    } catch (err) {
      console.error('Failed to load item delete preview', err);
      setDeletePreviewError(errorMessage(err, 'Failed to load item delete preview.'));
    } finally {
      setBusyAction(null);
    }
  };

  const handleConfirmDelete = async (item: InventoryItemDetail): Promise<ItemDeleteResponse | null> => {
    setBusyAction(`delete:${item.id}`);
    setDeletePreviewError(null);
    try {
      const response = await deleteItem(item.id);
      setItems((current) => current.filter((entry) => entry.id !== item.id));
      invalidateContainerCache(item.container?.id ?? detailItem?.container?.id);
      setSelectedItemId(null);
      setDetailItem(null);
      setDeletePreview(null);
      const warningNote = response.warnings.length > 0 ? ` Warnings: ${response.warnings.join(' ')}` : '';
      setMessage(`Deleted "${item.title ?? 'Untitled item'}". Removed ${response.deleted_image_asset_count} image rows and ${response.deleted_file_count} files.${warningNote}`);
      setError(null);
      setReloadKey((value) => value + 1);
      return response;
    } catch (err) {
      console.error('Failed to delete inventory item', err);
      const messageValue = errorMessage(err, 'Failed to delete inventory item.');
      setDeletePreviewError(messageValue);
      setDetailError(messageValue);
      return null;
    } finally {
      setBusyAction(null);
    }
  };

  return (
    <div className="grid gap-6">
      <Panel
        title="Inventory"
        eyebrow="Approved items"
        action={
          <button
            type="button"
            onClick={() => {
              setMessage(null);
              setError(null);
              setContainersError(null);
              setContainerItemsById({});
              setReloadKey((value) => value + 1);
            }}
            className="rounded-md border border-rack-steel/30 px-4 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
          >
            Refresh
          </button>
        }
      >
        <div className="grid gap-4">
          <p className="text-sm text-stone-300">
            {isFilteredListMode
              ? deferredSearch
                ? 'Global item search across all containers.'
                : 'Filtered inventory items across all containers.'
              : 'Containers load first. Expand a container to fetch its current inventory items.'}
          </p>
          {containerId ? <p className="text-sm text-amberline-100">Inventory filtered by container_id: {containerId}</p> : null}
          {activeFilterLabels.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {activeFilterLabels.map((label) => (
                <span key={label} className="rounded-full border border-amberline-400/30 bg-amberline-500/10 px-2.5 py-1 text-xs font-semibold uppercase tracking-[0.14em] text-amberline-100">
                  {label}
                </span>
              ))}
            </div>
          ) : null}

          <div className={`grid min-w-0 gap-3 ${isFilteredListMode ? 'xl:grid-cols-[minmax(0,1.6fr)_12rem_12rem_12rem_12rem_auto]' : 'xl:grid-cols-[minmax(0,1.6fr)_12rem_12rem_12rem_auto]'} xl:items-end`}>
            <label className="grid min-w-0 gap-2 text-sm text-stone-200">
              Search
              <input
                value={searchInput}
                onChange={(event) => setSearchInput(event.target.value)}
                placeholder="Search title, description, container, or location"
                className="field-input"
              />
            </label>

            <label className="grid min-w-0 gap-2 text-sm text-stone-200">
              Disposition
              <select
                value={dispositionFilter}
                onChange={(event) => setDispositionFilter(event.target.value)}
                className="field-input"
              >
                <option value="all">All dispositions</option>
                {dispositions.map((disposition) => (
                  <option key={disposition.code} value={disposition.code}>
                    {disposition.label}
                  </option>
                ))}
              </select>
            </label>

            <label className="grid min-w-0 gap-2 text-sm text-stone-200">
              Inventory
              <select
                value={inventoryState}
                onChange={(event) => setInventoryState(event.target.value as InventoryState)}
                className="field-input"
              >
                <option value="current">Current</option>
                <option value="former">Former</option>
                <option value="all">All</option>
              </select>
            </label>

            <label className="grid min-w-0 gap-2 text-sm text-stone-200">
              Inventory Group
              <select
                value={inventoryGroupFilter}
                onChange={(event) => setInventoryGroupFilter(event.target.value)}
                disabled={isInventoryGroupsLoading}
                className="field-input disabled:cursor-not-allowed disabled:opacity-50"
              >
                <option value="all">All groups</option>
                {inventoryGroups.map((group) => (
                  <option key={group.id} value={group.id}>
                    {group.name}
                  </option>
                ))}
              </select>
            </label>

            {isFilteredListMode ? (
              <label className="grid min-w-0 gap-2 text-sm text-stone-200">
                Sort
                <select
                  value={sort}
                  onChange={(event) => setSort(event.target.value)}
                  className="field-input"
                >
                  {sortOptions.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </select>
              </label>
            ) : null}

            <button
              type="button"
              onClick={() => {
                setSearchInput('');
                setDispositionFilter('all');
                setInventoryGroupFilter('all');
                setInventoryState('current');
                setSort('default');
                setShowArchived(false);
                setSearchParams({});
              }}
              className="w-full rounded-md border border-rack-steel/30 px-4 py-3 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 xl:w-auto"
            >
              Clear filters
            </button>
          </div>

          <label className="flex items-center gap-2 text-sm text-stone-300">
            <input
              type="checkbox"
              checked={showArchived}
              onChange={(event) => setShowArchived(event.target.checked)}
              disabled={archivedOnly}
              className="h-4 w-4 rounded border-rack-steel/35 bg-rack-soot/90 text-amberline-400 focus:ring-amberline-400"
            />
            {archivedOnly ? 'Showing archived only (URL filter)' : 'Show archived'}
          </label>

          <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-3 text-sm text-stone-300">
            <span>Showing {showingStart}-{showingEnd} of {totalCount}</span>
            <div className="flex items-center gap-2">
              <button
                type="button"
                onClick={() => setOffset((current) => Math.max(0, current - limit))}
                disabled={!canGoPrevious}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Previous
              </button>
              <button
                type="button"
                onClick={() => setOffset((current) => current + limit)}
                disabled={!canGoNext}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Next
              </button>
            </div>
          </div>

          {message ? <p className="rounded-md border border-signal-green/35 bg-signal-green/10 px-3 py-2 text-sm text-green-100">{message}</p> : null}
          {isFilteredListMode && error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          {!isFilteredListMode && containersError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{containersError}</p> : null}
          {dispositionsError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{dispositionsError}</p> : null}
        </div>
      </Panel>

      {isFilteredListMode ? (
        <Panel title="Inventory index" eyebrow={deferredSearch ? 'Search results' : 'Filtered results'}>
          <div className="grid gap-2">
            <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-3 text-sm text-stone-300">
              <span>Showing {showingStart}-{showingEnd} of {totalCount}</span>
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  onClick={() => setOffset((current) => Math.max(0, current - limit))}
                  disabled={!canGoPrevious}
                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Previous
                </button>
                <button
                  type="button"
                  onClick={() => setOffset((current) => current + limit)}
                  disabled={!canGoNext}
                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  Next
                </button>
              </div>
            </div>

            <div className="hidden grid-cols-[5rem_minmax(0,2fr)_minmax(0,1fr)_9rem_8rem] gap-3 border-b border-rack-steel/20 px-3 pb-2 text-xs font-semibold uppercase tracking-[0.18em] text-rack-glass lg:grid">
              <span>Image</span>
              <span>Item</span>
              <span>Container</span>
              <span>Value</span>
              <span>Images</span>
            </div>

            {isLoading ? <p className="px-3 py-6 text-sm text-stone-400">Loading inventory items...</p> : null}

            {!isLoading && items.length === 0 ? (
              <p className="px-3 py-6 text-sm text-stone-300">No inventory items found.</p>
            ) : null}

            {!isLoading
              ? items.map((item) => (
                  <InventoryRow
                    key={item.id}
                    item={item}
                    showArchived={showArchived}
                    onOpen={() => {
                      setSelectedItemId(item.id);
                      setMessage(null);
                    }}
                  />
                ))
              : null}
          </div>
        </Panel>
      ) : (
        <Panel title="Containers" eyebrow="Expandable inventory tree">
          <div className="grid gap-3">
            {isContainersLoading ? <p className="px-3 py-6 text-sm text-stone-400">Loading container summaries...</p> : null}

            {!isContainersLoading && filteredContainerSummaries.length === 0 ? (
              <p className="px-3 py-6 text-sm text-stone-300">{containerId ? 'No matching container found.' : 'No containers found.'}</p>
            ) : null}

            {!isContainersLoading
              ? filteredContainerSummaries.map((summary) => (
                  <InventoryContainerTreeRow
                    key={summary.container_id}
                    summary={summary}
                    showArchived={showArchived}
                    expanded={expandedContainerIds.includes(summary.container_id)}
                    itemsEntry={containerItemsById[summary.container_id]}
                    onToggle={() => {
                      setExpandedContainerIds((current) =>
                        current.includes(summary.container_id)
                          ? current.filter((value) => value !== summary.container_id)
                          : [...current, summary.container_id],
                      );
                    }}
                    onEnsureLoaded={() => void loadContainerItems(summary.container_id)}
                    onOpenItem={(item) => {
                      setSelectedItemId(item.id);
                      setMessage(null);
                    }}
                  />
                ))
              : null}
          </div>
        </Panel>
      )}

      {selectedItemId ? (
        <InventoryDetailModal
          item={detailItem}
          isLoading={isDetailLoading}
          isSaving={isSavingDetail}
          error={detailError}
          busyAction={busyAction}
          deletePreview={deletePreview}
          deletePreviewError={deletePreviewError}
          dispositions={dispositions}
          inventoryGroups={inventoryGroups}
          inventoryGroupsLoading={isInventoryGroupsLoading}
          inventoryGroupsError={inventoryGroupsError}
          containers={moveTargetContainers}
          containersLoading={isMoveTargetContainersLoading}
          containersError={moveTargetContainersError}
          locations={itemLocations}
          locationsLoading={isItemLocationsLoading}
          locationsError={itemLocationsError}
          onStartEditing={() => {
            void loadMoveTargetContainers(true);
            void loadItemLocations(true);
          }}
          onClose={() => {
            setSelectedItemId(null);
            setDetailItem(null);
            setDetailError(null);
            setDeletePreview(null);
            setDeletePreviewError(null);
          }}
          onSave={handleSaveItem}
          onReplaceItem={(updated) => {
            setDetailItem(updated);
            setItems((current) => current.map((entry) => (entry.id === updated.id ? detailToSummary(entry, updated) : entry)));
            invalidateContainerCache(updated.container?.id);
            setSelectedImage((current) => {
              if (!current) {
                return current;
              }
              return updated.images.some((image) => image.image_asset_id === current.image.image_asset_id) ? current : null;
            });
            setReloadKey((value) => value + 1);
          }}
          onArchiveToggle={handleArchiveToggle}
          onRequestDelete={handleRequestDelete}
          onConfirmDelete={handleConfirmDelete}
          onClearDeleteState={() => {
            setDeletePreview(null);
            setDeletePreviewError(null);
          }}
          onOpenImage={(image) =>
            setSelectedImage({
              itemTitle: detailItem?.title ?? null,
              containerName: detailItem?.container?.name ?? null,
              image,
            })
          }
        />
      ) : null}

      {selectedImage ? (
        <FullSizeImageModal
          image={selectedImage.image}
          itemTitle={selectedImage.itemTitle}
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

function InventoryRow({ item, showArchived, onOpen }: { item: InventoryItemSummary; showArchived: boolean; onOpen: () => void }) {
  const [previewFailed, setPreviewFailed] = useState(false);
  const title = item.title ?? 'Untitled item';
  const snippet = truncateText(item.description?.trim() || 'No description set.', 120);

  useEffect(() => {
    setPreviewFailed(false);
  }, [item.id, item.primary_image?.image_asset_id]);

  return (
    <button
      type="button"
      onClick={onOpen}
      className={`grid gap-3 rounded-md border px-3 py-3 text-left transition hover:border-amberline-400/35 hover:bg-rack-steel/10 lg:grid-cols-[5rem_minmax(0,2fr)_minmax(0,1fr)_9rem_8rem] lg:items-center ${
        item.archived ? 'border-red-400/18 bg-rack-soot/45 opacity-80' : 'border-rack-steel/22 bg-rack-soot/60'
      }`}
    >
      <div className="flex h-20 w-20 items-center justify-center overflow-hidden rounded-md border border-rack-steel/20 bg-black/25 lg:h-14 lg:w-14">
        {item.primary_image && !previewFailed ? (
          <img
            src={imageUrl(item.primary_image.image_asset_id, 'thumbnail')}
            alt={title}
            loading="lazy"
            onError={() => setPreviewFailed(true)}
            className="h-full w-full object-cover"
          />
        ) : (
          <span className="px-2 text-center text-[11px] text-stone-500">{item.primary_image ? 'Image unavailable' : 'No image'}</span>
        )}
      </div>

      <div className="grid gap-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold text-stone-100">{title}</span>
          {item.inventory_group_name ? <CompactBadge>{item.inventory_group_name}</CompactBadge> : null}
          {item.disposition_label ? <CompactBadge>{item.disposition_label}</CompactBadge> : null}
          {!item.current_inventory ? <CompactBadge>Former</CompactBadge> : null}
          {showArchived && item.archived ? <CompactBadge>Archived</CompactBadge> : null}
        </div>
        <p className="text-xs text-stone-400">{snippet}</p>
        <p className="truncate text-[11px] text-stone-500">{item.id}</p>
      </div>

      <div className="grid gap-1 text-xs text-stone-300">
        <span>{item.container?.name ?? 'No Container / Loose Item'}</span>
        <span className="text-stone-500">{inventoryItemLocationLabel(item)}</span>
      </div>

      <div className="grid gap-1 text-xs text-stone-300">
        <span>Approx: {formatCurrencyLike(item.approx_value)}</span>
        <span>Sale: {formatCurrencyLike(item.sold_price)}</span>
      </div>

      <div className="grid gap-1 text-xs text-stone-300">
        <span>{item.image_count} images</span>
        <span className="text-stone-500">{new Date(item.created_datetime).toLocaleDateString()}</span>
      </div>
    </button>
  );
}

function InventoryContainerTreeRow({
  summary,
  showArchived,
  expanded,
  itemsEntry,
  onToggle,
  onEnsureLoaded,
  onOpenItem,
}: {
  summary: InventoryContainerSummary;
  showArchived: boolean;
  expanded: boolean;
  itemsEntry: ContainerItemCacheEntry | undefined;
  onToggle: () => void;
  onEnsureLoaded: () => void;
  onOpenItem: (item: InventoryItemSummary) => void;
}) {
  useEffect(() => {
    if (expanded && !itemsEntry?.loaded && !itemsEntry?.isLoading) {
      onEnsureLoaded();
    }
  }, [expanded, itemsEntry?.isLoading, itemsEntry?.loaded, onEnsureLoaded]);

  const containerTypeLabel = summary.container_type_name ?? summary.container_type;
  const locationLabel = summary.is_synthetic ? 'Loose items across all locations' : summary.location_name ?? summary.location_description ?? 'No location';

  return (
    <div className="rounded-md border border-rack-steel/24 bg-rack-soot/60">
      <button
        type="button"
        onClick={onToggle}
        className="grid w-full gap-3 px-3 py-3 text-left transition hover:bg-rack-steel/10 lg:grid-cols-[1.8rem_minmax(0,1.8fr)_minmax(0,1.2fr)_10rem_10rem_10rem] lg:items-center"
      >
        <span className="text-lg text-amberline-200">{expanded ? '▾' : '▸'}</span>
        <div className="grid gap-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-sm font-semibold text-stone-100">{summary.container_name}</span>
            {containerTypeLabel ? <CompactBadge>{containerTypeLabel}</CompactBadge> : null}
            {summary.is_synthetic ? <CompactBadge>Loose</CompactBadge> : null}
          </div>
          {summary.container_type_name && summary.container_type ? <p className="text-[11px] text-stone-500">Legacy type: {summary.container_type}</p> : null}
          <p className="text-xs text-stone-400">{locationLabel}</p>
          {summary.location_name && summary.location_description ? <p className="text-[11px] text-stone-500">Legacy: {summary.location_description}</p> : null}
          {summary.notes ? <p className="line-clamp-2 text-xs text-stone-500">{summary.notes}</p> : null}
        </div>
        <div className="grid gap-1 text-xs text-stone-300">
          <span>Visible items: {summary.item_count}</span>
          <span className="text-stone-500">Archived: {summary.archived_item_count}</span>
        </div>
        <div className="grid gap-1 text-xs text-stone-300">
          <span>Value: {formatCurrencyNumber(summary.total_approx_value)}</span>
          <span className="text-stone-500">For Sale: {summary.for_sale_count}</span>
        </div>
        <div className="grid gap-1 text-xs text-stone-300">
          <span>In Use: {summary.in_use_count}</span>
          <span className="text-stone-500">Sale Pending: {summary.sale_pending_count}</span>
        </div>
        <div className="grid gap-1 text-xs text-stone-300">
          <span>{summary.latest_item_datetime ? new Date(summary.latest_item_datetime).toLocaleDateString() : 'No items yet'}</span>
          <span className="text-stone-500">{showArchived ? 'Showing archived' : 'Current inventory only'}</span>
        </div>
      </button>

      {expanded ? (
        <div className="border-t border-rack-steel/20 bg-black/15 px-3 py-3">
          {itemsEntry?.isLoading ? <p className="py-4 text-sm text-stone-400">Loading container items...</p> : null}
          {itemsEntry?.error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{itemsEntry.error}</p> : null}
          {itemsEntry?.loaded && itemsEntry.items.length === 0 ? <p className="py-4 text-sm text-stone-300">No items in this container.</p> : null}
          {itemsEntry?.loaded && itemsEntry.items.length > 0 ? (
            <div className="grid gap-2 border-l border-rack-steel/20 pl-3">
              {itemsEntry.items.map((item) => (
                <InventoryRow key={item.id} item={item} showArchived={showArchived} onOpen={() => onOpenItem(item)} />
              ))}
            </div>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

function InventoryDetailModal({
  item,
  dispositions,
  inventoryGroups,
  inventoryGroupsLoading,
  inventoryGroupsError,
  containers,
  containersLoading,
  containersError,
  locations,
  locationsLoading,
  locationsError,
  onStartEditing,
  isLoading,
  isSaving,
  error,
  busyAction,
  deletePreview,
  deletePreviewError,
  onClose,
  onSave,
  onReplaceItem,
  onArchiveToggle,
  onRequestDelete,
  onConfirmDelete,
  onClearDeleteState,
  onOpenImage,
}: {
  item: InventoryItemDetail | null;
  dispositions: ItemDisposition[];
  inventoryGroups: InventoryGroup[];
  inventoryGroupsLoading: boolean;
  inventoryGroupsError: string | null;
  containers: ContainerOption[];
  containersLoading: boolean;
  containersError: string | null;
  locations: LocationOption[];
  locationsLoading: boolean;
  locationsError: string | null;
  onStartEditing: () => void;
  isLoading: boolean;
  isSaving: boolean;
  error: string | null;
  busyAction: string | null;
  deletePreview: ItemDeletePreview | null;
  deletePreviewError: string | null;
  onClose: () => void;
  onSave: (itemId: string, input: PatchItemInput) => Promise<InventoryItemDetail>;
  onReplaceItem: (item: InventoryItemDetail) => void;
  onArchiveToggle: (item: InventoryItemDetail) => Promise<void>;
  onRequestDelete: (item: InventoryItemDetail) => Promise<void>;
  onConfirmDelete: (item: InventoryItemDetail) => Promise<ItemDeleteResponse | null>;
  onClearDeleteState: () => void;
  onOpenImage: (image: InventoryImage) => void;
}) {
  const [isEditing, setIsEditing] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saveMessage, setSaveMessage] = useState<string | null>(null);
  const [title, setTitle] = useState('');
  const [description, setDescription] = useState('');
  const [approxValue, setApproxValue] = useState('');
  const [soldPrice, setSoldPrice] = useState('');
  const [soldDate, setSoldDate] = useState('');
  const [notes, setNotes] = useState('');
  const [dispositionCode, setDispositionCode] = useState('');
  const [inventoryGroupId, setInventoryGroupId] = useState('');
  const [containerId, setContainerId] = useState('');
  const [locationId, setLocationId] = useState('');
  const [locationDetail, setLocationDetail] = useState('');
  const [sellProviders, setSellProviders] = useState<PublicSellProvider[]>([]);
  const [listingDrafts, setListingDrafts] = useState<ListingDraft[]>([]);
  const [sellLoading, setSellLoading] = useState(false);
  const [sellError, setSellError] = useState<string | null>(null);
  const [draftActionError, setDraftActionError] = useState<string | null>(null);
  const [openingProviderType, setOpeningProviderType] = useState<string | null>(null);
  const [activeDraft, setActiveDraft] = useState<ListingDraft | null>(null);
  const [isDraftModalOpen, setIsDraftModalOpen] = useState(false);
  const [isDraftSaving, setIsDraftSaving] = useState(false);
  const [isDraftDeleting, setIsDraftDeleting] = useState(false);
  const [isUploadingImages, setIsUploadingImages] = useState(false);
  const [imageUploadError, setImageUploadError] = useState<string | null>(null);
  const [imageUploadEntries, setImageUploadEntries] = useState<ItemImageUploadEntry[]>([]);
  const [imageDeleteTarget, setImageDeleteTarget] = useState<InventoryImage | null>(null);
  const [isDeletingImage, setIsDeletingImage] = useState(false);
  const [imageDeleteError, setImageDeleteError] = useState<string | null>(null);
  const [dispositionHistory, setDispositionHistory] = useState<ItemDispositionHistoryEntry[]>([]);
  const [isDispositionHistoryLoading, setIsDispositionHistoryLoading] = useState(false);
  const [dispositionHistoryError, setDispositionHistoryError] = useState<string | null>(null);

  useEffect(() => {
    if (!item) {
      return;
    }
    setTitle(item.title ?? '');
    setDescription(item.description ?? '');
    setApproxValue(item.approx_value ?? '');
    setSoldPrice(item.sold_price ?? '');
    setSoldDate(item.sold_date ?? '');
    setNotes(item.notes ?? '');
    setDispositionCode(item.disposition_code ?? '');
    setInventoryGroupId(item.inventory_group_id ?? '');
    setContainerId(item.container?.id ?? looseContainerSelectValue);
    setLocationId(item.location_id ?? '');
    setLocationDetail(item.location_detail ?? '');
    setSaveError(null);
    setSaveMessage(null);
    setImageUploadError(null);
    setImageUploadEntries([]);
    setImageDeleteTarget(null);
    setImageDeleteError(null);
    setIsDeletingImage(false);
    setIsEditing(false);
    setDraftActionError(null);
    setActiveDraft(null);
    setIsDraftModalOpen(false);
  }, [item]);

  useEffect(() => {
    if (!item) {
      setDispositionHistory([]);
      setDispositionHistoryError(null);
      setIsDispositionHistoryLoading(false);
      return;
    }

    let isMounted = true;

    const loadHistory = async () => {
      setIsDispositionHistoryLoading(true);
      setDispositionHistoryError(null);
      try {
        const response = await listItemDispositionHistory(item.id);
        if (isMounted) {
          setDispositionHistory(response.history);
        }
      } catch (err) {
        if (isMounted) {
          console.error('Failed to load item disposition history', err);
          setDispositionHistoryError(errorMessage(err, 'Failed to load disposition history.'));
        }
      } finally {
        if (isMounted) {
          setIsDispositionHistoryLoading(false);
        }
      }
    };

    void loadHistory();

    return () => {
      isMounted = false;
    };
  }, [item]);

  useEffect(() => {
    if (!item) {
      setSellProviders([]);
      setListingDrafts([]);
      setSellError(null);
      setDraftActionError(null);
      setSellLoading(false);
      return;
    }

    let isMounted = true;

    const loadSellData = async () => {
      setSellLoading(true);
      setSellError(null);

      try {
        const [providersResponse, draftsResponse] = await Promise.all([
          listEnabledSellProviders(),
          listItemListingDrafts(item.id),
        ]);
        if (!isMounted) {
          return;
        }
        setSellProviders(providersResponse.providers);
        setListingDrafts(draftsResponse.drafts);
      } catch (err) {
        if (!isMounted) {
          return;
        }
        console.error('Failed to load sell providers or listing drafts', err);
        setSellError(errorMessage(err, 'Failed to load listing draft providers.'));
      } finally {
        if (isMounted) {
          setSellLoading(false);
        }
      }
    };

    void loadSellData();

    return () => {
      isMounted = false;
    };
  }, [item]);

  const isArchiveBusy = item ? busyAction === `archive:${item.id}` || busyAction === `unarchive:${item.id}` : false;
  const isDeletePreviewLoading = item ? busyAction === `delete-preview:${item.id}` : false;
  const isDeleting = item ? busyAction === `delete:${item.id}` : false;
  const isLooseItem = containerId === looseContainerSelectValue;
  const selectedContainerOption = useMemo(() => {
    if (!containerId || containerId === looseContainerSelectValue) {
      return null;
    }
    return containers.find((container) => container.id === containerId) ?? null;
  }, [containerId, containers]);
  const selectedLocationOption = useMemo(() => {
    if (!locationId) {
      return null;
    }
    return locations.find((location) => location.id === locationId) ?? null;
  }, [locationId, locations]);
  const selectedInventoryGroupOption = useMemo(() => {
    if (!inventoryGroupId) {
      return null;
    }
    return inventoryGroups.find((group) => group.id === inventoryGroupId) ?? null;
  }, [inventoryGroupId, inventoryGroups]);
  const providerDraftMap = useMemo(() => {
    const next = new Map<string, ListingDraft>();
    for (const draft of listingDrafts) {
      if (!next.has(draft.provider_type)) {
        next.set(draft.provider_type, draft);
      }
    }
    return next;
  }, [listingDrafts]);

  const handleSave = async () => {
    if (!item) {
      return;
    }

    try {
      setSaveError(null);
      setSaveMessage(null);

      const normalizedTitle = title.trim();
      if (!normalizedTitle) {
        throw new Error('Title is required.');
      }

      const normalizedApprox = approxValue.trim();
      const normalizedSold = soldPrice.trim();
      const normalizedSoldDate = soldDate.trim();
      if (normalizedApprox && !moneyPattern.test(normalizedApprox)) {
        throw new Error('Approx Value must be a non-negative decimal with up to two decimal places.');
      }
      if (normalizedSold && !moneyPattern.test(normalizedSold)) {
        throw new Error('Sale Price must be a non-negative decimal with up to two decimal places.');
      }
      if (!inventoryGroupId) {
        throw new Error('Inventory Group is required.');
      }
      if (!dispositionCode) {
        throw new Error('Disposition is required.');
      }

      const patch: PatchItemInput = {
        title: normalizedTitle,
        description: description.trim() || null,
        approx_value: normalizedApprox || null,
        sold_price: normalizedSold || null,
        sold_date: normalizedSoldDate || null,
        notes: notes.trim(),
        disposition_code: dispositionCode,
        container_id: isLooseItem ? null : containerId || null,
        inventory_group_id: inventoryGroupId,
      };
      if (isLooseItem) {
        patch.location_id = locationId || null;
        patch.location_detail = locationDetail.trim() || null;
      }

      const updated = await onSave(item.id, patch);

      setTitle(updated.title ?? '');
      setDescription(updated.description ?? '');
      setApproxValue(updated.approx_value ?? '');
      setSoldPrice(updated.sold_price ?? '');
      setSoldDate(updated.sold_date ?? '');
      setNotes(updated.notes ?? '');
      setDispositionCode(updated.disposition_code ?? '');
      setInventoryGroupId(updated.inventory_group_id ?? '');
      setContainerId(updated.container?.id ?? looseContainerSelectValue);
      setLocationId(updated.location_id ?? '');
      setLocationDetail(updated.location_detail ?? '');
      setIsEditing(false);
      setSaveMessage('Item saved.');
    } catch (err) {
      console.error('Failed to save inventory item', err);
      setSaveError(errorMessage(err, 'Failed to save inventory item.'));
    }
  };

  const handleOpenDraftForProvider = async (provider: PublicSellProvider) => {
    if (!item) {
      return;
    }

    setOpeningProviderType(provider.provider_type);
    setDraftActionError(null);
    try {
      const response = await createOrOpenListingDraft(item.id, {
        sell_provider_config_id: provider.id,
        provider_type: provider.provider_type,
      });
      let draft = response.draft;
      try {
        draft = await handlePrepareDraftPhotos(response.draft.id);
      } catch (err) {
        setDraftActionError(errorMessage(err, `Opened ${provider.display_name} draft, but failed to prepare listing photos.`));
      }
      setActiveDraft(draft);
      setIsDraftModalOpen(true);
      setListingDrafts((current) => replaceListingDraft(current, draft));
    } catch (err) {
      console.error('Failed to create or open listing draft', err);
      setDraftActionError(errorMessage(err, `Failed to open ${provider.display_name} draft.`));
    } finally {
      setOpeningProviderType(null);
    }
  };

  const handlePrepareDraftPhotos = async (draftId: string) => {
    const response = await prepareListingDraftPhotos(draftId);
    setActiveDraft((current) => (current?.id === draftId ? mergeListingDraft(current, response.draft) : current));
    setListingDrafts((current) => replaceListingDraft(current, response.draft));
    return response.draft;
  };

  const handleSaveDraft = async (draftId: string, input: {
    title: string;
    description: string | null;
    asking_price: string | null;
    currency: string;
    status: ListingDraftStatus;
    listing_url: string | null;
    notes: string | null;
  }) => {
    setIsDraftSaving(true);
    try {
      const response = await updateListingDraft(draftId, input);
      setActiveDraft(response.draft);
      setListingDrafts((current) => replaceListingDraft(current, response.draft));
      return response.draft;
    } finally {
      setIsDraftSaving(false);
    }
  };

  const handleDeleteDraft = async (draftId: string) => {
    setIsDraftDeleting(true);
    try {
      await deleteListingDraft(draftId);
      setListingDrafts((current) => current.filter((draft) => draft.id !== draftId));
      setActiveDraft((current) => (current?.id === draftId ? null : current));
      setIsDraftModalOpen(false);
    } finally {
      setIsDraftDeleting(false);
    }
  };

  const handleSelectImages = async (fileList: FileList | null) => {
    if (!item || !fileList || fileList.length === 0) {
      return;
    }

    setIsUploadingImages(true);
    setImageUploadError(null);
    setSaveMessage(null);

    try {
      const files = Array.from(fileList);
      const response = await uploadItemImages(item.id, files, (entries) => setImageUploadEntries(entries));
      onReplaceItem(response.item);
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'complete', progress_percent: 100, loaded_bytes: entry.total_bytes })));
      setSaveMessage(`Uploaded ${files.length} image${files.length === 1 ? '' : 's'} and regenerated item variants.`);
    } catch (err) {
      console.error('Failed to upload item images', err);
      setImageUploadError(errorMessage(err, 'Failed to upload item images.'));
      setImageUploadEntries((entries) => entries.map((entry) => ({ ...entry, status: 'failed' })));
    } finally {
      setIsUploadingImages(false);
    }
  };

  const handleDeleteImage = async (): Promise<ItemImageDeleteResponse | null> => {
    if (!item || !imageDeleteTarget) {
      return null;
    }

    setIsDeletingImage(true);
    setImageDeleteError(null);
    setSaveMessage(null);

    try {
      const response = await deleteItemImage(item.id, imageDeleteTarget.image_asset_id);
      onReplaceItem(response.item);
      setImageDeleteTarget(null);

      const warningNote = response.warnings.length > 0 ? ` ${response.warnings.join(' ')}` : '';
      setSaveMessage(`Removed image "${imageDeleteTarget.original_filename ?? imageDeleteTarget.stored_filename ?? imageDeleteTarget.image_asset_id}" and deleted ${response.deleted_file_count} file${response.deleted_file_count === 1 ? '' : 's'}.${warningNote}`);
      return response;
    } catch (err) {
      console.error('Failed to delete item image', err);
      setImageDeleteError(errorMessage(err, 'Failed to delete item image.'));
      return null;
    } finally {
      setIsDeletingImage(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/75 px-4 py-6" onClick={onClose}>
      <div className="max-h-[92vh] w-full max-w-6xl overflow-y-auto overflow-x-hidden" onClick={(event) => event.stopPropagation()}>
        <Panel
          title={item?.title ?? 'Inventory item'}
          eyebrow="Item detail"
          action={
            <div className="flex flex-wrap gap-2">
              {item ? (
                isEditing ? (
                  <>
                    <button
                      type="button"
                      onClick={() => {
                        setIsEditing(false);
                        setSaveError(null);
                      }}
                      className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
                    >
                      Cancel
                    </button>
                    <button
                      type="button"
                      onClick={handleSave}
                      disabled={isSaving}
                      className="rounded-md border border-amberline-400/45 bg-amberline-500/15 px-3 py-2 text-sm font-medium text-amberline-100 hover:bg-amberline-500/25 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isSaving ? 'Saving...' : 'Save'}
                    </button>
                  </>
                ) : (
                  <>
                    <button
                      type="button"
                      onClick={() => {
                        onStartEditing();
                        setIsEditing(true);
                      }}
                      className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
                    >
                      Edit
                    </button>
                    <button
                      type="button"
                      disabled={isUploadingImages || isDeletingImage || isArchiveBusy || isDeletePreviewLoading || isDeleting}
                      onClick={() => document.getElementById(`item-image-upload-${item.id}`)?.click()}
                      className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isUploadingImages ? 'Uploading images...' : 'Add Images'}
                    </button>
                    <button
                      type="button"
                      disabled={isArchiveBusy || isDeletePreviewLoading || isDeleting}
                      onClick={() => void onArchiveToggle(item)}
                      className="rounded-md border border-copper-500/35 px-3 py-2 text-sm font-medium text-amberline-100 hover:bg-copper-500/12 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isArchiveBusy ? 'Updating...' : item.archived ? 'Unarchive' : 'Archive'}
                    </button>
                    <button
                      type="button"
                      disabled={isArchiveBusy || isDeletePreviewLoading || isDeleting}
                      onClick={() => void onRequestDelete(item)}
                      className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
                    >
                      {isDeletePreviewLoading ? 'Preparing delete...' : isDeleting ? 'Deleting...' : 'Delete'}
                    </button>
                  </>
                )
              ) : null}
              <button
                type="button"
                onClick={onClose}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
              >
                Close
              </button>
            </div>
          }
        >
          {isLoading ? <p className="text-sm text-stone-400">Loading inventory item...</p> : null}
          {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          {saveError ? <p className="mb-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{saveError}</p> : null}
          {saveMessage ? <p className="mb-3 rounded-md border border-signal-green/35 bg-signal-green/10 px-3 py-2 text-sm text-green-100">{saveMessage}</p> : null}
          {imageUploadError ? <p className="mb-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageUploadError}</p> : null}
          {imageDeleteError ? <p className="mb-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{imageDeleteError}</p> : null}
          {item ? (
            <input
              id={`item-image-upload-${item.id}`}
              type="file"
              accept="image/jpeg,image/png"
              multiple
              className="hidden"
              onChange={(event) => {
                const files = event.target.files;
                void handleSelectImages(files);
                event.currentTarget.value = '';
              }}
            />
          ) : null}

          {item && !isLoading ? (
            <div className="grid min-w-0 gap-5 lg:grid-cols-[minmax(0,1.2fr)_minmax(18rem,24rem)] lg:items-start">
              <div className="grid min-w-0 gap-5">
                <section className="grid gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
                  {isEditing ? (
                    <>
                      <label className="grid gap-2 text-sm text-stone-200">
                        Title
                        <input
                          value={title}
                          onChange={(event) => setTitle(event.target.value)}
                          className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                        />
                      </label>
                      <label className="grid gap-2 text-sm text-stone-200">
                        Description
                        <textarea
                          value={description}
                          onChange={(event) => setDescription(event.target.value)}
                          rows={5}
                          className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                        />
                      </label>
                      <label className="grid gap-2 text-sm text-stone-200">
                        Notes
                        <textarea
                          value={notes}
                          onChange={(event) => setNotes(event.target.value)}
                          rows={4}
                          className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                        />
                      </label>
                    </>
                  ) : (
                    <>
                      <div>
                        <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Title</div>
                        <h3 className="mt-1 text-xl font-semibold text-stone-100">{item.title ?? 'Untitled item'}</h3>
                      </div>
                      <div>
                        <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Description</div>
                        <p className="mt-1 whitespace-pre-wrap text-sm text-stone-300">{item.description?.trim() || 'No description set.'}</p>
                      </div>
                      <div>
                        <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Notes</div>
                        <p className="mt-1 whitespace-pre-wrap text-sm text-stone-300">{item.notes?.trim() || 'No notes set.'}</p>
                      </div>
                    </>
                  )}
                </section>

                <section className="grid gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Sell / Listing Drafts</div>
                      <p className="mt-1 text-sm text-stone-400">Prepare editable listing copy without posting anywhere.</p>
                    </div>
                  </div>

                  {sellError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{sellError}</p> : null}
                  {draftActionError ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{draftActionError}</p> : null}

                  {sellLoading ? <p className="text-sm text-stone-400">Loading sell providers...</p> : null}

                  {!sellLoading && sellProviders.length === 0 ? (
                    <p className="rounded-md border border-rack-steel/24 bg-black/20 px-3 py-3 text-sm text-stone-400">
                      No enabled sell providers are available. Enable a provider in Admin / Sell to create listing drafts.
                    </p>
                  ) : null}

                  {!sellLoading && sellProviders.length > 0 ? (
                    <div className="grid gap-3 sm:grid-cols-2">
                      {sellProviders.map((provider) => {
                        const existingDraft = providerDraftMap.get(provider.provider_type);
                        const isOpening = openingProviderType === provider.provider_type;
                        return (
                          <div key={provider.id} className="grid gap-3 rounded-md border border-rack-steel/24 bg-black/20 p-3">
                            <div className="flex items-center justify-between gap-3">
                              <div className="flex items-center gap-3">
                                <ProviderBadge iconKey={provider.icon_key} label={provider.display_name} />
                                <div className="grid gap-1">
                                  <span className="text-sm font-semibold text-stone-100">{provider.display_name}</span>
                                  <span className="text-xs text-stone-500">{provider.provider_type}</span>
                                </div>
                              </div>
                              {existingDraft ? <CompactBadge>{existingDraft.status}</CompactBadge> : null}
                            </div>

                            {existingDraft ? (
                              <div className="grid gap-1 text-xs text-stone-400">
                                <span className="truncate">Draft: {existingDraft.title}</span>
                                <span>Price: {formatPriceDisplay(existingDraft.currency, existingDraft.asking_price)}</span>
                              </div>
                            ) : (
                              <p className="text-xs text-stone-400">No draft yet for this provider.</p>
                            )}

                            <div className="flex flex-wrap gap-2">
                              <button
                                type="button"
                                disabled={isOpening}
                                onClick={() => void handleOpenDraftForProvider(provider)}
                                className="rounded-md border border-amberline-400/40 bg-amberline-500/12 px-3 py-2 text-sm font-medium text-amberline-100 hover:bg-amberline-500/20 disabled:cursor-not-allowed disabled:opacity-50"
                              >
                                {isOpening ? 'Opening...' : existingDraft ? `Open ${provider.display_name} Draft` : `Create ${provider.display_name} Draft`}
                              </button>
                              {provider.base_url ? (
                                <a
                                  href={provider.base_url}
                                  target="_blank"
                                  rel="noreferrer"
                                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
                                >
                                  Open Provider Site
                                </a>
                              ) : null}
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  ) : null}
                </section>

                <section className="grid gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
                  <div className="flex items-start justify-between gap-3">
                    <div>
                      <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Images</div>
                      <p className="mt-1 text-sm text-stone-400">Originals are preserved. Thumbnails and normalized variants regenerate for the full item gallery after upload.</p>
                    </div>
                  </div>
                  {imageUploadEntries.length > 0 ? (
                    <div className="grid gap-2 rounded-md border border-rack-steel/24 bg-black/20 p-3">
                      {imageUploadEntries.map((entry) => (
                        <div key={entry.file_name} className="grid gap-1">
                          <div className="flex items-center justify-between gap-3 text-xs text-stone-300">
                            <span className="truncate">{entry.file_name}</span>
                            <span>{entry.status === 'complete' ? 'Complete' : `${entry.progress_percent}%`}</span>
                          </div>
                          <div className="h-2 overflow-hidden rounded-full bg-rack-steel/18">
                            <div
                              className={`h-full transition-all ${entry.status === 'failed' ? 'bg-red-400/70' : 'bg-amberline-400/70'}`}
                              style={{ width: `${entry.progress_percent}%` }}
                            />
                          </div>
                        </div>
                      ))}
                    </div>
                  ) : null}
                  {item.images.length === 0 ? (
                    <p className="text-sm text-stone-400">No linked images.</p>
                  ) : (
                    <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
                      {item.images.map((image) => (
                        <InventoryImageTile
                          key={image.image_asset_id}
                          image={image}
                          title={item.title}
                          onOpen={() => onOpenImage(image)}
                          onRemove={() => {
                            setImageDeleteTarget(image);
                            setImageDeleteError(null);
                          }}
                          isRemoveDisabled={isUploadingImages || isDeletingImage}
                        />
                      ))}
                    </div>
                  )}
                </section>
              </div>

              <section className="grid min-w-0 self-start gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
                <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Metadata</div>

                {isEditing ? (
                  <div className="grid min-w-0 gap-3">
                    <label className="grid gap-2 text-sm text-stone-200">
                      Approx Value
                      <input
                        value={approxValue}
                        onChange={(event) => setApproxValue(event.target.value)}
                        inputMode="decimal"
                        placeholder="0.00"
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
                      />
                    </label>
                    <label className="grid gap-2 text-sm text-stone-200">
                      Sale Price
                      <input
                        value={soldPrice}
                        onChange={(event) => setSoldPrice(event.target.value)}
                        inputMode="decimal"
                        placeholder="0.00"
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
                      />
                    </label>
                    <label className="grid gap-2 text-sm text-stone-200">
                      Sold Date
                      <input
                        type="date"
                        value={soldDate}
                        onChange={(event) => setSoldDate(event.target.value)}
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                      />
                    </label>
                    <label className="grid gap-2 text-sm text-stone-200">
                      Disposition
                      <select
                        value={dispositionCode}
                        onChange={(event) => setDispositionCode(event.target.value)}
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        {!dispositionCode ? <option value="">Select disposition</option> : null}
                        {dispositions.map((disposition) => (
                          <option key={disposition.code} value={disposition.code}>
                            {disposition.label}
                          </option>
                        ))}
                      </select>
                    </label>
                    <label className="grid gap-2 text-sm text-stone-200">
                      Inventory Group
                      <select
                        value={inventoryGroupId}
                        onChange={(event) => setInventoryGroupId(event.target.value)}
                        disabled={isSaving || inventoryGroupsLoading}
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <option value="">Select inventory group</option>
                        {!selectedInventoryGroupOption && item.inventory_group_id ? (
                          <option value={item.inventory_group_id}>
                            {item.inventory_group_name ?? item.inventory_group_code ?? 'Current inventory group'}
                          </option>
                        ) : null}
                        {inventoryGroups.map((group) => (
                          <option key={group.id} value={group.id}>
                            {group.name}
                          </option>
                        ))}
                        {inventoryGroups.length === 0 && inventoryGroupsLoading ? <option value={inventoryGroupId}>Loading inventory groups...</option> : null}
                      </select>
                    </label>
                    <label className="grid gap-2 text-sm text-stone-200">
                      Container
                      <select
                        value={containerId}
                        onChange={(event) => setContainerId(event.target.value)}
                        disabled={isSaving}
                        className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
                      >
                        <option value={looseContainerSelectValue}>No Container / Loose Item</option>
                        {!selectedContainerOption && item.container ? (
                          <option value={item.container.id}>
                            {formatContainerOptionLabel({
                              name: item.container.name,
                              type: item.container.type,
                              containerTypeName: item.container.container_type_name,
                              locationName: item.container.location_name,
                              locationDescription: item.container.location_description,
                              archived: false,
                            })}
                          </option>
                        ) : null}
                        {containers.map((container) => (
                          <option key={container.id} value={container.id}>
                            {formatContainerOptionLabel(container)}
                          </option>
                        ))}
                        {containers.length === 0 && containersLoading ? <option value={containerId}>Loading containers...</option> : null}
                      </select>
                    </label>
                    {isLooseItem ? (
                      <>
                        <label className="grid gap-2 text-sm text-stone-200">
                          Location
                          <select
                            value={locationId}
                            onChange={(event) => setLocationId(event.target.value)}
                            disabled={isSaving}
                            className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400 disabled:cursor-not-allowed disabled:opacity-50"
                          >
                            <option value="">No location</option>
                            {!selectedLocationOption && item.location_id && item.location_name ? <option value={item.location_id}>{item.location_name}</option> : null}
                            {locations.map((location) => (
                              <option key={location.id} value={location.id}>
                                {location.name}
                              </option>
                            ))}
                            {locations.length === 0 && locationsLoading ? <option value={locationId}>Loading locations...</option> : null}
                          </select>
                        </label>
                        <label className="grid gap-2 text-sm text-stone-200">
                          Location Detail
                          <input
                            value={locationDetail}
                            onChange={(event) => setLocationDetail(event.target.value)}
                            placeholder="Shelf 3, under desk, etc."
                            className="w-full min-w-0 max-w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
                          />
                        </label>
                      </>
                    ) : null}
                    {containersError ? <p className="text-sm text-red-100">{containersError}</p> : null}
                    {inventoryGroupsError ? <p className="text-sm text-red-100">{inventoryGroupsError}</p> : null}
                    {locationsError && isLooseItem ? <p className="text-sm text-red-100">{locationsError}</p> : null}
                  </div>
                ) : (
                  <div className="grid gap-3">
                    <MetaRow label="Inventory group" value={item.inventory_group_name ?? item.inventory_group_code ?? 'Not set'} />
                    <MetaRow label="Disposition" value={item.disposition_label ?? 'Not set'} />
                    <MetaRow label="Current inventory" value={item.current_inventory ? 'Yes' : 'Former item'} />
                    <MetaRow label="Archived" value={item.archived ? 'Yes' : 'No'} />
                    <MetaRow label="Approx value" value={formatCurrencyLike(item.approx_value)} />
                    <MetaRow label="Sale price" value={formatCurrencyLike(item.sold_price)} />
                    <MetaRow label="Sold date" value={item.sold_date ? new Date(`${item.sold_date}T00:00:00`).toLocaleDateString() : 'Not set'} />
                    <MetaRow label="Container" value={item.container?.name ?? 'No Container / Loose Item'} />
                    <MetaRow label="Location" value={inventoryItemLocationLabel(item)} />
                    <MetaRow label="Images" value={String(item.image_count)} />
                    <MetaRow label="AI enriched" value={item.ai_enriched ? 'Yes' : 'No'} />
                    <MetaRow label="Archived at" value={item.archived_datetime ? new Date(item.archived_datetime).toLocaleString() : 'Not archived'} />
                    <MetaRow label="Created" value={new Date(item.created_datetime).toLocaleString()} />
                    <MetaRow label="Updated" value={item.updated_datetime ? new Date(item.updated_datetime).toLocaleString() : 'Not updated'} />
                  </div>
                )}

                {!isEditing ? (
                  <div className="grid gap-2 rounded-md border border-rack-steel/24 bg-black/20 p-3">
                    <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Disposition history</div>
                    {isDispositionHistoryLoading ? <p className="text-sm text-stone-400">Loading history...</p> : null}
                    {dispositionHistoryError ? <p className="text-sm text-red-100">{dispositionHistoryError}</p> : null}
                    {!isDispositionHistoryLoading && dispositionHistory.length === 0 ? <p className="text-sm text-stone-400">No disposition changes recorded.</p> : null}
                    {dispositionHistory.map((entry) => (
                      <div key={entry.id} className="grid gap-1 rounded-md border border-rack-steel/18 bg-rack-soot/60 px-3 py-2 text-xs text-stone-300">
                        <span>
                          {entry.previous_disposition_label ?? entry.previous_disposition_code ?? 'None'} to {entry.new_disposition_label ?? entry.new_disposition_code}
                        </span>
                        <span className="text-stone-500">
                          {new Date(entry.changed_datetime).toLocaleString()} / {entry.new_current_inventory ? 'current inventory' : 'former inventory'}
                        </span>
                      </div>
                    ))}
                  </div>
                ) : null}
              </section>
            </div>
          ) : null}

          {item ? (
            <ItemDeleteConfirmationPanel
              item={item}
              preview={deletePreview}
              error={deletePreviewError}
              isLoading={isDeletePreviewLoading}
              isDeleting={isDeleting}
              onCancel={onClearDeleteState}
              onConfirm={() => void onConfirmDelete(item)}
            />
          ) : null}
        </Panel>

        {imageDeleteTarget ? (
          <ItemImageDeleteConfirmationModal
            image={imageDeleteTarget}
            error={imageDeleteError}
            isDeleting={isDeletingImage}
            onCancel={() => {
              setImageDeleteTarget(null);
              setImageDeleteError(null);
            }}
            onConfirm={() => void handleDeleteImage()}
          />
        ) : null}

        {item && activeDraft && isDraftModalOpen ? (
          <ListingDraftModal
            draft={activeDraft}
            provider={sellProviders.find((provider) => provider.provider_type === activeDraft.provider_type) ?? null}
            isSaving={isDraftSaving}
            isDeleting={isDraftDeleting}
            onClose={() => setIsDraftModalOpen(false)}
            onSave={handleSaveDraft}
            onDelete={handleDeleteDraft}
            onPreparePhotos={handlePrepareDraftPhotos}
            onDraftChange={(draft) => {
              setActiveDraft((current) => mergeListingDraft(current, draft));
              setListingDrafts((current) => replaceListingDraft(current, draft));
            }}
          />
        ) : null}
      </div>
    </div>
  );
}

function InventoryImageTile({
  image,
  title,
  onOpen,
  onRemove,
  isRemoveDisabled,
}: {
  image: InventoryImage;
  title: string | null;
  onOpen: () => void;
  onRemove: () => void;
  isRemoveDisabled: boolean;
}) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [image.image_asset_id]);

  return (
    <div className="grid gap-2 rounded-md border border-rack-steel/24 bg-black/20 p-2 text-left">
      <button
        type="button"
        onClick={onOpen}
        className="grid gap-2 text-left"
      >
        <div className="flex aspect-square items-center justify-center overflow-hidden rounded-md border border-rack-steel/20 bg-black/30">
          {!failed ? (
            <img
              src={imageUrl(image.image_asset_id, 'thumbnail')}
              alt={title ?? 'Inventory image'}
              loading="lazy"
              onError={() => setFailed(true)}
              className="h-full w-full object-cover"
            />
          ) : (
            <span className="px-2 text-center text-xs text-stone-400">Image unavailable</span>
          )}
        </div>
        <div className="grid gap-1 text-xs text-stone-300">
          <span className="truncate">{image.original_filename ?? image.stored_filename ?? 'Image'}</span>
          <span className="text-stone-500">{image.status}</span>
        </div>
      </button>
      <button
        type="button"
        onClick={onRemove}
        disabled={isRemoveDisabled}
        className="rounded border border-red-400/35 px-3 py-2 text-sm font-medium text-red-100 transition hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
      >
        Remove image
      </button>
    </div>
  );
}

function ListingDraftModal({
  draft,
  provider,
  isSaving,
  isDeleting,
  onClose,
  onSave,
  onDelete,
  onPreparePhotos,
  onDraftChange,
}: {
  draft: ListingDraft;
  provider: PublicSellProvider | null;
  isSaving: boolean;
  isDeleting: boolean;
  onClose: () => void;
  onSave: (draftId: string, input: {
    title: string;
    description: string | null;
    asking_price: string | null;
    currency: string;
    status: ListingDraftStatus;
    listing_url: string | null;
    notes: string | null;
  }) => Promise<ListingDraft>;
  onDelete: (draftId: string) => Promise<void>;
  onPreparePhotos: (draftId: string) => Promise<ListingDraft>;
  onDraftChange: (draft: ListingDraft) => void;
}) {
  const [title, setTitle] = useState(draft.title);
  const [description, setDescription] = useState(draft.description ?? '');
  const [askingPrice, setAskingPrice] = useState(draft.asking_price ?? '');
  const [currency, setCurrency] = useState(draft.currency);
  const [status, setStatus] = useState<ListingDraftStatus>(draft.status);
  const [listingUrl, setListingUrl] = useState(draft.listing_url ?? '');
  const [notes, setNotes] = useState(draft.notes ?? '');
  const [error, setError] = useState<string | null>(null);
  const [message, setMessage] = useState<string | null>(null);
  const [copyMessage, setCopyMessage] = useState<string | null>(null);
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false);
  const [isPreparingPhotos, setIsPreparingPhotos] = useState(false);
  const [photoExportError, setPhotoExportError] = useState<string | null>(null);

  useEffect(() => {
    setTitle(draft.title);
    setDescription(draft.description ?? '');
    setAskingPrice(draft.asking_price ?? '');
    setCurrency(draft.currency);
    setStatus(draft.status);
    setListingUrl(draft.listing_url ?? '');
    setNotes(draft.notes ?? '');
    setError(null);
    setMessage(null);
    setCopyMessage(null);
    setShowDeleteConfirm(false);
    setIsPreparingPhotos(false);
    setPhotoExportError(null);
  }, [draft]);

  const fullListingText = buildFullListingText({
    title,
    currency,
    askingPrice,
    description,
    notes,
  });

  const handleCopy = async (label: string, value: string) => {
    try {
      setCopyMessage(null);
      setError(null);
      await copyTextToClipboard(value);
      setCopyMessage(`${label} copied.`);
    } catch (err) {
      console.error(`Failed to copy ${label}`, err);
      setError(errorMessage(err, `Failed to copy ${label.toLowerCase()}.`));
    }
  };

  const handleSave = async () => {
    try {
      setError(null);
      setMessage(null);

      const normalizedTitle = title.trim();
      if (!normalizedTitle) {
        throw new Error('Title is required.');
      }

      const normalizedPrice = askingPrice.trim();
      if (normalizedPrice && !moneyPattern.test(normalizedPrice)) {
        throw new Error('Asking Price must be a non-negative decimal with up to two decimal places.');
      }

      const normalizedCurrency = currency.trim().toUpperCase();
      if (!normalizedCurrency) {
        throw new Error('Currency is required.');
      }

      const updated = await onSave(draft.id, {
        title: normalizedTitle,
        description: description.trim() || null,
        asking_price: normalizedPrice || null,
        currency: normalizedCurrency,
        status,
        listing_url: listingUrl.trim() || null,
        notes: notes.trim() || null,
      });
      onDraftChange(updated);
      setMessage('Draft saved.');
    } catch (err) {
      console.error('Failed to save listing draft', err);
      setError(errorMessage(err, 'Failed to save listing draft.'));
    }
  };

  const handleDelete = async () => {
    try {
      setError(null);
      await onDelete(draft.id);
    } catch (err) {
      console.error('Failed to delete listing draft', err);
      setError(errorMessage(err, 'Failed to delete listing draft.'));
    }
  };

  const handlePreparePhotos = async () => {
    try {
      setPhotoExportError(null);
      setMessage(null);
      setIsPreparingPhotos(true);
      const updated = await onPreparePhotos(draft.id);
      onDraftChange(updated);
      setMessage('Listing photos prepared.');
    } catch (err) {
      console.error('Failed to prepare listing photos', err);
      setPhotoExportError(errorMessage(err, 'Failed to prepare listing photos.'));
    } finally {
      setIsPreparingPhotos(false);
    }
  };

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/80 p-4" onClick={onClose}>
      <div className="max-h-[calc(100vh-2rem)] w-full max-w-[min(72rem,calc(100vw-2rem))] overflow-hidden" onClick={(event) => event.stopPropagation()}>
        <Panel
          title={draft.title || provider?.display_name || 'Listing draft'}
          eyebrow="Listing draft"
          action={
            <div className="flex min-w-0 flex-wrap gap-2">
              <button
                type="button"
                onClick={() => void handleCopy('Title', title.trim())}
                disabled={!title.trim()}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Copy Title
              </button>
              <button
                type="button"
                onClick={() => void handleCopy('Description', description.trim())}
                disabled={!description.trim()}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Copy Description
              </button>
              <button
                type="button"
                onClick={() => void handleCopy('Price', formatPriceDisplay(currency, askingPrice || null))}
                disabled={!askingPrice.trim()}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Copy Price
              </button>
              <button
                type="button"
                onClick={() => void handleCopy('Full listing', fullListingText)}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
              >
                Copy Full Listing
              </button>
              <button
                type="button"
                onClick={() => void handlePreparePhotos()}
                disabled={isPreparingPhotos}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
              >
                {isPreparingPhotos ? 'Preparing Photos...' : 'Refresh Listing Photos'}
              </button>
              <button
                type="button"
                onClick={onClose}
                className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
              >
                Close
              </button>
            </div>
          }
        >
          <div className="max-h-[calc(100vh-11rem)] overflow-y-auto pr-1">
          {error ? <p className="mb-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}
          {message ? <p className="mb-3 rounded-md border border-signal-green/35 bg-signal-green/10 px-3 py-2 text-sm text-green-100">{message}</p> : null}
          {copyMessage ? <p className="mb-3 rounded-md border border-rack-steel/24 bg-black/20 px-3 py-2 text-sm text-stone-200">{copyMessage}</p> : null}
          {photoExportError ? <p className="mb-3 rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{photoExportError}</p> : null}

          <div className="grid min-w-0 gap-5 xl:grid-cols-[minmax(0,1fr)_minmax(280px,360px)]">
            <section className="grid min-w-0 gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
              <label className="grid gap-2 text-sm text-stone-200">
                Provider
                <div className="flex min-w-0 flex-wrap items-center gap-3 rounded-md border border-rack-steel/24 bg-black/20 px-3 py-3">
                  <ProviderBadge iconKey={draft.provider_icon_key ?? provider?.icon_key ?? draft.provider_type} label={draft.provider_display_name ?? provider?.display_name ?? draft.provider_type} />
                  <div className="grid min-w-0 gap-1">
                    <span className="text-sm font-semibold text-stone-100">{draft.provider_display_name ?? provider?.display_name ?? draft.provider_type}</span>
                    <span className="text-xs text-stone-500">{draft.provider_type}</span>
                  </div>
                </div>
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Title
                <input
                  value={title}
                  onChange={(event) => setTitle(event.target.value)}
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                />
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Description
                <textarea
                  value={description}
                  onChange={(event) => setDescription(event.target.value)}
                  rows={8}
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                />
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Notes
                <textarea
                  value={notes}
                  onChange={(event) => setNotes(event.target.value)}
                  rows={4}
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                />
              </label>

              <div className="grid min-w-0 gap-3 rounded-md border border-rack-steel/24 bg-black/20 p-3">
                <div>
                  <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">Listing Photos</div>
                  <p className="mt-1 text-sm text-stone-400">This temporary folder is deleted automatically after 24 hours.</p>
                </div>
                {isPreparingPhotos ? <p className="text-sm text-stone-300">Preparing temporary listing photos...</p> : null}
                {draft.photo_export ? (
                  <>
                    <div className="grid gap-2 text-sm text-stone-200">
                      <div className="flex min-w-0 flex-col gap-1 rounded-md border border-rack-steel/18 bg-rack-soot/60 px-3 py-3">
                        <span className="text-xs uppercase tracking-[0.14em] text-rack-glass">Export folder path</span>
                        <span className="break-all font-mono text-xs text-stone-100">{draft.photo_export.export_path}</span>
                      </div>
                      <div className="grid min-w-0 gap-2 sm:grid-cols-2">
                        <InfoChip label="Expires" value={new Date(draft.photo_export.expires_at).toLocaleString()} />
                        <InfoChip label="Images" value={String(draft.photo_export.image_count)} />
                      </div>
                    </div>
                    <div className="flex min-w-0 flex-wrap gap-2">
                      <button
                        type="button"
                        onClick={() => void handleCopy('Folder path', draft.photo_export?.export_path ?? '')}
                        disabled={!draft.photo_export.export_path}
                        className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-40"
                      >
                        Copy Folder Path
                      </button>
                    </div>
                    {draft.photo_export.warnings.length > 0 ? (
                      <div className="grid gap-2 rounded-md border border-copper-500/25 bg-copper-500/10 p-3 text-sm text-amberline-100">
                        {draft.photo_export.warnings.map((warning) => (
                          <p key={warning}>{warning}</p>
                        ))}
                      </div>
                    ) : null}
                    {draft.photo_export.files.length > 0 ? (
                      <div className="grid min-w-0 gap-2 rounded-md border border-rack-steel/18 bg-rack-soot/60 p-3">
                        {draft.photo_export.files.map((file) => (
                          <div key={file.filename} className="flex min-w-0 items-center justify-between gap-3 text-xs text-stone-300">
                            <span className="truncate">{file.filename}</span>
                            <span className="shrink-0">{formatBytes(file.size_bytes)}</span>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <p className="text-sm text-stone-400">No listing photo files are available yet.</p>
                    )}
                  </>
                ) : (
                  <p className="text-sm text-stone-400">Listing photo export will appear here after preparation completes.</p>
                )}
              </div>
            </section>

            <section className="grid min-w-0 content-start gap-3 rounded-md border border-rack-steel/24 bg-rack-soot/55 p-4">
              <label className="grid gap-2 text-sm text-stone-200">
                Asking Price
                <input
                  value={askingPrice}
                  onChange={(event) => setAskingPrice(event.target.value)}
                  inputMode="decimal"
                  placeholder="0.00"
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
                />
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Currency
                <input
                  value={currency}
                  onChange={(event) => setCurrency(event.target.value)}
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                />
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Status
                <select
                  value={status}
                  onChange={(event) => setStatus(event.target.value as ListingDraftStatus)}
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none focus:border-amberline-400"
                >
                  {listingDraftStatuses.map((value) => (
                    <option key={value} value={value}>
                      {labelizeListingDraftStatus(value)}
                    </option>
                  ))}
                </select>
              </label>

              <label className="grid gap-2 text-sm text-stone-200">
                Listing URL
                <input
                  value={listingUrl}
                  onChange={(event) => setListingUrl(event.target.value)}
                  placeholder="https://..."
                  className="w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400"
                />
              </label>

              {provider?.base_url ? (
                <a
                  href={provider.base_url}
                  target="_blank"
                  rel="noreferrer"
                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-center text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
                >
                  Open Provider Site
                </a>
              ) : null}

              <div className="grid gap-2 rounded-md border border-rack-steel/24 bg-black/20 p-3 text-xs text-stone-400">
                <span>This draft is separate from the inventory item.</span>
                <span>Saving here does not modify the original item title, description, or value.</span>
              </div>

              <div className="flex min-w-0 flex-wrap gap-2 pt-2">
                <button
                  type="button"
                  onClick={handleSave}
                  disabled={isSaving || isDeleting}
                  className="rounded-md border border-amberline-400/45 bg-amberline-500/15 px-3 py-2 text-sm font-medium text-amberline-100 hover:bg-amberline-500/25 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {isSaving ? 'Saving...' : 'Save Draft'}
                </button>
                <button
                  type="button"
                  onClick={() => setShowDeleteConfirm(true)}
                  disabled={isSaving || isDeleting}
                  className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Delete Draft
                </button>
                <button
                  type="button"
                  onClick={onClose}
                  disabled={isSaving || isDeleting}
                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Cancel
                </button>
              </div>
            </section>
          </div>

          {showDeleteConfirm ? (
            <div className="mt-4 grid gap-3 rounded-md border border-red-400/30 bg-red-950/20 p-4">
              <div>
                <div className="text-xs uppercase tracking-[0.16em] text-red-200">Delete listing draft</div>
                <p className="mt-1 text-sm text-red-100">
                  This deletes only the listing draft row. It does not delete the inventory item, images, or provider configuration.
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <button
                  type="button"
                  onClick={() => void handleDelete()}
                  disabled={isDeleting || isSaving}
                  className="rounded-md border border-red-400/45 px-3 py-2 text-sm font-semibold text-red-100 hover:bg-red-500/10 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  {isDeleting ? 'Deleting...' : 'Delete Draft'}
                </button>
                <button
                  type="button"
                  onClick={() => setShowDeleteConfirm(false)}
                  disabled={isDeleting}
                  className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12 disabled:cursor-not-allowed disabled:opacity-50"
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function ProviderBadge({ iconKey, label }: { iconKey: string; label: string }) {
  return (
    <span className="inline-flex min-w-[4.5rem] items-center justify-center rounded-full border border-amberline-400/35 bg-amberline-500/12 px-3 py-1 text-xs font-semibold uppercase tracking-[0.16em] text-amberline-100">
      {providerBadgeText(iconKey, label)}
    </span>
  );
}

function FullSizeImageModal({
  image,
  itemTitle,
  containerName,
  failed,
  onClose,
  onImageError,
  onBackdropClick,
}: {
  image: InventoryImage;
  itemTitle: string | null;
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

  return (
    <div className="fixed inset-0 z-[60] flex items-center justify-center bg-black/80 px-4 py-6" onClick={onBackdropClick}>
      <div className="max-h-[92vh] w-full max-w-5xl overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel title={itemTitle ?? 'Inventory image'} eyebrow="Full-size preview" action={
          <button
            type="button"
            onClick={onClose}
            className="rounded-md border border-rack-steel/30 px-3 py-2 text-sm font-medium text-stone-200 hover:bg-rack-steel/12"
          >
            Close
          </button>
        }>
          <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_18rem]">
            <div className="flex min-h-[18rem] items-center justify-center rounded-md border border-rack-steel/24 bg-black/25 p-3">
              {failed ? (
                <div className="text-sm text-stone-300">Image unavailable</div>
              ) : (
                <div className="relative flex w-full items-center justify-center">
                  {!loaded ? <div className="absolute inset-0 flex items-center justify-center text-sm text-stone-400">Loading full-size preview...</div> : null}
                  <img
                    src={imageUrl(image.image_asset_id, 'normalized')}
                    alt={itemTitle ?? 'Inventory image'}
                    onLoad={() => setLoaded(true)}
                    onError={onImageError}
                    className={`max-h-[78vh] w-full object-contain ${loaded ? 'opacity-100' : 'opacity-0'}`}
                  />
                </div>
              )}
            </div>

            <div className="grid gap-3 text-sm text-stone-300">
              <MetaRow label="Container" value={containerName ?? 'No Container'} />
              <MetaRow label="Status" value={image.status} />
              <MetaRow label="Original" value={image.original_filename ?? 'n/a'} />
              <MetaRow label="Stored" value={image.stored_filename ?? 'n/a'} />
              <MetaRow label="MIME type" value={image.mime_type ?? 'n/a'} />
              <MetaRow label="File size" value={image.file_size_bytes != null ? formatFileSize(image.file_size_bytes) : 'n/a'} />
            </div>
          </div>
        </Panel>
      </div>
    </div>
  );
}

function MetaRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-rack-steel/24 bg-rack-soot/70 px-3 py-2">
      <div className="text-xs uppercase tracking-[0.16em] text-rack-glass">{label}</div>
      <div className="mt-1 text-sm text-stone-200">{value}</div>
    </div>
  );
}

function CompactBadge({ children }: { children: string }) {
  return <span className="rounded-full border border-rack-steel/24 bg-rack-soot/75 px-2.5 py-1 text-[11px] text-stone-300">{children}</span>;
}

function ItemImageDeleteConfirmationModal({
  image,
  error,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  image: InventoryImage;
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
          eyebrow="Inventory / Image delete"
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
                Permanently delete <span className="font-semibold text-red-100">{imageName}</span> from FastSell?
              </p>
              <p className="mt-3 text-sm leading-6 text-stone-200">
                This deletes the selected image asset row plus its original, thumbnail, and normalized files. The inventory item and remaining images stay unchanged.
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

function ItemDeleteConfirmationPanel({
  item,
  preview,
  error,
  isLoading,
  isDeleting,
  onCancel,
  onConfirm,
}: {
  item: InventoryItemDetail;
  preview: ItemDeletePreview | null;
  error: string | null;
  isLoading: boolean;
  isDeleting: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  if (!preview && !error && !isLoading) {
    return null;
  }

  return (
    <div
      className="fixed inset-0 z-[60] flex items-center justify-center bg-black/80 px-4 py-6"
      onClick={() => {
        if (!isDeleting) {
          onCancel();
        }
      }}
      role="dialog"
      aria-modal="true"
      aria-label={`Delete ${item.title ?? 'inventory item'}`}
    >
      <div className="max-h-[92vh] w-full max-w-xl overflow-y-auto" onClick={(event) => event.stopPropagation()}>
        <Panel
          title={`Delete ${item.title ?? 'Untitled item'}?`}
          eyebrow="Inventory / Delete confirmation"
          action={(
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                disabled={!preview || isLoading || isDeleting}
                onClick={onConfirm}
                className="rounded-md border border-red-400/60 bg-red-950/40 px-4 py-3 text-sm font-semibold text-red-100 hover:bg-red-500/15 disabled:cursor-not-allowed disabled:opacity-50"
              >
                {isDeleting ? 'Deleting...' : 'Delete item'}
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
          )}
        >
          <div className="grid gap-4">
            <div className="rounded-md border border-red-400/40 bg-red-950/25 p-4">
              <p className="text-sm leading-6 text-stone-200">
                This permanently deletes the inventory item, linked image metadata, and DB-referenced image files from disk.
              </p>
              <p className="mt-3 text-sm font-semibold text-red-100">This cannot be undone.</p>
            </div>

            {isLoading ? <p className="text-sm text-stone-300">Loading delete preview...</p> : null}
            {error ? <p className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-2 text-sm text-red-100">{error}</p> : null}

            {preview ? (
              <>
                <dl className="grid gap-2 rounded-md border border-rack-steel/30 bg-rack-soot/75 p-4 text-sm sm:grid-cols-2">
                  <DeleteCount label="Images" value={preview.image_count} />
                  <DeleteCount label="Files" value={preview.file_count} />
                  <DeleteCount label="File size" value={formatBytes(preview.total_file_size_bytes)} />
                  <DeleteCount label="Upload groups" value={String(preview.linked_upload_group_count)} />
                  <DeleteCount label="Upload sessions" value={String(preview.linked_upload_session_count)} />
                </dl>
                {preview.warnings.map((warning) => (
                  <p key={warning} className="rounded-md border border-rack-steel/25 bg-rack-soot/70 px-3 py-2 text-sm text-stone-300">
                    {warning}
                  </p>
                ))}
              </>
            ) : null}
          </div>
        </Panel>
      </div>
    </div>
  );
}

function DeleteCount({ label, value }: { label: string; value: string | number }) {
  return (
    <div className="flex items-center justify-between gap-4">
      <dt className="text-stone-400">{label}</dt>
      <dd className="font-semibold text-stone-100">{value}</dd>
    </div>
  );
}

function replaceListingDraft(current: ListingDraft[], draft: ListingDraft): ListingDraft[] {
  const existing = current.find((entry) => entry.id === draft.id) ?? null;
  const merged = mergeListingDraft(existing, draft);
  const remaining = current.filter((entry) => entry.id !== draft.id);
  return [merged, ...remaining].sort((left, right) => Date.parse(right.created_datetime) - Date.parse(left.created_datetime));
}

function mergeListingDraft(current: ListingDraft | null, next: ListingDraft): ListingDraft {
  if (!current || next.photo_export !== undefined) {
    return next;
  }

  return {
    ...next,
    photo_export: current.photo_export ?? null,
  };
}

function InfoChip({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-rack-steel/18 bg-rack-soot/60 px-3 py-3">
      <div className="text-[11px] uppercase tracking-[0.14em] text-rack-glass">{label}</div>
      <div className="mt-1 text-sm text-stone-100">{value}</div>
    </div>
  );
}

function providerBadgeText(iconKey: string, label: string): string {
  switch (iconKey.toLowerCase()) {
    case 'meta':
      return 'Meta';
    case 'ebay':
      return 'eBay';
    case 'craigslist':
      return 'CL';
    case 'etsy':
      return 'Etsy';
    default:
      return label.slice(0, 10);
  }
}

function labelizeListingDraftStatus(status: ListingDraftStatus): string {
  switch (status) {
    case 'draft':
      return 'Draft';
    case 'ready':
      return 'Ready';
    case 'listed':
      return 'Listed';
    case 'archived':
      return 'Archived';
    default:
      return status;
  }
}

function formatPriceDisplay(currency: string, askingPrice: string | null): string {
  const normalizedCurrency = currency.trim().toUpperCase() || 'USD';
  if (!askingPrice) {
    return `${normalizedCurrency} Not set`;
  }

  const numeric = Number(askingPrice);
  if (!Number.isFinite(numeric)) {
    return `${normalizedCurrency} ${askingPrice}`;
  }

  try {
    return new Intl.NumberFormat('en-US', { style: 'currency', currency: normalizedCurrency }).format(numeric);
  } catch {
    return `${normalizedCurrency} ${askingPrice}`;
  }
}

function buildFullListingText({
  title,
  currency,
  askingPrice,
  description,
  notes,
}: {
  title: string;
  currency: string;
  askingPrice: string;
  description: string;
  notes: string;
}): string {
  const sections = [
    `Title: ${title.trim() || 'Untitled listing'}`,
    `Price: ${formatPriceDisplay(currency, askingPrice.trim() || null)}`,
    `Description:\n${description.trim() || 'No description provided.'}`,
  ];

  if (notes.trim()) {
    sections.push(`Notes:\n${notes.trim()}`);
  }

  return sections.join('\n\n');
}

async function copyTextToClipboard(text: string): Promise<void> {
  if (!text || !text.trim()) {
    throw new Error('Nothing to copy.');
  }

  if (navigator.clipboard?.writeText && window.isSecureContext) {
    await navigator.clipboard.writeText(text);
    return;
  }

  const textarea = document.createElement('textarea');
  textarea.value = text;
  textarea.setAttribute('readonly', '');
  textarea.style.position = 'fixed';
  textarea.style.left = '-9999px';
  textarea.style.top = '0';
  textarea.style.opacity = '0';

  document.body.appendChild(textarea);

  try {
    textarea.focus();
    textarea.select();

    const successful = document.execCommand('copy');
    if (!successful) {
      throw new Error('Fallback copy command failed.');
    }
  } finally {
    document.body.removeChild(textarea);
  }
}

function detailToSummary(existing: InventoryItemSummary, item: InventoryItemDetail): InventoryItemSummary {
  const primaryImage = item.images[0] ?? null;

  return {
    ...existing,
    title: item.title,
    description: item.description,
    approx_value: item.approx_value,
    sold_price: item.sold_price,
    sold_date: item.sold_date,
    notes: item.notes,
    disposition_code: item.disposition_code,
    disposition_label: item.disposition_label,
    current_inventory: item.current_inventory,
    ai_enriched: item.ai_enriched,
    archived: item.archived,
    archived_datetime: item.archived_datetime,
    created_datetime: item.created_datetime,
    updated_datetime: item.updated_datetime,
    inventory_group_id: item.inventory_group_id,
    inventory_group_code: item.inventory_group_code,
    inventory_group_name: item.inventory_group_name,
    container: item.container,
    location_id: item.location_id,
    location_name: item.location_name,
    location_detail: item.location_detail,
    image_count: item.image_count,
    primary_image: primaryImage,
  };
}

function inventoryBucketKey(containerId: string | null | undefined): string | null {
  if (containerId === null) {
    return looseBucketId;
  }
  return containerId ?? null;
}

function inventoryItemLocationLabel(item: Pick<InventoryItemSummary, 'container' | 'location_name' | 'location_detail'> | Pick<InventoryItemDetail, 'container' | 'location_name' | 'location_detail'>): string {
  if (item.container) {
    const parts = [item.container.location_name, item.container.location_description, item.location_detail].filter((value): value is string => Boolean(value && value.trim()));
    if (parts.length === 0) {
      return 'No location';
    }
    return parts.join(' / ');
  }

  const parts = [item.location_name, item.location_detail].filter((value): value is string => Boolean(value && value.trim()));
  if (parts.length === 0) {
    return 'No location';
  }
  return parts.join(' / ');
}

function readBooleanSearchParam(searchParams: URLSearchParams, key: string): boolean {
  return searchParams.get(key)?.trim().toLowerCase() === 'true';
}

function readInventoryStateSearchParam(searchParams: URLSearchParams): InventoryState {
  const value = searchParams.get('inventory_state')?.trim().toLowerCase();
  if (value === 'former' || value === 'all') {
    return value;
  }
  return 'current';
}

function dispositionLabelForCode(dispositions: ItemDisposition[], code: string): string {
  return dispositions.find((disposition) => disposition.code === code)?.label ?? code;
}

function formatContainerOptionLabel(container: Pick<ContainerOption, 'name' | 'type' | 'containerTypeName' | 'locationName' | 'locationDescription' | 'archived'>): string {
  const parts = [container.name];
  if (container.containerTypeName) {
    parts.push(container.containerTypeName);
  } else if (container.type) {
    parts.push(container.type);
  }
  if (container.locationName) {
    parts.push(container.locationName);
  } else if (container.locationDescription) {
    parts.push(container.locationDescription);
  }
  if (container.archived) {
    parts.push('Archived');
  }
  return parts.join(' - ');
}

function truncateText(value: string, maxLength: number): string {
  if (value.length <= maxLength) {
    return value;
  }
  return `${value.slice(0, maxLength - 1)}...`;
}

function formatCurrencyLike(value: string | null): string {
  if (!value) {
    return 'Not set';
  }

  const numeric = Number(value);
  if (!Number.isFinite(numeric)) {
    return value;
  }

  try {
    return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(numeric);
  } catch {
    return `$${value}`;
  }
}

function formatCurrencyNumber(value: number): string {
  try {
    return new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD' }).format(value);
  } catch {
    return `$${value.toFixed(2)}`;
  }
}

function formatFileSize(bytes: number): string {
  return formatBytes(bytes);
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

function updatedArchiveLabel(nextArchived: boolean): string {
  return nextArchived ? 'Failed to archive item' : 'Failed to unarchive item';
}
