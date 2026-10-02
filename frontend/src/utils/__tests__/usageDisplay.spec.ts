import { describe, expect, it } from 'vitest'
import {
  createDefaultUsageDisplayConfig,
  formatUsageDuration,
  formatUsageSpeed,
  formatUsageTokens,
  normalizeUsageDisplayConfig,
  outputTokensPerSecond,
  usageLatencySeverity,
  usageSpeedSeverity,
} from '@/utils/usageDisplay'

const defaults = createDefaultUsageDisplayConfig()

describe('usage display formatting', () => {
  it('uses exact end-to-end output speed and rejects invalid rows', () => {
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000 })).toBe(20)
    expect(outputTokensPerSecond({ output_tokens: 0, duration_ms: 30_000 })).toBeNull()
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 0 })).toBeNull()
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, image_count: 1 })).toBeNull()
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, media_type: 'audio' })).toBeNull()
  })

  it('only calculates after-first-token speed for valid streaming rows', () => {
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, first_token_ms: 10_000, stream: true }, 'after_first_token')).toBe(30)
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, first_token_ms: 30_000, stream: true }, 'after_first_token')).toBeNull()
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, first_token_ms: 10_000, stream: false }, 'after_first_token')).toBeNull()
    expect(outputTokensPerSecond({ output_tokens: 600, duration_ms: 30_000, first_token_ms: null, stream: true }, 'after_first_token')).toBeNull()
  })

  it('formats tokens, duration, and speed with safe boundaries', () => {
    expect(formatUsageTokens(1234, defaults)).toBe('1,234')
    expect(formatUsageTokens(null, defaults)).toBe('-')
    expect(formatUsageTokens(1234, { ...defaults, token_unit: 'compact' })).toBe('1.23K')
    expect(formatUsageDuration(850, defaults)).toBe('850.00ms')
    expect(formatUsageDuration(1250, defaults)).toBe('1.25s')
    expect(formatUsageDuration(null, defaults)).toBe('-')
    expect(formatUsageSpeed(12.345, defaults)).toBe('12.35')
    expect(formatUsageSpeed(null, defaults)).toBe('-')
  })

  it('normalizes malformed server data without enabling invalid enums', () => {
    const normalized = normalizeUsageDisplayConfig({
      token_unit: 'invalid',
      token_decimals: 99,
      fields: { speed: false },
      first_token_thresholds: { warn: 100, slow: 10, critical: -1 },
    })
    expect(normalized.token_unit).toBe('raw')
    expect(normalized.token_decimals).toBe(4)
    expect(normalized.fields.speed).toBe(false)
    expect(normalized.first_token_thresholds.warn).toBe(100)
    expect(normalized.first_token_thresholds.slow).toBe(100)
    expect(normalized.first_token_thresholds.critical).toBe(100)
  })

  it('classifies only valid values', () => {
    expect(usageLatencySeverity(null, defaults.first_token_thresholds)).toBeNull()
    expect(usageLatencySeverity(30_000, defaults.first_token_thresholds)).toBe('slow')
    expect(usageSpeedSeverity(null, 10)).toBeNull()
    expect(usageSpeedSeverity(9.99, 10)).toBe('warn')
    expect(usageSpeedSeverity(10, 10)).toBe('good')
  })
})
