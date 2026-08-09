<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
          <div>
            <h1 class="text-2xl font-semibold text-gray-900 dark:text-white">{{ t('admin.modelPricing.title') }}</h1>
            <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.description') }}</p>
          </div>
          <div class="flex flex-wrap items-center gap-3">
            <div class="relative w-full sm:w-72">
              <Icon name="search" size="md" class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400" />
              <input
                v-model="searchQuery"
                type="search"
                class="input pl-10"
                :placeholder="t('admin.modelPricing.searchPlaceholder')"
                :aria-label="t('admin.modelPricing.searchPlaceholder')"
              />
            </div>
            <button
              type="button"
              class="btn btn-secondary inline-flex items-center gap-2"
              :disabled="loading"
              :title="t('common.refresh')"
              @click="loadModels"
            >
              <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
              <span class="hidden sm:inline">{{ t('common.refresh') }}</span>
            </button>
          </div>
        </div>
      </template>

      <template #table>
        <div v-if="loading" class="flex min-h-[320px] items-center justify-center">
          <div class="flex items-center gap-3 text-sm text-gray-500 dark:text-gray-400">
            <Icon name="refresh" size="md" class="animate-spin" />
            {{ t('common.loading') }}
          </div>
        </div>

        <div v-else-if="error" class="flex min-h-[320px] flex-col items-center justify-center px-6 text-center">
          <Icon :name="unauthorized ? 'lock' : 'exclamationCircle'" size="xl" class="mb-4 text-red-500" />
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ unauthorized ? t('admin.modelPricing.unauthorizedTitle') : t('admin.modelPricing.loadError') }}
          </h2>
          <p class="mt-2 max-w-md text-sm text-gray-500 dark:text-gray-400">{{ error }}</p>
          <button v-if="!unauthorized" type="button" class="btn btn-secondary mt-5" @click="loadModels">
            <Icon name="refresh" size="sm" class="mr-2" />
            {{ t('common.refresh') }}
          </button>
        </div>

        <div v-else-if="filteredModels.length === 0" class="flex min-h-[320px] flex-col items-center justify-center px-6 text-center">
          <Icon name="inbox" size="xl" class="mb-4 text-gray-400 dark:text-dark-500" />
          <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
            {{ models.length === 0 ? t('admin.modelPricing.emptyTitle') : t('admin.modelPricing.noMatchesTitle') }}
          </h2>
          <p class="mt-2 max-w-md text-sm text-gray-500 dark:text-gray-400">
            {{ models.length === 0 ? t('admin.modelPricing.emptyDescription') : t('admin.modelPricing.noMatchesDescription') }}
          </p>
        </div>

        <div v-else class="overflow-x-auto">
          <table class="w-full min-w-[980px] divide-y divide-gray-200 dark:divide-dark-700">
            <thead class="bg-gray-50 dark:bg-dark-800/80">
              <tr>
                <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('admin.modelPricing.columns.model') }}</th>
                <th class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('admin.modelPricing.columns.source') }}</th>
                <th v-for="field in summaryFields" :key="field.key" class="px-5 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">
                  {{ t(field.labelKey) }}
                </th>
                <th class="px-5 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500 dark:text-dark-400">{{ t('common.actions') }}</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200 bg-white dark:divide-dark-700 dark:bg-dark-900">
              <template v-for="item in filteredModels" :key="item.name">
                <tr class="hover:bg-gray-50 dark:hover:bg-dark-800/70">
                  <td class="px-5 py-4 align-top">
                    <div class="font-mono text-sm font-semibold text-gray-900 dark:text-white">{{ item.name }}</div>
                    <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ item.platform }}</div>
                  </td>
                  <td class="px-5 py-4 align-top">
                    <span class="inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium" :class="itemHasOverride(item) ? 'bg-amber-50 text-amber-700 dark:bg-amber-900/20 dark:text-amber-300' : 'bg-emerald-50 text-emerald-700 dark:bg-emerald-900/20 dark:text-emerald-300'">
                      {{ itemHasOverride(item) ? t('admin.modelPricing.source.override') : t('admin.modelPricing.source.default') }}
                    </span>
                    <div class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ itemHasOverride(item) ? t('admin.modelPricing.inheritance.customized') : t('admin.modelPricing.inheritance.inherited') }}</div>
                  </td>
                  <td v-for="field in summaryFields" :key="field.key" class="px-5 py-4 align-top text-sm text-gray-700 dark:text-gray-300">
                    <div class="font-mono">{{ formatPrice(item.effective?.[field.key]) }}</div>
                    <div class="mt-1 text-xs" :class="hasOverride(item, field.key) ? 'text-amber-600 dark:text-amber-400' : 'text-gray-400 dark:text-gray-500'">
                      {{ hasOverride(item, field.key) ? t('admin.modelPricing.fieldSource.override') : t('admin.modelPricing.fieldSource.inherited') }}
                    </div>
                  </td>
                  <td class="px-5 py-4 text-right align-top">
                    <button type="button" class="btn btn-secondary inline-flex items-center gap-2" @click="toggleEditor(item.name)">
                      <Icon :name="expandedModel === item.name ? 'chevronUp' : 'edit'" size="sm" />
                      <span>{{ expandedModel === item.name ? t('common.close') : t('common.edit') }}</span>
                    </button>
                  </td>
                </tr>
                <tr v-if="expandedModel === item.name" class="bg-gray-50/70 dark:bg-dark-800/50">
                  <td :colspan="summaryFields.length + 3" class="px-5 py-5">
                    <div class="mb-4 flex flex-col gap-3 border-b border-gray-200 pb-4 dark:border-dark-700 sm:flex-row sm:items-start sm:justify-between">
                      <div>
                        <h2 class="font-mono text-base font-semibold text-gray-900 dark:text-white">{{ item.name }}</h2>
                        <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.editorHint') }}</p>
                      </div>
                      <span class="text-xs text-gray-500 dark:text-gray-400">
                        {{ itemHasOverride(item) ? t('admin.modelPricing.inheritance.customized') : t('admin.modelPricing.inheritance.inherited') }}
                      </span>
                    </div>
                    <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
                      <div v-for="field in editableFields" :key="field.key" class="rounded-lg border border-gray-200 bg-white p-4 dark:border-dark-700 dark:bg-dark-900">
                        <div class="flex items-start justify-between gap-3">
                          <label class="text-sm font-medium text-gray-800 dark:text-gray-200">{{ t(field.labelKey) }}</label>
                          <span class="text-xs" :class="hasOverride(item, field.key) ? 'text-amber-600 dark:text-amber-400' : 'text-gray-400 dark:text-gray-500'">
                            {{ hasOverride(item, field.key) ? t('admin.modelPricing.fieldSource.override') : t('admin.modelPricing.fieldSource.inherited') }}
                          </span>
                        </div>
                        <div class="mt-3 grid grid-cols-3 gap-2 text-xs">
                          <div>
                            <div class="text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.values.builtIn') }}</div>
                            <div class="mt-1 font-mono text-gray-700 dark:text-gray-300">{{ formatPrice(item.built_in?.[field.key]) }}</div>
                          </div>
                          <div>
                            <label class="text-gray-500 dark:text-gray-400" :for="`${item.name}-${field.key}`">{{ t('admin.modelPricing.values.override') }}</label>
                            <input
                              :id="`${item.name}-${field.key}`"
                              v-model="drafts[item.name][field.key]"
                              type="number"
                              min="0"
                              step="any"
                              class="input mt-1 h-8 px-2 py-1 font-mono text-xs"
                              :placeholder="t('admin.modelPricing.inheritPlaceholder')"
                              :disabled="savingModel === item.name"
                            />
                          </div>
                          <div>
                            <div class="text-gray-500 dark:text-gray-400">{{ t('admin.modelPricing.values.effective') }}</div>
                            <div class="mt-1 font-mono font-medium text-gray-900 dark:text-white">{{ formatPrice(item.effective?.[field.key]) }}</div>
                          </div>
                        </div>
                      </div>
                    </div>
                    <div v-if="editorError[item.name]" class="mt-4 rounded-lg border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-900/20 dark:text-red-300">
                      {{ editorError[item.name] }}
                    </div>
                    <div class="mt-5 flex flex-wrap justify-end gap-2">
                      <button type="button" class="btn btn-secondary inline-flex items-center gap-2" :disabled="savingModel === item.name || !itemHasOverride(item)" @click="resetModel(item)">
                        <Icon name="refresh" size="sm" />
                        {{ t('admin.modelPricing.resetDefault') }}
                      </button>
                      <button type="button" class="btn btn-primary inline-flex items-center gap-2" :disabled="savingModel === item.name" @click="saveModel(item)">
                        <Icon :name="savingModel === item.name ? 'refresh' : 'check'" size="sm" :class="savingModel === item.name ? 'animate-spin' : ''" />
                        {{ savingModel === item.name ? t('common.saving') : t('common.save') }}
                      </button>
                    </div>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { ModelPricingFieldKey, ModelPricingItem } from '@/api/admin/modelPricing'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const models = ref<ModelPricingItem[]>([])
