export type SellProviderType = 'facebook_marketplace' | 'ebay' | 'craigslist' | 'etsy';

export interface SellProviderConfig {
  id: string;
  provider_type: SellProviderType;
  display_name: string;
  enabled: boolean;
  sort_order: number;
  icon_key: string;
  base_url: string | null;
  seller_profile_url: string | null;
  notes: string | null;
  created_datetime: string;
  updated_datetime: string | null;
}

export interface PublicSellProvider {
  id: string;
  provider_type: SellProviderType;
  display_name: string;
  enabled: boolean;
  sort_order: number;
  icon_key: string;
  base_url: string | null;
  seller_profile_url: string | null;
}

export interface ListSellProvidersResponse {
  providers: SellProviderConfig[];
}

export interface ListPublicSellProvidersResponse {
  providers: PublicSellProvider[];
}

export interface GetSellProviderResponse {
  provider: SellProviderConfig;
}

export interface CreateSellProviderInput {
  provider_type: SellProviderType;
  display_name: string;
  enabled?: boolean;
  sort_order?: number;
  icon_key: string;
  base_url?: string | null;
  seller_profile_url?: string | null;
  notes?: string | null;
}

export interface UpdateSellProviderInput {
  display_name?: string | null;
  enabled?: boolean | null;
  sort_order?: number | null;
  icon_key?: string | null;
  base_url?: string | null;
  seller_profile_url?: string | null;
  notes?: string | null;
}

export interface DeleteSellProviderResponse {
  provider_id: string;
  deleted: boolean;
}
