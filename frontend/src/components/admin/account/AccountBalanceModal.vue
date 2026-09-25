<template>
  <BaseDialog :show="show" :title="t('admin.accounts.balance.title')" width="wide" @close="emit('close')">
    <div v-if="account" class="space-y-4">
      <div class="flex items-center justify-between border-b border-gray-200 pb-3 dark:border-dark-700">
        <div>
          <div class="font-semibold text-gray-900 dark:text-gray-100">{{ account.name }}</div>
          <div class="text-xs text-gray-500 dark:text-gray-400">{{ account.platform }} · {{ account.type }}</div>
        </div>
        <button type="button" class="btn btn-secondary" :disabled="loading" @click="probe">
          <Icon name="refresh" size="sm" :class="{ 'animate-spin': loading }" />
          {{ t('admin.accounts.balance.probe') }}
        </button>
      </div>
      <div v-if="loading" class="py-8 text-center text-sm text-gray-500">{{ t('common.loading') }}</div>
      <div v-else class="space-y-3">
        <div class="flex items-baseline justify-between">
          <span class="text-sm text-gray-500 dark:text-gray-400">{{ t('admin.accounts.balance.value') }}</span>
          <span class="text-xl font-semibold text-gray-900 dark:text-white">{{ valueLabel }}</span>
        </div>
        <div class="grid grid-cols-2 gap-3 text-sm">
          <div><span class="text-gray-500">{{ t('admin.accounts.balance.status') }}</span><div class="font-medium">{{ result?.status || '-' }}</div></div>
          <div><span class="text-gray-500">{{ t('admin.accounts.balance.updated') }}</span><div class="font-medium">{{ updatedLabel }}</div></div>
        </div>
        <div v-if="result?.stale" class="rounded-md bg-amber-50 px-3 py-2 text-xs text-amber-700 dark:bg-amber-900/20 dark:text-amber-300">{{ t('admin.accounts.balance.stale') }}</div>
        <div v-if="result?.error" class="rounded-md bg-red-50 px-3 py-2 text-xs text-red-700 dark:bg-red-900/20 dark:text-red-300">{{ result.error }}</div>
        <div v-if="result?.status === 'unsupported'" class="rounded-md bg-gray-50 px-3 py-2 text-xs text-gray-600 dark:bg-dark-700 dark:text-gray-300">{{ t('admin.accounts.balance.unsupportedHint') }}</div>
        <div v-if="entries.length > 1" class="border-t border-gray-200 pt-3 dark:border-dark-700">
          <div class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">{{ t('admin.accounts.balance.currencies') }}</div>
          <div v-for="entry in entries" :key="entry.currency" class="flex justify-between text-sm"><span>{{ entry.currency || '-' }}</span><span>{{ formatBalance(entry.balance) }}</span></div>
        </div>
      </div>
    </div>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminAPI } from '@/api/admin'
import type { Account } from '@/types'
import type { AccountBalanceResult, CNProviderBalanceEntry } from '@/api/admin/accounts'

const props = defineProps<{ show: boolean; account: Account | null }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
const loading = ref(false)
const result = ref<AccountBalanceResult | null>(null)

const load = async (probe = false) => {
  if (!props.account) return
  loading.value = true
  try {
    result.value = probe
      ? await adminAPI.accounts.probeBalance(props.account.id)
      : await adminAPI.accounts.getBalance(props.account.id)
  } finally {
    loading.value = false
  }
}
const probe = () => load(true)
watch(() => [props.show, props.account?.id], ([show]) => { if (show) load() }, { immediate: true })
const entries = computed<CNProviderBalanceEntry[]>(() => result.value?.balances?.length ? result.value.balances : result.value ? [{ currency: result.value.currency || '', balance: result.value.balance }] : [])
const formatBalance = (value: number) => Number.isFinite(value) ? value.toFixed(value >= 100 ? 0 : 2) : '-'
const valueLabel = computed(() => result.value?.status === 'unsupported' ? `${formatBalance(result.value.rate_multiplier ?? 1)}x (${t('admin.accounts.balance.unsupported')})` : entries.value.map(entry => `${entry.currency || '-'} ${formatBalance(entry.balance)}`).join(' · ') || '-')
const updatedLabel = computed(() => result.value?.fetched_at ? new Date(result.value.fetched_at * 1000).toLocaleString() : '-')
</script>
