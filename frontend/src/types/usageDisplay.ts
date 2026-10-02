export type UsageTokenUnit = 'raw' | 'compact' | 'k' | 'm'
export type UsageDurationUnit = 'auto' | 'ms' | 's'
export type UsageSpeedFormula = 'end_to_end' | 'after_first_token'

export interface UsageDisplayFields {
  input_tokens: boolean
  output_tokens: boolean
  cache_read_tokens: boolean
  cache_creation_tokens: boolean
  cache_ttl_breakdown: boolean
  first_token: boolean
  duration: boolean
  speed: boolean
}

export type UsageDisplayField = keyof UsageDisplayFields

export interface UsageLatencyThresholds {
  warn: number
  slow: number
  critical: number
}

export interface UsageDisplayConfig {
  fields: UsageDisplayFields
  token_unit: UsageTokenUnit
  token_decimals: number
  duration_unit: UsageDurationUnit
  duration_decimals: number
  speed_formula: UsageSpeedFormula
  speed_decimals: number
  color_enabled: boolean
  first_token_thresholds: UsageLatencyThresholds
  duration_thresholds: UsageLatencyThresholds
  speed_low_threshold: number
}

export interface UsageSpeedSample {
  output_tokens?: number | null
  duration_ms?: number | null
  first_token_ms?: number | null
  stream?: boolean | null
  request_type?: string | null
  image_count?: number | null
  image_output_tokens?: number | null
  billing_mode?: string | null
  media_type?: string | null
}

export const USAGE_DISPLAY_FIELD_KEYS: UsageDisplayField[] = [
  'input_tokens',
  'output_tokens',
  'cache_read_tokens',
  'cache_creation_tokens',
  'cache_ttl_breakdown',
  'first_token',
  'duration',
  'speed',
]