const loading = ref(false)
const error = ref('')
const unauthorized = ref(false)
const searchQuery = ref('')
const expandedModel = ref<string | null>(null)
const savingModel = ref<string | null>(null)
const drafts = reactive<Record<string, Record<ModelPricingFieldKey, string>>>({})
const editorError = reactive<Record<string, string>>({})

interface PricingField {
  key: ModelPricingFieldKey
  labelKey: string
}

const editableFields: PricingField[] = [
  { key: 'input_cost_per_token', labelKey: 'admin.modelPricing.fields.input' },
  { key: 'input_cost_per_token_priority', labelKey: 'admin.modelPricing.fields.inputPriority' },
  { key: 'output_cost_per_token', labelKey: 'admin.modelPricing.fields.output' },
  { key: 'output_cost_per_token_priority', labelKey: 'admin.modelPricing.fields.outputPriority' },
  { key: 'cache_creation_input_token_cost', labelKey: 'admin.modelPricing.fields.cacheCreation' },
  { key: 'cache_creation_input_token_cost_priority', labelKey: 'admin.modelPricing.fields.cacheCreationPriority' },
  { key: 'cache_creation_input_token_cost_above_1hr', labelKey: 'admin.modelPricing.fields.cacheCreationAboveHour' },
  { key: 'cache_read_input_token_cost', labelKey: 'admin.modelPricing.fields.cacheRead' },
  { key: 'cache_read_input_token_cost_priority', labelKey: 'admin.modelPricing.fields.cacheReadPriority' },
  { key: 'long_context_input_token_threshold', labelKey: 'admin.modelPricing.fields.longContextThreshold' },
  { key: 'long_context_input_cost_multiplier', labelKey: 'admin.modelPricing.fields.longContextInputMultiplier' },
  { key: 'long_context_output_cost_multiplier', labelKey: 'admin.modelPricing.fields.longContextOutputMultiplier' },
  { key: 'output_cost_per_image', labelKey: 'admin.modelPricing.fields.imageOutput' },
  { key: 'output_cost_per_image_token', labelKey: 'admin.modelPricing.fields.imageOutputToken' },
  { key: 'input_cost_per_image_token', labelKey: 'admin.modelPricing.fields.imageInputToken' },
]

