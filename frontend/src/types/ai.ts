export type AIProviderType = 'gemini' | 'openai' | 'ollama';

export interface AIProviderConfig {
  id: string;
  name: string;
  provider_type: AIProviderType;
  enabled: boolean;
  active: boolean;
  base_url: string | null;
  api_key_configured: boolean;
  api_key_display: string;
  api_key_env_var: string | null;
  model_name: string;
  vision_enabled: boolean;
  timeout_seconds: number;
  max_output_tokens: number | null;
  temperature: number | null;
  last_test_datetime: string | null;
  last_test_status: string | null;
  last_error_message: string | null;
  created_datetime: string;
  updated_datetime: string | null;
}

export interface ListAIProvidersResponse {
  providers: AIProviderConfig[];
}

export interface GetAIProviderResponse {
  provider: AIProviderConfig;
}

export interface CreateAIProviderInput {
  name: string;
  provider_type: AIProviderType;
  enabled?: boolean;
  active?: boolean;
  base_url?: string | null;
  api_key_value?: string | null;
  api_key_env_var?: string | null;
  model_name: string;
  vision_enabled?: boolean;
  timeout_seconds?: number;
  max_output_tokens?: number | null;
  temperature?: number | null;
}

export interface UpdateAIProviderInput {
  name?: string | null;
  provider_type?: AIProviderType | null;
  enabled?: boolean | null;
  active?: boolean | null;
  base_url?: string | null;
  api_key_value?: string | null;
  api_key_env_var?: string | null;
  model_name?: string | null;
  vision_enabled?: boolean | null;
  timeout_seconds?: number | null;
  max_output_tokens?: number | null;
  temperature?: number | null;
  clear_api_key?: boolean | null;
}

export interface DeleteAIProviderResponse {
  provider_id: string;
  deleted: boolean;
}

export interface AIProviderTestResult {
  provider_id: string;
  provider_type: AIProviderType;
  model_name: string;
  status: 'success' | 'failed';
  message: string;
  tested_datetime: string;
}

export interface AISettings {
  ai_assist_enabled: boolean;
  active_provider_id: string | null;
  active_provider_name: string | null;
  active_provider_type: AIProviderType | null;
  active_model_name: string | null;
}

export interface GetAISettingsResponse {
  settings: AISettings;
}

export interface UpdateAISettingsInput {
  ai_assist_enabled?: boolean | null;
  active_provider_id?: string | null;
}
