<template>
  <section class="plaza-filters rounded-xl border border-gray-200 bg-white p-3 shadow-sm dark:border-dark-700 dark:bg-dark-800/70 sm:p-4">
    <div class="flex flex-col gap-3 xl:flex-row xl:items-center">
      <div class="relative min-w-0 flex-1 xl:max-w-md">
        <Icon
          name="search"
          size="sm"
          class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-dark-500"
        />
        <input
          :value="search"
          type="search"
          :placeholder="t('modelPlaza.filters.searchPlaceholder')"
          class="input w-full rounded-lg py-2.5 pl-9 pr-9"
          @input="$emit('update:search', ($event.target as HTMLInputElement).value)"
        />
        <button
          v-if="search"
          type="button"
          class="absolute right-2.5 top-1/2 -translate-y-1/2 rounded-md p-1 text-gray-400 transition hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-700 dark:hover:text-white"
          :aria-label="t('modelPlaza.filters.clearSearch')"
          @click="$emit('update:search', '')"
        >
          <Icon name="x" size="xs" />
        </button>
      </div>

      <div class="grid grid-cols-2 gap-2 sm:grid-cols-4 xl:w-auto xl:flex xl:items-center">
        <label class="relative min-w-0">
          <span class="sr-only">{{ t('modelPlaza.filters.groupLabel') }}</span>
          <select
            :value="groupId"
            class="input w-full appearance-none truncate rounded-lg py-2.5 pl-3 pr-8 text-sm xl:w-40"
            @change="$emit('update:groupId', Number(($event.target as HTMLSelectElement).value) || 'all')"
          >
            <option value="all">{{ t('modelPlaza.filters.groupLabel') }}: {{ t('modelPlaza.filters.all') }}</option>
            <option v-for="g in groups" :key="`select-group-${g.id}`" :value="g.id">{{ g.name }}</option>
          </select>
          <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
        </label>
        <label class="relative min-w-0">
          <span class="sr-only">{{ t('modelPlaza.filters.rateLabel') }}</span>
          <select
            :value="rate"
            class="input w-full appearance-none rounded-lg py-2.5 pl-3 pr-8 font-mono text-sm xl:w-28"
            @change="$emit('update:rate', ($event.target as HTMLSelectElement).value === 'all' ? 'all' : Number(($event.target as HTMLSelectElement).value))"
          >
            <option value="all">{{ t('modelPlaza.filters.rateLabel') }}: {{ t('modelPlaza.filters.all') }}</option>
            <option v-for="r in rates" :key="`select-rate-${r}`" :value="r">{{ r }}x</option>
          </select>
          <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
        </label>
        <label class="relative min-w-0">
          <span class="sr-only">{{ t('modelPlaza.filters.typeLabel') }}</span>
          <select
            :value="type"
            class="input w-full appearance-none truncate rounded-lg py-2.5 pl-3 pr-8 text-sm xl:w-36"
            @change="$emit('update:type', ($event.target as HTMLSelectElement).value as PlazaFilterType)"
          >
            <option value="all">{{ t('modelPlaza.filters.typeLabel') }}: {{ t('modelPlaza.filters.all') }}</option>
            <option value="standard">{{ t('modelPlaza.filters.standard') }}</option>
            <option value="subscription">{{ t('modelPlaza.filters.subscription') }}</option>
            <option value="exclusive">{{ t('modelPlaza.filters.exclusive') }}</option>
          </select>
          <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
        </label>
        <label class="relative min-w-0">
          <span class="sr-only">{{ t('modelPlaza.filters.sortLabel') }}</span>
          <select
            :value="sort"
            class="input w-full appearance-none truncate rounded-lg py-2.5 pl-3 pr-8 text-sm xl:w-40"
            @change="$emit('update:sort', ($event.target as HTMLSelectElement).value as PlazaSort)"
          >
            <option value="recommended">{{ t('modelPlaza.filters.sortRecommended') }}</option>
            <option value="name">{{ t('modelPlaza.filters.sortName') }}</option>
            <option value="rate">{{ t('modelPlaza.filters.sortRate') }}</option>
          </select>
          <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
        </label>
      </div>
    </div>

    <div class="mt-3 flex items-start gap-2 border-t border-gray-100 pt-3 dark:border-dark-700">
      <span class="flex shrink-0 items-center gap-1.5 pt-1.5 text-[11px] font-semibold uppercase tracking-wider text-gray-400 dark:text-dark-500">
        <Icon name="globe" size="xs" />
        <span class="hidden sm:inline">{{ t('modelPlaza.filters.platformLabel') }}</span>
      </span>
      <div class="flex min-w-0 flex-wrap gap-1.5">
        <button
          v-for="p in ['all', ...platforms]"
          :key="`platform-${p}`"
          type="button"
          class="inline-flex max-w-full items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium transition disabled:cursor-not-allowed disabled:opacity-40 disabled:grayscale"
          :class="p === 'all' ? chipClass(platform === 'all') : platform === p ? 'chip-tinted-active' : 'chip-tinted'"
          :style="p === 'all' ? undefined : { '--chip-accent': platformAccentColor(p) }"
          :disabled="p !== 'all' && !platformEnabled(p)"
          @click="$emit('update:platform', p)"
        >
          <PlatformIcon v-if="p !== 'all'" :platform="p as GroupPlatform" size="xs" />
          {{ p === 'all' ? t('modelPlaza.filters.all') : p }}
        </button>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { platformAccentColor } from '@/utils/platformColors'
