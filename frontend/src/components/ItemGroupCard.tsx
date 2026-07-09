import type { ChangeEvent } from 'react';
import type { ImageDraft, ItemGroupDraft } from '../types/upload';
import { formatBytes } from '../utils/formatBytes';

interface ItemGroupCardProps {
  group: ItemGroupDraft;
  canRemove: boolean;
  onUpdate: (patch: Partial<Pick<ItemGroupDraft, 'title' | 'notes'>>) => void;
  onAddImages: (files: FileList) => void;
  onRemoveImage: (image: ImageDraft) => void;
  onRemoveGroup: () => void;
}

export function ItemGroupCard({
  group,
  canRemove,
  onUpdate,
  onAddImages,
  onRemoveImage,
  onRemoveGroup,
}: ItemGroupCardProps) {
  const handleFileChange = (event: ChangeEvent<HTMLInputElement>) => {
    if (event.target.files?.length) {
      onAddImages(event.target.files);
      event.target.value = '';
    }
  };

  return (
    <article className="rounded-lg border border-rack-steel/30 bg-[linear-gradient(180deg,rgba(22,26,24,0.92),rgba(8,9,8,0.95))] shadow-panel">
      <div className="flex flex-col gap-3 border-b border-rack-steel/25 bg-graphite-800/35 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="grid flex-1 gap-3 sm:grid-cols-[1fr_1.2fr]">
          <div>
            <label className="text-xs font-semibold uppercase tracking-[0.18em] text-rack-glass" htmlFor={`${group.clientGroupId}-title`}>
              Item label
            </label>
            <input
              id={`${group.clientGroupId}-title`}
              value={group.title}
              onChange={(event) => onUpdate({ title: event.target.value })}
              className="mt-2 w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none focus:border-amberline-400 focus:shadow-glow"
            />
          </div>
          <div>
            <label className="text-xs font-semibold uppercase tracking-[0.18em] text-rack-glass" htmlFor={`${group.clientGroupId}-notes`}>
              Item notes
            </label>
            <input
              id={`${group.clientGroupId}-notes`}
              value={group.notes}
              onChange={(event) => onUpdate({ notes: event.target.value })}
              placeholder="front/back/serial photos"
              className="mt-2 w-full rounded-md border border-rack-steel/35 bg-rack-soot/90 px-3 py-2 text-stone-100 outline-none placeholder:text-stone-500 focus:border-amberline-400 focus:shadow-glow"
            />
          </div>
        </div>
        <button
          type="button"
          onClick={onRemoveGroup}
          disabled={!canRemove}
          className="rounded-md border border-red-400/30 px-3 py-2 text-sm font-medium text-red-200 transition enabled:hover:bg-red-500/12 disabled:cursor-not-allowed disabled:opacity-40"
        >
          Remove item
        </button>
      </div>

      <div className="p-4">
        <label className="flex cursor-pointer flex-col items-center justify-center rounded-lg border border-dashed border-copper-400/32 bg-[radial-gradient(circle_at_center,rgba(240,82,42,0.08),rgba(8,9,8,0.88)_58%)] px-4 py-6 text-center transition hover:border-amberline-400/55 hover:bg-graphite-900">
          <span className="text-sm font-semibold text-amberline-200">Add images</span>
          <span className="mt-1 text-sm text-stone-400">Select from camera roll or filesystem</span>
          <input className="sr-only" type="file" accept="image/*" multiple onChange={handleFileChange} />
        </label>

        {group.images.length === 0 ? (
          <div className="mt-4 rounded-md border border-rack-steel/28 bg-rack-soot/70 px-4 py-5 text-sm text-stone-400">
            No images for this item yet.
          </div>
        ) : (
          <div className="mt-4 grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {group.images.map((image) => (
              <div key={image.clientFileId} className="overflow-hidden rounded-md border border-rack-steel/30 bg-rack-soot">
                <img src={image.objectUrl} alt={image.originalFilename} className="h-40 w-full object-cover" />
                <div className="space-y-2 p-3">
                  <p className="truncate text-sm font-medium text-stone-100" title={image.originalFilename}>
                    {image.originalFilename}
                  </p>
                  <div className="flex items-center justify-between gap-3 text-xs text-stone-400">
                    <span>{image.mimeType || 'image/*'}</span>
                    <span>{formatBytes(image.sizeBytes)}</span>
                  </div>
                  <button
                    type="button"
                    onClick={() => onRemoveImage(image)}
                    className="w-full rounded border border-copper-500/35 px-3 py-2 text-sm font-medium text-amberline-200 transition hover:bg-copper-500/12"
                  >
                    Remove image
                  </button>
                </div>
              </div>
            ))}
          </div>
        )}
      </div>
    </article>
  );
}
