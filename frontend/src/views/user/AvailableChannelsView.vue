<template>
  <AppLayout>
    <TablePageLayout>
      <template #filters>
        <div class="channel-toolbar">
          <div class="channel-toolbar-heading">
            <div>
              <p class="channel-eyebrow">{{ t('availableChannels.kicker') }}</p>
              <h1 class="text-2xl font-bold tracking-tight text-gray-950 dark:text-white sm:text-3xl">
                {{ t('availableChannels.title') }}
              </h1>
              <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">
                {{ t('availableChannels.description') }}
              </p>
            </div>
            <div class="channel-summary">
              <strong>{{ filteredChannels.length }}</strong>
              <span>{{ t('availableChannels.resultCount') }}</span>
            </div>
          </div>

          <div class="channel-filter-grid">
            <div class="relative min-w-0 sm:col-span-2 xl:col-span-1">
              <Icon
                name="search"
                size="md"
                class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-gray-400 dark:text-gray-500"
              />
              <input
                v-model="searchQuery"
                type="search"
                :placeholder="t('availableChannels.searchPlaceholder')"
                class="input w-full pl-10"
              />
            </div>

            <label class="relative min-w-0">
              <span class="sr-only">{{ t('availableChannels.filters.platform') }}</span>
              <select v-model="selectedPlatform" class="input w-full appearance-none truncate pr-8">
                <option value="all">{{ t('availableChannels.filters.platform') }}: {{ t('availableChannels.filters.all') }}</option>
                <option v-for="platform in platformOptions" :key="platform" :value="platform">{{ platform }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
            </label>

            <label class="relative min-w-0">
              <span class="sr-only">{{ t('availableChannels.filters.group') }}</span>
              <select v-model="selectedGroupId" class="input w-full appearance-none truncate pr-8">
                <option value="all">{{ t('availableChannels.filters.group') }}: {{ t('availableChannels.filters.all') }}</option>
                <option v-for="group in groupOptions" :key="group.id" :value="String(group.id)">{{ group.name }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
            </label>

            <label class="relative min-w-0">
              <span class="sr-only">{{ t('availableChannels.filters.model') }}</span>
              <select v-model="selectedModel" class="input w-full appearance-none truncate pr-8">
                <option value="all">{{ t('availableChannels.filters.model') }}: {{ t('availableChannels.filters.all') }}</option>
                <option v-for="model in modelOptions" :key="model" :value="model">{{ model }}</option>
              </select>
              <Icon name="chevronDown" size="xs" class="pointer-events-none absolute right-2.5 top-1/2 -translate-y-1/2 text-gray-400" />
            </label>

            <div class="flex items-center justify-end gap-2 sm:col-span-2 xl:col-span-1">
              <button
                v-if="hasActiveFilters"
                type="button"
                class="btn btn-secondary min-w-0 flex-1 sm:flex-none"
                @click="resetFilters"
              >
                <Icon name="x" size="sm" />
                <span>{{ t('availableChannels.clearFilters') }}</span>
              </button>
              <button
                type="button"
                @click="loadChannels"
                :disabled="loading"
                class="btn btn-secondary"
                :title="t('common.refresh', 'Refresh')"
              >
                <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
                <span class="hidden sm:inline">{{ t('common.refresh', 'Refresh') }}</span>
              </button>
            </div>
          </div>
        </div>
      </template>

      <template #table>
        <AvailableChannelsTable
          :columns="columnLabels"
          :rows="filteredChannels"
          :loading="loading"
          :user-group-rates="userGroupRates"
          pricing-key-prefix="availableChannels.pricing"
          :no-pricing-label="t('availableChannels.noPricing')"
          :no-models-label="t('availableChannels.noModels')"
          :empty-label="hasActiveFilters ? t('availableChannels.noFilterResults') : t('availableChannels.empty')"
        />
      </template>
    </TablePageLayout>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import Icon from '@/components/icons/Icon.vue'
import AvailableChannelsTable from '@/components/channels/AvailableChannelsTable.vue'
import userChannelsAPI, {
  type UserAvailableChannel,
  type UserAvailableGroup,
} from '@/api/channels'
import userGroupsAPI from '@/api/groups'
import { useAppStore } from '@/stores/app'
import { extractApiErrorMessage } from '@/utils/apiError'

const { t } = useI18n()
const appStore = useAppStore()

const channels = ref<UserAvailableChannel[]>([])
const userGroupRates = ref<Record<number, number>>({})
const loading = ref(false)
const searchQuery = ref('')
const selectedPlatform = ref('all')
const selectedGroupId = ref('all')
const selectedModel = ref('all')

const columnLabels = computed(() => ({
  name: t('availableChannels.columns.name'),
  description: t('availableChannels.columns.description'),
  platform: t('availableChannels.columns.platform'),
  groups: t('availableChannels.columns.groups'),
  supportedModels: t('availableChannels.columns.supportedModels'),
}))

const platformOptions = computed(() =>
  [...new Set(channels.value.flatMap((channel) => channel.platforms.map((section) => section.platform)))].sort(),
)

const groupOptions = computed(() => {
  const groups = new Map<number, UserAvailableGroup>()
  for (const channel of channels.value) {
    for (const section of channel.platforms) {
      for (const group of section.groups) groups.set(group.id, group)
    }
  }
  return [...groups.values()].sort((a, b) => a.name.localeCompare(b.name))
})

const modelOptions = computed(() => {
  const models = new Set<string>()
  for (const channel of channels.value) {
    for (const section of channel.platforms) {
      for (const model of section.supported_models) models.add(model.name)
    }
  }
  return [...models].sort()
})

const hasActiveFilters = computed(() =>
  Boolean(searchQuery.value.trim()) ||
  selectedPlatform.value !== 'all' ||
  selectedGroupId.value !== 'all' ||
  selectedModel.value !== 'all',
)

/**
 * Client-side facets narrow only the sections and models already authorized by
 * GET /channels/available. A channel is retained when its name/description
 * matches search, while structured filters still constrain its sections.
 */
const filteredChannels = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  const selectedGroup = selectedGroupId.value === 'all' ? null : Number(selectedGroupId.value)

  return channels.value
    .map((channel) => {
      const channelTextHit =
        channel.name.toLowerCase().includes(q) || (channel.description || '').toLowerCase().includes(q)

      const sections = channel.platforms
        .filter((section) => selectedPlatform.value === 'all' || section.platform === selectedPlatform.value)
        .map((section) => {
          const groups = selectedGroup == null
            ? section.groups
            : section.groups.filter((group) => group.id === selectedGroup)
          const models = selectedModel.value === 'all'
            ? section.supported_models
            : section.supported_models.filter((model) => model.name === selectedModel.value)

          const sectionTextHit =
            section.platform.toLowerCase().includes(q) ||
            groups.some((group) => group.name.toLowerCase().includes(q)) ||
            models.some((model) => model.name.toLowerCase().includes(q))

          if (q && !channelTextHit && !sectionTextHit) return null
          if (selectedGroup != null && groups.length === 0) return null
          if (selectedModel.value !== 'all' && models.length === 0) return null
          return { ...section, groups, supported_models: models }
        })
        .filter((section): section is UserAvailableChannel['platforms'][number] => section !== null)

      if (sections.length === 0) return null
      return { ...channel, platforms: sections }
    })
    .filter((channel): channel is UserAvailableChannel => channel !== null)
})

