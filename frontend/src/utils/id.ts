let groupCounter = 1;
let fileCounter = 1;

export function createGroupId(): string {
  return `group_${groupCounter++}`;
}

export function createFileId(): string {
  return `file_${fileCounter++}`;
}
