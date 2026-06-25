import type { InventoryGroup } from '../types/inventoryGroups';
import type { LocationOption } from '../types/locations';
import type { ContainerOption } from '../types/upload';

interface IntakeContextPanelProps {
  containers: ContainerOption[];
  locations: LocationOption[];
  inventoryGroups: InventoryGroup[];
  isLoadingContainers: boolean;
  isLoadingLocations: boolean;
  isLoadingInventoryGroups: boolean;
  containerLoadError: string | null;
  locationsLoadError: string | null;
  inventoryGroupsLoadError: string | null;
  selectedContainerId: string | null;
  selectedInventoryGroupId: string | null;
  selectedLocationId: string | null;
  noContainer: boolean;
  locationDetail: string;
  sessionNotes: string;
  onSelectContainer: (containerId: string | null) => void;
  onSelectInventoryGroup: (inventoryGroupId: string | null) => void;
  onSetNoContainer: () => void;
  onSelectLocation: (locationId: string | null) => void;
  onLocationDetailChange: (value: string) => void;
  onSessionNotesChange: (value: string) => void;
}

export function IntakeContextPanel({
  containers,
  locations,
  inventoryGroups,
  isLoadingContainers,
  isLoadingLocations,
  isLoadingInventoryGroups,
  containerLoadError,
  locationsLoadError,
  inventoryGroupsLoadError,
  selectedContainerId,
  selectedInventoryGroupId,
  selectedLocationId,
  noContainer,
  locationDetail,
  sessionNotes,
  onSelectContainer,
  onSelectInventoryGroup,
  onSetNoContainer,
  onSelectLocation,
  onLocationDetailChange,
  onSessionNotesChange,
}: IntakeContextPanelProps) {
  return (
    <div className="grid gap-4 lg:grid-cols-[1.15fr_0.85fr]">
      <div className="grid gap-3">
        {containerLoadError ? (
          <div className="rounded-md border border-red-400/35 bg-red-950/30 px-3 py-3 text-sm text-red-100">
            {containerLoadError}
          </div>
        ) : null}

        <div className="grid gap-3 sm:grid-cols-2">
          <div>
            <label className="text-sm font-medium text-stone-200" htmlFor="container-select">
              Container
            </label>
            <select
              id="container-select"
              disabled={isLoadingContainers}
              value={noContainer ? 'no-container' : selectedContainerId ?? ''}
              onChange={(event) => {
                if (event.target.value === 'no-container') {
                  onSetNoContainer();
                  return;
                }
                onSelectContainer(event.target.value || null);
              }}
              className="mt-2 w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2.5 text-sm text-stone-100 outline-none transition focus:border-amberline-400 focus:shadow-glow disabled:cursor-not-allowed disabled:opacity-60"
            >
              <option value="">{isLoadingContainers ? 'Loading containers...' : 'Select a container'}</option>
              {containers.map((container) => (
                <option key={container.id} value={container.id}>
                  {container.name}
                  {container.containerTypeName ? ` - ${container.containerTypeName}` : container.type ? ` - ${container.type}` : ''}
                  {container.locationName ? ` - ${container.locationName}` : container.locationDescription ? ` - ${container.locationDescription}` : ''}
                </option>
              ))}
              <option value="no-container">No Container</option>
            </select>
          </div>

          <div>
            <label className="text-sm font-medium text-stone-200" htmlFor="inventory-group-select">
              Inventory Group
            </label>
            <select
              id="inventory-group-select"
              value={selectedInventoryGroupId ?? ''}
              disabled={isLoadingInventoryGroups}
              onChange={(event) => onSelectInventoryGroup(event.target.value || null)}
              className="mt-2 w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2.5 text-sm text-stone-100 outline-none transition focus:border-amberline-400 focus:shadow-glow disabled:cursor-not-allowed disabled:opacity-60"
            >
              <option value="">{isLoadingInventoryGroups ? 'Loading inventory groups...' : 'Select inventory group'}</option>
              {inventoryGroups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name}
                </option>
              ))}
            </select>
            {inventoryGroupsLoadError ? <p className="mt-2 text-sm text-red-200">{inventoryGroupsLoadError}</p> : null}
          </div>
        </div>

        {noContainer ? (
          <div>
            <label className="text-sm font-medium text-stone-200" htmlFor="location-select">
              Location
            </label>
            <select
              id="location-select"
              value={selectedLocationId ?? ''}
              onChange={(event) => onSelectLocation(event.target.value || null)}
              disabled={isLoadingLocations}
              className="mt-2 w-full rounded-md border border-copper-500/45 bg-rack-soot/90 px-3 py-2.5 text-sm text-stone-100 outline-none transition focus:border-amberline-400 focus:shadow-glow disabled:cursor-not-allowed disabled:opacity-60"
            >
              <option value="">{isLoadingLocations ? 'Loading locations...' : 'No location'}</option>
              {locations.map((location) => (
                <option key={location.id} value={location.id}>
                  {location.name}
                </option>
              ))}
            </select>
            {locationsLoadError ? <p className="mt-2 text-sm text-red-200">{locationsLoadError}</p> : null}
          </div>
        ) : null}

        {noContainer ? (
          <div>
            <label className="text-sm font-medium text-stone-200" htmlFor="location-detail">
              Location Detail
            </label>
            <input
              id="location-detail"
              value={locationDetail}
              onChange={(event) => onLocationDetailChange(event.target.value)}
              placeholder="Shelf 3, under desk, loose parts table, etc."
              className="mt-2 w-full rounded-md border border-copper-500/45 bg-rack-soot/90 px-3 py-2.5 text-sm text-stone-100 outline-none transition placeholder:text-stone-500 focus:border-amberline-400 focus:shadow-glow"
            />
          </div>
        ) : null}
      </div>

      <div>
        <label className="text-sm font-medium text-stone-200" htmlFor="session-notes">
          Session notes
        </label>
        <textarea
          id="session-notes"
          value={sessionNotes}
          onChange={(event) => onSessionNotesChange(event.target.value)}
          rows={5}
          placeholder="Optional notes for this intake batch"
          className="mt-2 w-full resize-y rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-3 text-stone-100 outline-none transition placeholder:text-stone-500 focus:border-amberline-400 focus:shadow-glow"
        />
      </div>
    </div>
  );
}
