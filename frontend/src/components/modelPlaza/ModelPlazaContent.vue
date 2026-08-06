<template>
  <div class="model-plaza space-y-5">
    <!-- Standalone pages need their own title; AppHeader supplies it in the console. -->
    <header class="plaza-hero" :class="{ 'plaza-hero-embedded': embedded }">
      <div class="min-w-0">
        <div class="mb-2 flex items-center gap-2 text-xs font-semibold uppercase tracking-[0.18em] text-primary-600 dark:text-primary-300">
          <span class="flex h-7 w-7 items-center justify-center rounded-lg bg-primary-50 text-primary-600 ring-1 ring-primary-100 dark:bg-primary-500/10 dark:text-primary-300 dark:ring-primary-400/20">
            <Icon name="grid" size="sm" />
          </span>
          {{ t('modelPlaza.kicker') }}
        </div>
        <h1 v-if="!embedded" class="text-3xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-4xl">
          {{ t('modelPlaza.title') }}
        </h1>
        <h2 v-else class="text-2xl font-bold tracking-tight text-gray-950 dark:text-white">
          {{ t('modelPlaza.title') }}
        </h2>
        <p class="mt-2 max-w-2xl text-sm leading-6 text-gray-600 dark:text-dark-300">
          {{ t('modelPlaza.description') }}
        </p>
      </div>
      <div class="plaza-stats" aria-label="Model plaza summary">
        <div>
          <strong>{{ groupCount }}</strong>
          <span>{{ t('modelPlaza.stats.groups') }}</span>
        </div>
        <div>
          <strong>{{ modelCount }}</strong>
          <span>{{ t('modelPlaza.stats.models') }}</span>
        </div>
        <div>
          <strong>{{ platformCount }}</strong>
          <span>{{ t('modelPlaza.stats.platforms') }}</span>
        </div>
      </div>
    </header>

    <div
      v-if="descriptionHtml"
      class="plaza-description rounded-xl border border-primary-100 bg-primary-50/70 px-5 py-4 text-sm dark:border-primary-400/20 dark:bg-primary-500/5"
      v-html="descriptionHtml"
    ></div>

    <p
      v-if="!isAuthenticated"
      class="flex items-center gap-2 rounded-lg border border-dashed border-gray-200 px-3 py-2 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400"
    >
      <Icon name="infoCircle" size="xs" class="h-3.5 w-3.5" />
      {{ t('modelPlaza.anonymousHint') }}
    </p>

    <div v-if="loading" class="flex min-h-[240px] items-center justify-center">
      <div class="h-8 w-8 animate-spin rounded-full border-2 border-primary-600/25 border-t-primary-600 dark:border-primary-400/25 dark:border-t-primary-400"></div>
    </div>
    <div
      v-else-if="error"
      class="rounded-xl border border-red-200 bg-red-50 px-5 py-8 text-center text-sm text-red-600 dark:border-red-500/30 dark:bg-red-500/10 dark:text-red-300"
    >
      {{ t('modelPlaza.loadFailed') }}
    </div>
    <template v-else>
      <!-- Faceted filters only operate on the groups returned by the API. -->
      <PlazaFilterBar
        :platforms="platforms"
        :groups="groupOptions"
        :rates="rates"
        :platform="selectedPlatform"
        :group-id="selectedGroupId"
        :rate="selectedRate"
        :search="searchQuery"
        :type="selectedType"
        :sort="sortBy"
        @update:platform="selectedPlatform = $event"
        @update:group-id="selectedGroupId = $event"
        @update:rate="selectedRate = $event"
        @update:search="searchQuery = $event"
        @update:type="selectedType = $event"
        @update:sort="sortBy = $event"
      />

      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-gray-200/80 pb-3 text-xs text-gray-500 dark:border-dark-700 dark:text-dark-400">
        <span class="inline-flex items-center gap-1.5">
          <span class="h-1.5 w-1.5 rounded-full bg-emerald-500"></span>
          {{ t('modelPlaza.results', { groups: filteredGroups.length, models: filteredModelCount }) }}
        </span>
        <span v-if="searchActive" class="rounded-md bg-gray-100 px-2 py-1 font-medium text-gray-600 dark:bg-dark-800 dark:text-dark-300">
          {{ t('modelPlaza.searchingFor', { query: searchQuery }) }}
        </span>
      </div>

      <!-- Group cards become a two-column directory on wide screens. -->
      <div v-if="filteredGroups.length > 0" class="grid gap-5 xl:grid-cols-2">
        <PlazaGroupSection v-for="g in filteredGroups" :key="g.id" :group="g" />
      </div>
      <div
        v-else
        class="rounded-xl border border-dashed border-gray-300 px-5 py-12 text-center text-sm text-gray-500 dark:border-dark-600 dark:text-dark-400"
      >
        {{ searchActive ? t('modelPlaza.noSearchResult') : t('modelPlaza.empty') }}
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import Icon from '@/components/icons/Icon.vue'
import PlazaFilterBar from './PlazaFilterBar.vue'
import PlazaGroupSection from './PlazaGroupSection.vue'
import type { ModelPlazaGroup, ModelPlazaResponse } from '@/api/modelPlaza'
import { useAuthStore } from '@/stores/auth'