import type { GroupPlatform } from '@/types'

type PlazaFilterType = 'all' | 'standard' | 'subscription' | 'exclusive'
type PlazaSort = 'recommended' | 'name' | 'rate'

const props = defineProps<{
  /** Data-backed platform choices; no visibility is inferred client-side. */
  platforms: string[]
  groups: Array<{
    id: number
    name: string
    platform: string
    rate: number
    subscriptionType: string
    exclusive: boolean
  }>
  rates: number[]
  platform: string
  groupId: number | 'all'
  rate: number | 'all'
  search: string
  type: PlazaFilterType
  sort: PlazaSort
}>()

defineEmits<{
  'update:platform': [value: string]
  'update:groupId': [value: number | 'all']
  'update:rate': [value: number | 'all']
  'update:search': [value: string]
  'update:type': [value: PlazaFilterType]
  'update:sort': [value: PlazaSort]
}>()

const { t } = useI18n()

function platformEnabled(p: string): boolean {
  return props.groups.some(
    (g) =>
      g.platform === p &&
      (props.groupId === 'all' || g.id === props.groupId) &&
      (props.rate === 'all' || g.rate === props.rate),
  )
}

function chipClass(active: boolean): string {
  return active
    ? 'bg-gray-900 text-white shadow-sm dark:bg-white dark:text-gray-900'
    : 'bg-white text-gray-600 ring-1 ring-inset ring-gray-200 hover:bg-gray-50 hover:text-gray-900 dark:bg-dark-800/60 dark:text-dark-300 dark:ring-dark-700 dark:hover:bg-dark-800 dark:hover:text-white'
}
</script>

<style scoped>
.chip-tinted {
  color: color-mix(in srgb, var(--chip-accent) 78%, black);
  background-color: color-mix(in srgb, var(--chip-accent) 9%, transparent);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--chip-accent) 25%, transparent);
}

.chip-tinted:not(:disabled):hover {
  background-color: color-mix(in srgb, var(--chip-accent) 16%, transparent);
}

.dark .chip-tinted {
  color: color-mix(in srgb, var(--chip-accent) 72%, white);
  background-color: color-mix(in srgb, var(--chip-accent) 12%, transparent);
  box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--chip-accent) 30%, transparent);
}

.chip-tinted-active {
  color: #fff;
  background-color: color-mix(in srgb, var(--chip-accent) 85%, black);
  box-shadow: 0 1px 2px 0 color-mix(in srgb, var(--chip-accent) 35%, transparent);
}

.dark .chip-tinted-active {
  background-color: color-mix(in srgb, var(--chip-accent) 80%, transparent);
}
</style>
