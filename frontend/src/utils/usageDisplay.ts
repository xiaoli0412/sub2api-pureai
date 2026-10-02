import type {
  UsageDisplayConfig,
  UsageDisplayField,
  UsageLatencyThresholds,
  UsageSpeedSample,
  UsageTokenUnit,
  UsageDurationUnit,
  UsageSpeedFormula,
} from '@/types/usageDisplay'

export type UsageDisplaySeverity = 'good' | 'warn' | 'slow' | 'critical'

export const DEFAULT_USAGE_DISPLAY_CONFIG: UsageDisplayConfig = {
  fields: {
    input_tokens: true,
    output_tokens: true,
    cache_read_tokens: true,
    cache_creation_tokens: true,
    cache_ttl_breakdown: true,
    first_token: true,
    duration: true,
    speed: true,
  },
  token_unit: 'raw',
  token_decimals: 2,
  duration_unit: 'auto',
  duration_decimals: 2,
  speed_formula: 'end_to_end',
  speed_decimals: 2,
  color_enabled: true,
  first_token_thresholds: { warn: 10_000, slow: 30_000, critical: 60_000 },
  duration_thresholds: { warn: 60_000, slow: 180_000, critical: 300_000 },
  speed_low_threshold: 10,
}

const finiteNumber = (value: unknown): number | null => {
  if (value == null || (typeof value === 'string' && value.trim() === '')) return null
  if (typeof value !== 'number' && typeof value !== 'string') return null
  const n = typeof value === 'number' ? value : Number(value)
  return Number.isFinite(n) ? n : null
}

const clampDecimals = (value: unknown, fallback: number): number => {
  const parsed = finiteNumber(value)
  const n = parsed == null ? fallback : Math.trunc(parsed)
  return Math.min(4, Math.max(0, n))
}

const normalizeThresholds = (
  input: unknown,
  fallback: UsageLatencyThresholds,
): UsageLatencyThresholds => {
  const source = input && typeof input === 'object' ? input as Record<string, unknown> : {}
  const hasOwn = (key: keyof UsageLatencyThresholds): boolean =>
    Object.prototype.hasOwnProperty.call(source, key)
  const normalizeValue = (
    key: keyof UsageLatencyThresholds,
    previous: number,
  ): number => {
    const parsed = finiteNumber(source[key])
    if (parsed != null && parsed >= 0) return parsed
    return hasOwn(key) ? previous : fallback[key]
  }
  const warn = Math.max(0, normalizeValue('warn', fallback.warn))
  const slow = Math.max(warn, normalizeValue('slow', warn))
  const critical = Math.max(slow, normalizeValue('critical', slow))
  return { warn, slow, critical }
}

const normalizeEnum = <T extends string>(value: unknown, allowed: readonly T[], fallback: T): T =>
  typeof value === 'string' && allowed.includes(value as T) ? value as T : fallback

export function createDefaultUsageDisplayConfig(): UsageDisplayConfig {
  return structuredClone(DEFAULT_USAGE_DISPLAY_CONFIG)
}

export function normalizeUsageDisplayConfig(input: unknown): UsageDisplayConfig {
  const source = input && typeof input === 'object' ? input as Record<string, unknown> : {}
  const rawFields = source.fields && typeof source.fields === 'object'
    ? source.fields as Record<string, unknown>
    : {}
  const fields = { ...DEFAULT_USAGE_DISPLAY_CONFIG.fields }
  for (const key of Object.keys(fields) as UsageDisplayField[]) {
    if (typeof rawFields[key] === 'boolean') fields[key] = rawFields[key] as boolean
  }
  const speedThreshold = finiteNumber(source.speed_low_threshold)
  return {
    fields,
    token_unit: normalizeEnum<UsageTokenUnit>(source.token_unit, ['raw', 'compact', 'k', 'm'], 'raw'),
    token_decimals: clampDecimals(source.token_decimals, 2),
    duration_unit: normalizeEnum<UsageDurationUnit>(source.duration_unit, ['auto', 'ms', 's'], 'auto'),
    duration_decimals: clampDecimals(source.duration_decimals, 2),
    speed_formula: normalizeEnum<UsageSpeedFormula>(source.speed_formula, ['end_to_end', 'after_first_token'], 'end_to_end'),
    speed_decimals: clampDecimals(source.speed_decimals, 2),
    color_enabled: typeof source.color_enabled === 'boolean' ? source.color_enabled : true,
    first_token_thresholds: normalizeThresholds(source.first_token_thresholds, DEFAULT_USAGE_DISPLAY_CONFIG.first_token_thresholds),
    duration_thresholds: normalizeThresholds(source.duration_thresholds, DEFAULT_USAGE_DISPLAY_CONFIG.duration_thresholds),
    speed_low_threshold: speedThreshold != null && speedThreshold >= 0 ? Math.min(1_000_000, speedThreshold) : 10,
  }
}

