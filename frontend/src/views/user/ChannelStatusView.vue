<template>
  <ChannelStatusV1View v-if="view === 'v1'" />
  <ChannelStatusV2View v-else-if="view === 'v2'" />
  <ChannelStatusV3View v-else />
</template>

<script setup lang="ts">
import { computed } from 'vue'
import {
  isChannelMonitorV1Mode,
  isChannelMonitorV3Mode,
} from '@/utils/featureFlags'
import ChannelStatusV1View from './ChannelStatusV1View.vue'
import ChannelStatusV2View from './ChannelStatusV2View.vue'
import ChannelStatusV3View from './ChannelStatusV3View.vue'

/**
 * Resolves which channel-status console to render.
 *
 * V1 and V2 keep their previous behaviour, and a disabled feature flag still
 * falls through to V2 as before (every mode predicate is false in that case).
 * V3 renders only when the backend mode is actually v3.
 */
const view = computed<'v1' | 'v2' | 'v3'>(() => {
  if (isChannelMonitorV1Mode()) return 'v1'
  if (isChannelMonitorV3Mode()) return 'v3'
  return 'v2'
})
</script>
