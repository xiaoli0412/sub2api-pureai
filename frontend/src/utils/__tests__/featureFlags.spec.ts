import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'
import { useAppStore } from '@/stores/app'
import {
  FeatureFlags,
  getChannelMonitorMode,
  isChannelMonitorPassiveMode,
  isChannelMonitorV1Mode,
  isChannelMonitorV2Mode,
  isChannelMonitorV3Mode,
  isFeatureFlagEnabled,
  makeSidebarFlag,
  resolveFeatureFlag,
} from '@/utils/featureFlags'
import type { PublicSettings } from '@/types'

vi.mock('@/api/admin/system', () => ({
  checkUpdates: vi.fn(),
}))

vi.mock('@/api/auth', () => ({
  getPublicSettings: vi.fn(),
}))

describe('FeatureFlags.subscription', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    delete (window as any).__APP_CONFIG__
  })

  it('reads subscription_enabled as an opt-out flag: visible before settings load', () => {
    expect(FeatureFlags.subscription.key).toBe('subscription_enabled')
    expect(FeatureFlags.subscription.mode).toBe('opt-out')
    expect(useAppStore().cachedPublicSettings).toBeNull()
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(true)
  })

  it('hides only when the backend explicitly sends false', () => {
    const store = useAppStore()
    const sidebarFlag = makeSidebarFlag(FeatureFlags.subscription)

    store.cachedPublicSettings = { subscription_enabled: false } as PublicSettings
    expect(sidebarFlag()).toBe(false)

    store.cachedPublicSettings = { subscription_enabled: true } as PublicSettings
    expect(sidebarFlag()).toBe(true)

    store.cachedPublicSettings = {} as PublicSettings
    expect(sidebarFlag()).toBe(true)
  })
})

describe('resolveFeatureFlag', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  it('reads an explicit boolean from the given settings object', () => {
    expect(resolveFeatureFlag({ subscription_enabled: false } as PublicSettings, FeatureFlags.subscription)).toBe(false)
    expect(resolveFeatureFlag({ subscription_enabled: true } as PublicSettings, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({ available_channels_enabled: true } as PublicSettings, FeatureFlags.availableChannels)).toBe(true)
  })

  it('falls back to the declared mode when settings are missing or the key is absent', () => {
    expect(resolveFeatureFlag(undefined, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag(null, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({} as PublicSettings, FeatureFlags.subscription)).toBe(true)
    expect(resolveFeatureFlag({} as PublicSettings, FeatureFlags.availableChannels)).toBe(false)
  })

  it('backs isFeatureFlagEnabled with the same resolution', () => {
    useAppStore().cachedPublicSettings = { subscription_enabled: false } as PublicSettings
    expect(isFeatureFlagEnabled(FeatureFlags.subscription)).toBe(false)
  })
})

// V3 shares the passive aggregation pipeline with V2, so the two predicates must
// stay distinct: isChannelMonitorV2Mode() gates "the V2 console", while
// isChannelMonitorPassiveMode() gates "passive aggregation is running at all".
describe('channel monitor mode resolution', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
  })

  function setMode(mode?: string, enabled = true) {
    useAppStore().cachedPublicSettings = {
      channel_monitor_enabled: enabled,
      ...(mode === undefined ? {} : { channel_monitor_mode: mode }),
    } as PublicSettings
  }

  it('resolves v3 and treats it as a passive mode', () => {
    setMode('v3')
    expect(getChannelMonitorMode()).toBe('v3')
    expect(isChannelMonitorV3Mode()).toBe(true)
    expect(isChannelMonitorPassiveMode()).toBe(true)
    // V3 must not be reported as V2 or V1.
    expect(isChannelMonitorV2Mode()).toBe(false)
    expect(isChannelMonitorV1Mode()).toBe(false)
  })

  it('keeps v2 behaviour unchanged and also passive', () => {
    setMode('v2')
    expect(getChannelMonitorMode()).toBe('v2')
    expect(isChannelMonitorV2Mode()).toBe(true)
    expect(isChannelMonitorPassiveMode()).toBe(true)
    expect(isChannelMonitorV3Mode()).toBe(false)
  })

  it('treats v1 as an active-probe mode, never passive', () => {
    setMode('v1')
    expect(getChannelMonitorMode()).toBe('v1')
    expect(isChannelMonitorV1Mode()).toBe(true)
    expect(isChannelMonitorPassiveMode()).toBe(false)
    expect(isChannelMonitorV3Mode()).toBe(false)
  })

  it('falls back to v1 for a missing or unknown mode', () => {
    setMode(undefined)
    expect(getChannelMonitorMode()).toBe('v1')
    setMode('v9')
    expect(getChannelMonitorMode()).toBe('v1')
    expect(isChannelMonitorV3Mode()).toBe(false)
  })

  it('reports every mode as disabled when the feature flag is off', () => {
    setMode('v3', false)
    expect(isChannelMonitorV1Mode()).toBe(false)
    expect(isChannelMonitorV2Mode()).toBe(false)
    expect(isChannelMonitorV3Mode()).toBe(false)
    expect(isChannelMonitorPassiveMode()).toBe(false)
  })
})