const summaryFields = editableFields.slice(0, 2)

const filteredModels = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  if (!query) return models.value
  return models.value.filter((item) => `${item.name} ${item.platform}`.toLowerCase().includes(query))
})

function formatPrice(value: number | undefined): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—'
  return value.toLocaleString(undefined, { maximumFractionDigits: 12 })
}

function hasOverride(item: ModelPricingItem, key: ModelPricingFieldKey): boolean {
  return item.override[key] !== undefined && item.override[key] !== null
}

function itemHasOverride(item: ModelPricingItem): boolean {
  return Object.keys(item.override).some((key) => hasOverride(item, key as ModelPricingFieldKey))
}

function draftFor(item: ModelPricingItem): Record<ModelPricingFieldKey, string> {
  const draft = {} as Record<ModelPricingFieldKey, string>
  for (const field of editableFields) {
    const value = item.override[field.key]
    draft[field.key] = value === undefined || value === null ? '' : String(value)
  }
  return draft
}

function toggleEditor(model: string): void {
  expandedModel.value = expandedModel.value === model ? null : model
}

function syncDrafts(): void {
  for (const item of models.value) drafts[item.name] = draftFor(item)
}

async function loadModels(): Promise<void> {
  loading.value = true
  error.value = ''
  unauthorized.value = false
  try {
    const response = await adminAPI.modelPricing.list()
    models.value = response.models ?? []
    syncDrafts()
  } catch (err) {
    const status = typeof err === 'object' && err !== null && 'status' in err ? Number((err as { status?: unknown }).status) : undefined
    unauthorized.value = status === 401 || status === 403
    error.value = extractApiErrorMessage(err, t('admin.modelPricing.loadError'))
  } finally {
    loading.value = false
  }
}

function buildPayload(item: ModelPricingItem): Record<string, number> {
  const payload: Record<string, number> = {}
  for (const field of editableFields) {
    const raw = drafts[item.name]?.[field.key]?.trim() ?? ''
    if (!raw) continue
    const value = Number(raw)
    if (!Number.isFinite(value) || value < 0) throw new Error(t('admin.modelPricing.invalidValue'))
    payload[field.key] = value
  }
  return payload
}

async function saveModel(item: ModelPricingItem): Promise<void> {
  savingModel.value = item.name
  editorError[item.name] = ''
  try {
    const updated = await adminAPI.modelPricing.update(item.name, buildPayload(item))
    const index = models.value.findIndex((candidate) => candidate.name === item.name)
    if (index >= 0) models.value[index] = updated
    drafts[item.name] = draftFor(updated)
  } catch (err) {
    editorError[item.name] = extractApiErrorMessage(err, t('admin.modelPricing.saveError'))
  } finally {
    savingModel.value = null
  }
}

async function resetModel(item: ModelPricingItem): Promise<void> {
  if (!window.confirm(t('admin.modelPricing.resetConfirm', { model: item.name }))) return
  savingModel.value = item.name
  editorError[item.name] = ''
  try {
    await adminAPI.modelPricing.remove(item.name)
    await loadModels()
  } catch (err) {
    editorError[item.name] = extractApiErrorMessage(err, t('admin.modelPricing.resetError'))
  } finally {
    savingModel.value = null
  }
}

onMounted(loadModels)
</script>
