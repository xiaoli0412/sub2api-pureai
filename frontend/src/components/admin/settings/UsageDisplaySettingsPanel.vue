<template>
  <section class="card" data-testid="usage-display-settings">
    <div class="border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <h2 class="text-lg font-semibold text-gray-900 dark:text-white">{{ t('admin.settings.usageDisplay.title') }}</h2>
      <p class="mt-1 text-sm text-gray-500 dark:text-gray-400">{{ t('admin.settings.usageDisplay.description') }}</p>
    </div>
    <div class="space-y-5 p-6">
      <div v-if="loading" class="text-sm text-gray-500">{{ t('common.loading') }}</div>
      <template v-else>
        <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
          <label v-for="field in fieldOptions" :key="field.key" class="flex items-center justify-between gap-3 rounded-lg border border-gray-200 px-3 py-2 dark:border-dark-600">
            <span class="text-sm text-gray-700 dark:text-gray-300">{{ t(field.label) }}</span>
            <Toggle v-model="form.fields[field.key]" />
          </label>
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <label class="input-label">{{ t('admin.settings.usageDisplay.tokenUnit') }}
            <Select v-model="form.token_unit" class="mt-1" :options="tokenUnitOptions" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.durationUnit') }}
            <Select v-model="form.duration_unit" class="mt-1" :options="durationUnitOptions" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.speedFormula') }}
            <Select v-model="form.speed_formula" class="mt-1" :options="speedFormulaOptions" />
          </label>
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <label class="input-label">{{ t('admin.settings.usageDisplay.tokenDecimals') }}
            <input v-model.number="form.token_decimals" type="number" min="0" max="4" class="input mt-1" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.durationDecimals') }}
            <input v-model.number="form.duration_decimals" type="number" min="0" max="4" class="input mt-1" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.speedDecimals') }}
            <input v-model.number="form.speed_decimals" type="number" min="0" max="4" class="input mt-1" />
          </label>
        </div>
        <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
          <label class="input-label">{{ t('admin.settings.usageDisplay.firstTokenWarn') }}
            <input v-model.number="form.first_token_thresholds.warn" type="number" min="0" class="input mt-1" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.durationWarn') }}
            <input v-model.number="form.duration_thresholds.warn" type="number" min="0" class="input mt-1" />
          </label>
          <label class="input-label">{{ t('admin.settings.usageDisplay.speedLowThreshold') }}
            <input v-model.number="form.speed_low_threshold" type="number" min="0" step="0.1" class="input mt-1" />
          </label>
        </div>
        <div class="flex items-center justify-end gap-3">
          <span v-if="saved" class="text-sm text-emerald-600 dark:text-emerald-400">{{ t('common.saved') }}</span>
          <button type="button" class="btn btn-primary" :disabled="saving" @click="save">
            {{ saving ? t('common.saving') : t('common.save') }}
          </button>
        </div>
      </template>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import Select, { type SelectOption } from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import { getAdminUsageDisplayConfig, updateAdminUsageDisplayConfig } from '@/api/usageDisplay'
import { createDefaultUsageDisplayConfig, normalizeUsageDisplayConfig } from '@/utils/usageDisplay'
import { extractApiErrorMessage } from '@/utils/apiError'
import { useAppStore } from '@/stores'
import type { UsageDisplayConfig, UsageDisplayField } from '@/types/usageDisplay'

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const saved = ref(false)
const form = reactive<UsageDisplayConfig>(createDefaultUsageDisplayConfig())
const fieldOptions: Array<{ key: UsageDisplayField; label: string }> = [
  { key: 'input_tokens', label: 'usage.inputTokens' },
  { key: 'output_tokens', label: 'usage.outputTokens' },
  { key: 'cache_read_tokens', label: 'usage.cacheReadTokens' },
  { key: 'cache_creation_tokens', label: 'usage.cacheCreationTokens' },
  { key: 'cache_ttl_breakdown', label: 'usage.cacheTtlBreakdown' },
  { key: 'first_token', label: 'usage.firstToken' },
  { key: 'duration', label: 'usage.duration' },
  { key: 'speed', label: 'usage.outputSpeed' },
]
const tokenUnitOptions = computed<SelectOption[]>(() => [
  { value: 'raw', label: t('admin.settings.usageDisplay.options.raw') },
  { value: 'compact', label: t('admin.settings.usageDisplay.options.compact') },
  { value: 'k', label: t('admin.settings.usageDisplay.options.thousands') },
  { value: 'm', label: t('admin.settings.usageDisplay.options.millions') },
])
const durationUnitOptions = computed<SelectOption[]>(() => [
  { value: 'auto', label: t('admin.settings.usageDisplay.options.auto') },
  { value: 'ms', label: t('admin.settings.usageDisplay.options.milliseconds') },
  { value: 's', label: t('admin.settings.usageDisplay.options.seconds') },
])
const speedFormulaOptions = computed<SelectOption[]>(() => [
  { value: 'end_to_end', label: t('admin.settings.usageDisplay.options.endToEnd') },
  { value: 'after_first_token', label: t('admin.settings.usageDisplay.options.afterFirstToken') },
])

const assign = (value: UsageDisplayConfig) => Object.assign(form, normalizeUsageDisplayConfig(value))

onMounted(async () => {
  try { assign(await getAdminUsageDisplayConfig()) } catch { /* defaults remain usable */ }
  finally { loading.value = false }
})

const save = async () => {
  saving.value = true
  saved.value = false
  try {
    assign(await updateAdminUsageDisplayConfig(normalizeUsageDisplayConfig(form)))
    saved.value = true
    window.setTimeout(() => { saved.value = false }, 1800)
  } catch (error: unknown) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    saving.value = false
  }
}
</script>