const props = defineProps<{
  response: ModelPlazaResponse | null
  loading: boolean
  error?: boolean
  /** Console embedded form hides only the standalone page chrome. */
  embedded?: boolean
}>()

const { t } = useI18n()
const authStore = useAuthStore()
const isAuthenticated = computed(() => authStore.isAuthenticated)

const selectedPlatform = ref<string>('all')
const selectedGroupId = ref<number | 'all'>('all')
const selectedRate = ref<number | 'all'>('all')
const searchQuery = ref('')
const selectedType = ref<'all' | 'standard' | 'subscription' | 'exclusive'>('all')
const sortBy = ref<'recommended' | 'name' | 'rate'>('recommended')

const searchActive = computed(() => searchQuery.value.trim() !== '')
const sourceGroups = computed(() => props.response?.groups ?? [])
const groupCount = computed(() => sourceGroups.value.length)
const modelCount = computed(() => sourceGroups.value.reduce((total, group) => total + group.models.length, 0))
const platformCount = computed(() => new Set(sourceGroups.value.map((group) => group.platform).filter(Boolean)).size)

const descriptionHtml = computed(() => {
  const md = props.response?.description?.trim()
  if (!md) return ''
  return DOMPurify.sanitize(marked.parse(md) as string)
})

/** Effective rate is the user-specific rate when the backend supplied one. */
function effectiveRate(g: ModelPlazaGroup): number {
  return g.user_rate_multiplier ?? g.rate_multiplier
}

const platforms = computed(() =>
  [...new Set(sourceGroups.value.map((g) => g.platform).filter(Boolean))].sort(),
)

const groupOptions = computed(() =>
  sourceGroups.value.map((g) => ({
    id: g.id,
    name: g.name,
    platform: g.platform,
    rate: effectiveRate(g),
    subscriptionType: g.subscription_type,
    exclusive: g.is_exclusive,
  })),
)

const rates = computed(() =>
  [...new Set(sourceGroups.value.map(effectiveRate))].sort((a, b) => a - b),
)

watch(rates, (list) => {
  if (selectedRate.value !== 'all' && !list.includes(selectedRate.value)) {
    selectedRate.value = 'all'
  }
})

