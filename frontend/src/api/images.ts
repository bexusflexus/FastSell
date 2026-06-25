import { API_BASE_URL } from './client';

export type ImageVariant = 'original' | 'thumbnail' | 'normalized';

export function imageUrl(imageAssetId: string, variant: ImageVariant = 'original'): string {
  const params = new URLSearchParams();
  if (variant !== 'original') {
    params.set('variant', variant);
  }
  const query = params.toString();
  return `${API_BASE_URL}/api/images/${encodeURIComponent(imageAssetId)}${query ? `?${query}` : ''}`;
}