export function formatUsageTokens(
  value: number | null | undefined,
  config: UsageDisplayConfig,
  locale = 'en-US',
): string {
  const n = finiteNumber(value)
  if (n == null || n < 0) return '-'
  const decimals = config.token_decimals
  if (config.token_unit === 'raw') return n.toLocaleString(locale, { maximumFractionDigits: decimals })
  if (config.token_unit === 'k') return `${(n / 1_000).toFixed(decimals)}K`
  if (config.token_unit === 'm') return `${(n / 1_000_000).toFixed(decimals)}M`
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(decimals)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(decimals)}K`
  return n.toLocaleString(locale, { maximumFractionDigits: decimals })
}

export function formatUsageDuration(
  value: number | null | undefined,
  config: UsageDisplayConfig,
): string {
  const n = finiteNumber(value)
  if (n == null || n < 0) return '-'
  if (config.duration_unit === 'ms' || (config.duration_unit === 'auto' && n < 1_000)) {
    return `${n.toFixed(config.duration_decimals)}ms`
  }
  if (config.duration_unit === 's') {
    return `${(n / 1_000).toFixed(config.duration_decimals)}s`
  }
  if (config.duration_unit === 'auto') {
    if (n < 1_000) return `${n.toFixed(config.duration_decimals)}ms`
    if (n < 60_000) return `${(n / 1_000).toFixed(config.duration_decimals)}s`
    const totalSec = Math.round(n / 1_000)
    if (totalSec < 3_600) return `${Math.floor(totalSec / 60)}m ${totalSec % 60}s`
    return `${Math.floor(totalSec / 3_600)}h ${Math.floor((totalSec % 3_600) / 60)}m`
  }
  return '-'
}

const isImageOrNonText = (row: UsageSpeedSample): boolean => {
  if ((finiteNumber(row.image_count) ?? 0) > 0) return true
  if ((finiteNumber(row.image_output_tokens) ?? 0) > 0) return true
  if (row.billing_mode === 'image') return true
  const mediaType = row.media_type?.trim().toLowerCase()
  return Boolean(mediaType && mediaType !== 'text')
}

const isGenerationRequest = (row: UsageSpeedSample): boolean =>
  row.stream === true || row.request_type === 'stream' || row.request_type === 'ws_v2'

export function outputTokensPerSecond(
  row: UsageSpeedSample,
  formula: UsageSpeedFormula = 'end_to_end',
): number | null {
  if (isImageOrNonText(row)) return null
  const output = finiteNumber(row.output_tokens)
  const duration = finiteNumber(row.duration_ms)
  if (output == null || output <= 0 || duration == null || duration <= 0) return null
  if (formula === 'after_first_token') {
    if (!isGenerationRequest(row)) return null
    const firstToken = finiteNumber(row.first_token_ms)
    if (firstToken == null || firstToken < 0 || firstToken >= duration) return null
    const generationDuration = duration - firstToken
    if (generationDuration <= 0) return null
    return output * 1_000 / generationDuration
  }
  return output * 1_000 / duration
}

export function formatUsageSpeed(value: number | null | undefined, config: UsageDisplayConfig): string {
  const n = finiteNumber(value)
  if (n == null || n < 0) return '-'
  return n.toFixed(config.speed_decimals)
}

const classify = (value: number, thresholds: UsageLatencyThresholds): UsageDisplaySeverity => {
  if (value >= thresholds.critical) return 'critical'
  if (value >= thresholds.slow) return 'slow'
  if (value >= thresholds.warn) return 'warn'
  return 'good'
}

export function usageLatencySeverity(
  value: number | null | undefined,
  thresholds: UsageLatencyThresholds,
): UsageDisplaySeverity | null {
  const n = finiteNumber(value)
  if (n == null || n < 0) return null
  return classify(n, thresholds)
}

export function usageSpeedSeverity(
  speed: number | null | undefined,
  lowThreshold: number,
): 'good' | 'warn' | null {
  const n = finiteNumber(speed)
  if (n == null || n < 0) return null
  return n < lowThreshold ? 'warn' : 'good'
}
