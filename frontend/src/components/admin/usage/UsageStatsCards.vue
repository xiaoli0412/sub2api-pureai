<template>
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <div class="card p-4 flex items-center gap-3">
      <div class="rounded-lg bg-blue-100 p-2 dark:bg-blue-900/30 text-blue-600">
        <Icon name="document" size="md" />
      </div>
      <div>
        <p class="text-xs font-medium text-gray-500">{{ t('usage.totalRequests') }}</p>
        <p class="text-xl font-bold">{{ stats?.total_requests?.toLocaleString() || '0' }}</p>
        <p class="text-xs text-gray-400">{{ t('usage.inSelectedRange') }}</p>
      </div>
    </div>
    <div v-if="showTokenCard" class="card p-4 flex items-center gap-3">
      <div class="rounded-lg bg-amber-100 p-2 dark:bg-amber-900/30 text-amber-600"><svg class="h-5 w-5" aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="m21 7.5-9-5.25L3 7.5m18 0-9 5.25m9-5.25v9l-9 5.25M3 7.5l9 5.25M3 7.5v9l9 5.25m0-9v9" /></svg></div>
      <div>
        <p class="text-xs font-medium text-gray-500">{{ t('usage.totalTokens') }}</p>
        <p class="text-xl font-bold">{{ formatTokens(visibleTokenTotal) }}</p>
        <p class="flex flex-wrap items-center gap-x-1 text-xs text-gray-500">
          <template v-if="displayConfig.fields.input_tokens"><span>{{ t('usage.in') }}: {{ formatTokens(stats?.total_input_tokens || 0) }}</span></template>
          <span v-if="displayConfig.fields.input_tokens && displayConfig.fields.output_tokens">/</span>
          <template v-if="displayConfig.fields.output_tokens"><span>{{ t('usage.out') }}: {{ formatTokens(stats?.total_output_tokens || 0) }}</span></template>
          <span v-if="(displayConfig.fields.input_tokens || displayConfig.fields.output_tokens) && hasCacheSummary">/</span>
          <span v-if="hasCacheSummary" class="group relative inline-flex cursor-help items-center gap-0.5" tabindex="0" :title="cacheDetailLabel()">
            <span>{{ cacheLabel() }}: {{ formatTokens(visibleCacheTotal) }}</span>
            <svg class="h-3.5 w-3.5 text-gray-400" aria-hidden="true" fill="none" stroke="currentColor" viewBox="0 0 24 24"><path stroke-linecap="round" stroke-linejoin="round" stroke-width="2" d="M13 16h-1v-4h-1m1-4h.01M21 12a9 9 0 11-18 0 9 9 0 0118 0z" /></svg>
            <span class="pointer-events-none absolute left-1/2 top-full z-30 mt-2 hidden w-56 -translate-x-1/2 rounded-lg border border-gray-200 bg-white p-3 text-left text-xs text-gray-700 shadow-lg group-hover:block group-focus:block dark:border-dark-600 dark:bg-dark-800 dark:text-dark-200">
              <span class="mb-2 block font-medium text-gray-900 dark:text-white">{{ cacheDetailLabel() }}</span>
              <span v-if="displayConfig.fields.cache_creation_tokens" class="flex items-center justify-between gap-3"><span>{{ t('usage.cacheCreationTokensLabel') }}</span><span class="tabular-nums">{{ formatTokens(stats?.total_cache_creation_tokens || 0) }}</span></span>
              <span v-if="displayConfig.fields.cache_read_tokens" class="mt-1 flex items-center justify-between gap-3"><span>{{ t('usage.cacheReadTokensLabel') }}</span><span class="tabular-nums">{{ formatTokens(stats?.total_cache_read_tokens || 0) }}</span></span>
            </span>
          </span>
        </p>
      </div>
    </div>
    <div class="card p-4 flex items-center gap-3">
      <div class="rounded-lg bg-green-100 p-2 dark:bg-green-900/30 text-green-600">
        <Icon name="dollar" size="md" />
      </div>
      <div class="min-w-0 flex-1">
        <p class="text-xs font-medium text-gray-500">{{ t('usage.totalCost') }}</p>
        <p class="text-xl font-bold text-green-600">
          ${{ (stats?.total_actual_cost || 0).toFixed(4) }}
        </p>
        <p class="text-xs text-gray-400">
          <template v-if="showAccountCost && totalAccountCost != null">
            <span class="text-orange-500">{{ t('usage.accountCost') }} ${{ totalAccountCost.toFixed(4) }}</span>
            <span> · </span>
          </template>
          <span>
            {{ t('usage.standardCost') }}
            <span :class="{ 'line-through': strikeStandardCost }">${{ (stats?.total_cost || 0).toFixed(4) }}</span>
          </span>
        </p>
      </div>
    </div>
    <div v-if="displayConfig.fields.duration" class="card p-4 flex items-center gap-3">
      <div class="rounded-lg bg-purple-100 p-2 dark:bg-purple-900/30 text-purple-600">
        <Icon name="clock" size="md" />
      </div>
      <div><p class="text-xs font-medium text-gray-500">{{ t('usage.avgDuration') }}</p><p class="text-xl font-bold">{{ formatUsageDuration(stats?.average_duration_ms, displayConfig) }}</p></div>
    </div>
    <div v-if="displayConfig.fields.speed" class="card p-4 flex items-center gap-3">
      <div class="rounded-lg bg-cyan-100 p-2 dark:bg-cyan-900/30 text-cyan-600">
        <Icon name="arrowUp" size="md" />
      </div>
      <div><p class="text-xs font-medium text-gray-500">{{ speedLabel }}</p><p class="text-xl font-bold">{{ formatUsageSpeed(aggregateSpeed, displayConfig) }} <span v-if="aggregateSpeed != null" class="text-sm font-normal">{{ t('usage.speedUnit') }}</span></p></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { AdminUsageStatsResponse } from '@/api/admin/usage'