const filteredGroups = computed(() => {
  let groups = sourceGroups.value
  if (selectedPlatform.value !== 'all') {
    groups = groups.filter((g) => g.platform === selectedPlatform.value)
  }
  if (selectedGroupId.value !== 'all') {
    groups = groups.filter((g) => g.id === selectedGroupId.value)
  }
  if (selectedRate.value !== 'all') {
    groups = groups.filter((g) => effectiveRate(g) === selectedRate.value)
  }
  if (selectedType.value !== 'all') {
    groups = groups.filter((g) =>
      selectedType.value === 'exclusive' ? g.is_exclusive : g.subscription_type === selectedType.value,
    )
  }

  // Search keeps an authorized group intact when its name/platform matches;
  // otherwise it narrows only that group's already-authorized model list.
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    groups = groups
      .map((g) => {
        const groupHit = g.name.toLowerCase().includes(q) || g.platform.toLowerCase().includes(q)
        return groupHit ? g : { ...g, models: g.models.filter((m) => m.name.toLowerCase().includes(q)) }
      })
      .filter((g) => g.models.length > 0)
  }

  return [...groups].sort((a, b) => {
    if (sortBy.value === 'name') return a.name.localeCompare(b.name)
    if (sortBy.value === 'rate') return effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name)
    const exclusiveOrder = Number(b.is_exclusive) - Number(a.is_exclusive)
    return exclusiveOrder || effectiveRate(a) - effectiveRate(b) || a.name.localeCompare(b.name)
  })
})

const filteredModelCount = computed(() =>
  filteredGroups.value.reduce((total, group) => total + group.models.length, 0),
)
</script>

<style scoped>
.plaza-description {
  line-height: 1.7;
  overflow-wrap: anywhere;
}

.plaza-description :deep(h1),
.plaza-description :deep(h2),
.plaza-description :deep(h3) {
  @apply mb-2 mt-3 font-semibold text-gray-900 first:mt-0 dark:text-white;
}

.plaza-description :deep(p) {
  @apply mb-2 text-gray-700 last:mb-0 dark:text-dark-200;
}

.plaza-description :deep(a) {
  @apply text-primary-600 underline underline-offset-4 hover:text-primary-700 dark:text-primary-300;
}

.plaza-description :deep(ul) {
  @apply mb-2 list-disc pl-5;
}

.plaza-description :deep(ol) {
  @apply mb-2 list-decimal pl-5;
}

.plaza-description :deep(li) {
  @apply mb-0.5 text-gray-700 dark:text-dark-200;
}

.plaza-description :deep(code) {
  @apply rounded bg-gray-100 px-1.5 py-0.5 font-mono text-xs dark:bg-dark-800;
}

.plaza-description :deep(blockquote) {
  @apply my-2 border-l-4 border-gray-300 pl-3 text-gray-600 dark:border-dark-600 dark:text-dark-300;
}

.plaza-hero {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 2rem;
  border-bottom: 1px solid rgb(229 231 235 / 0.9);
  padding-bottom: 1.25rem;
}

.plaza-hero-embedded {
  padding-top: 0.25rem;
}

.plaza-stats {
  display: flex;
  flex-shrink: 0;
  align-items: stretch;
  overflow: hidden;
  border: 1px solid rgb(229 231 235 / 0.9);
  border-radius: 0.75rem;
  background: rgb(255 255 255 / 0.75);
}

.plaza-stats div {
  display: flex;
  min-width: 5.5rem;
  flex-direction: column;
  gap: 0.15rem;
  border-left: 1px solid rgb(229 231 235 / 0.9);
  padding: 0.75rem 0.9rem;
}

.plaza-stats div:first-child {
  border-left: 0;
}

.plaza-stats strong {
  color: rgb(17 24 39);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 1.05rem;
  line-height: 1.2;
}

.plaza-stats span {
  color: rgb(107 114 128);
  font-size: 0.68rem;
  white-space: nowrap;
}

.dark .plaza-hero {
  border-color: rgb(55 65 81 / 0.8);
}

.dark .plaza-stats {
  border-color: rgb(55 65 81 / 0.8);
  background: rgb(31 41 55 / 0.5);
}

.dark .plaza-stats div {
  border-color: rgb(55 65 81 / 0.8);
}

.dark .plaza-stats strong {
  color: rgb(249 250 251);
}

.dark .plaza-stats span {
  color: rgb(156 163 175);
}

@media (max-width: 640px) {
  .plaza-hero {
    align-items: flex-start;
    flex-direction: column;
    gap: 1rem;
  }

  .plaza-stats {
    width: 100%;
  }

  .plaza-stats div {
    flex: 1;
    min-width: 0;
  }
}
</style>