watch(platformOptions, (options) => {
  if (selectedPlatform.value !== 'all' && !options.includes(selectedPlatform.value)) selectedPlatform.value = 'all'
})

watch(groupOptions, (options) => {
  if (selectedGroupId.value !== 'all' && !options.some((group) => String(group.id) === selectedGroupId.value)) {
    selectedGroupId.value = 'all'
  }
})

watch(modelOptions, (options) => {
  if (selectedModel.value !== 'all' && !options.includes(selectedModel.value)) selectedModel.value = 'all'
})

function resetFilters() {
  searchQuery.value = ''
  selectedPlatform.value = 'all'
  selectedGroupId.value = 'all'
  selectedModel.value = 'all'
}

async function loadChannels() {
  loading.value = true
  try {
    // Channel visibility comes from the backend. Rates are an independent
    // decoration and failing to load them must not hide authorized channels.
    const [list, rates] = await Promise.all([
      userChannelsAPI.getAvailable(),
      userGroupsAPI.getUserGroupRates().catch((err: unknown) => {
        console.error('Failed to load user group rates:', err)
        return {} as Record<number, number>
      }),
    ])
    channels.value = list
    userGroupRates.value = rates
  } catch (err: unknown) {
    appStore.showError(extractApiErrorMessage(err, t('common.error')))
  } finally {
    loading.value = false
  }
}

onMounted(loadChannels)
</script>

<style scoped>
.channel-toolbar {
  display: flex;
  flex-direction: column;
  gap: 1rem;
}

.channel-toolbar-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 1rem;
}

.channel-eyebrow {
  margin-bottom: 0.35rem;
  color: rgb(13 148 136);
  font-size: 0.68rem;
  font-weight: 700;
  letter-spacing: 0.16em;
  text-transform: uppercase;
}

.channel-summary {
  display: flex;
  flex-shrink: 0;
  flex-direction: column;
  align-items: flex-end;
  gap: 0.15rem;
  color: rgb(107 114 128);
  font-size: 0.7rem;
  text-transform: uppercase;
  letter-spacing: 0.08em;
}

.channel-summary strong {
  color: rgb(17 24 39);
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  font-size: 1.25rem;
  letter-spacing: 0;
}

.channel-filter-grid {
  display: grid;
  grid-template-columns: minmax(0, 1.35fr) repeat(3, minmax(0, 1fr)) auto;
  gap: 0.65rem;
}

.dark .channel-summary strong {
  color: rgb(249 250 251);
}

@media (max-width: 1279px) {
  .channel-filter-grid {
    grid-template-columns: repeat(4, minmax(0, 1fr));
  }
}

@media (max-width: 640px) {
  .channel-toolbar-heading {
    align-items: flex-start;
  }

  .channel-filter-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }

  .channel-filter-grid > :first-child {
    grid-column: 1 / -1;
  }

  .channel-filter-grid > :last-child {
    grid-column: 1 / -1;
  }
}
</style>