import type { UsageStatsResponse } from '@/types'
import type { UsageDisplayConfig } from '@/types/usageDisplay'
import { createDefaultUsageDisplayConfig, formatUsageDuration, formatUsageSpeed, formatUsageTokens } from '@/utils/usageDisplay'
import Icon from '@/components/icons/Icon.vue'

const props = withDefaults(defineProps<{
  stats: (AdminUsageStatsResponse | UsageStatsResponse) | null
  showAccountCost?: boolean
  strikeStandardCost?: boolean
  displayConfig?: UsageDisplayConfig
}>(), {
  showAccountCost: true,
  strikeStandardCost: false,
  displayConfig: () => createDefaultUsageDisplayConfig(),
})

const { t } = useI18n()

const totalAccountCost = computed(() => {
  const stats = props.stats as (AdminUsageStatsResponse & { total_account_cost?: number }) | null
  return stats?.total_account_cost ?? null
})
const showAccountCost = computed(() => props.showAccountCost)
const strikeStandardCost = computed(() => props.strikeStandardCost)
const displayConfig = computed(() => props.displayConfig)
const showTokenCard = computed(() => displayConfig.value.fields.input_tokens || displayConfig.value.fields.output_tokens || displayConfig.value.fields.cache_read_tokens || displayConfig.value.fields.cache_creation_tokens)
const hasCacheSummary = computed(() => displayConfig.value.fields.cache_read_tokens || displayConfig.value.fields.cache_creation_tokens)
const visibleTokenTotal = computed(() => {
  const fields = displayConfig.value.fields
  const stats = props.stats
  return (fields.input_tokens ? (stats?.total_input_tokens ?? 0) : 0)
    + (fields.output_tokens ? (stats?.total_output_tokens ?? 0) : 0)
    + (fields.cache_read_tokens ? (stats?.total_cache_read_tokens ?? 0) : 0)
    + (fields.cache_creation_tokens ? (stats?.total_cache_creation_tokens ?? 0) : 0)
})
const visibleCacheTotal = computed(() => {
  const fields = displayConfig.value.fields
  const stats = props.stats
  return (fields.cache_read_tokens ? (stats?.total_cache_read_tokens ?? 0) : 0)
    + (fields.cache_creation_tokens ? (stats?.total_cache_creation_tokens ?? 0) : 0)
})
const aggregateSpeed = computed(() => {
  const stats = props.stats as (AdminUsageStatsResponse & UsageStatsResponse) | null
  return displayConfig.value.speed_formula === 'after_first_token'
    ? (stats?.generation_tokens_per_second ?? null)
    : (stats?.output_tokens_per_second ?? null)
})
const speedLabel = computed(() => t(displayConfig.value.speed_formula === 'after_first_token' ? 'usage.avgGenerationSpeed' : 'usage.avgOutputSpeed'))
const cacheLabel = () => t('usage.cacheTotal')
const cacheDetailLabel = () => t('usage.cacheBreakdown')
const formatTokens = (value: number) => formatUsageTokens(value, displayConfig.value)
</script>
