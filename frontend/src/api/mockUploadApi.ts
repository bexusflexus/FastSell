import type { ContainerOption, SimulatedUploadResult, UploadSessionPayload } from '../types/upload';

const mockContainers: ContainerOption[] = [
  { id: 'container-1', name: 'Blue Tote 3', type: 'tote', containerTypeId: null, containerTypeName: 'Tote', locationId: null, locationName: 'Garage Shelf', locationDescription: 'Garage west rack', notes: 'Fragile items', archived: false, archivedDatetime: null },
  { id: 'container-2', name: 'Garage Shelf A', type: 'shelf', containerTypeId: null, containerTypeName: 'Shelf', locationId: null, locationName: 'Garage Shelf', locationDescription: 'North wall', notes: null, archived: false, archivedDatetime: null },
  { id: 'container-3', name: 'Office Parts Bin', type: 'bin', containerTypeId: null, containerTypeName: 'Bin', locationId: null, locationName: 'Upstairs Office Closet', locationDescription: 'Office closet', notes: 'Mostly cables', archived: false, archivedDatetime: null },
];

const wait = (milliseconds: number) =>
  new Promise((resolve) => {
    window.setTimeout(resolve, milliseconds);
  });

export async function listContainers(): Promise<ContainerOption[]> {
  await wait(180);
  return mockContainers;
}

export async function simulateUploadSession(payload: UploadSessionPayload): Promise<SimulatedUploadResult> {
  await wait(420);

  return {
    upload_session_id: `mock_session_${Date.now()}`,
    accepted_at: new Date().toISOString(),
    status: 'simulated',
    file_count: payload.groups.reduce((count, group) => count + group.files.length, 0),
    group_count: payload.groups.length,
  };
}
