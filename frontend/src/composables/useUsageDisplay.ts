import { computed, ref, type ComputedRef, type Ref } from 'vue'
import { getUsageDisplayConfig } from '@/api/usageDisplay'
import {
  createDefaultUsageDisplayConfig,
  normalizeUsageDisplayConfig,
} from '@/utils/usageDisplay'
import type { UsageDisplayConfig, UsageDisplayField } from '@/types/usageDisplay'

const siteConfig = ref<UsageDisplayConfig>(createDefaultUsageDisplayConfig())
let request: Promise<void> | null = null

export function useUsageDisplay(storageKey = 'usage-display-preferences'): {
  config: Ref<UsageDisplayConfig>
  siteConfig: Ref<UsageDisplayConfig>
  fields: ComputedRef<UsageDisplayConfig['fields']>
  hasPersonalPreferences: Ref<boolean>
  load: () => Promise<void>
  toggleField: (key: UsageDisplayField) => void
  resetPreferences: () => void
} {
  const personal = ref<Partial<UsageDisplayConfig['fields']> | null>(null)
  const config = computed(() => {
    const normalized = normalizeUsageDisplayConfig(siteConfig.value)
    return {
      ...normalized,
      fields: { ...normalized.fields, ...(personal.value ?? {}) },
    }
  }) as unknown as Ref<UsageDisplayConfig>
  const fields = computed(() => config.value.fields)
  const hasPersonalPreferences = computed(() => personal.value != null) as unknown as Ref<boolean>

  const readPersonal = () => {
    try {
      const raw = localStorage.getItem(storageKey)
      if (!raw) {
        personal.value = null
        return
      }
      const parsed = JSON.parse(raw) as Record<string, unknown>
      const fields = parsed.fields && typeof parsed.fields === 'object' ? parsed.fields as Record<string, unknown> : parsed
      const result: Partial<UsageDisplayConfig['fields']> = {}
      for (const key of Object.keys(siteConfig.value.fields) as UsageDisplayField[]) {
        if (typeof fields[key] === 'boolean') result[key] = fields[key] as boolean
      }
      personal.value = Object.keys(result).length > 0 ? result : null
    } catch {
      personal.value = null
    }
  }

  const load = async () => {
    if (!request) {
      request = getUsageDisplayConfig()
        .then((next) => { siteConfig.value = normalizeUsageDisplayConfig(next) })
        .catch(() => { siteConfig.value = createDefaultUsageDisplayConfig() })
        .finally(() => { request = null })
    }
    await request
    readPersonal()
  }

  const persist = () => {
    try {
      if (personal.value && Object.keys(personal.value).length > 0) {
        localStorage.setItem(storageKey, JSON.stringify({ fields: personal.value }))
      } else {
        localStorage.removeItem(storageKey)
      }
    } catch {
      // Display preferences are best-effort and must never block usage pages.
    }
  }

  const toggleField = (key: UsageDisplayField) => {
    const next = { ...(personal.value ?? {}) }
    next[key] = !config.value.fields[key]
    personal.value = next
    persist()
  }

  const resetPreferences = () => {
    personal.value = null
    persist()
  }

  return { config, siteConfig, fields, hasPersonalPreferences, load, toggleField, resetPreferences }
}
