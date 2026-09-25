/**
 * V3 channel-status console.
 *
 * V3 is a group-scoped presentation layer over the passive aggregation
 * pipeline; it owns its own copy so its wording can evolve without touching
 * the V1/V2 views.
 */
export default {
  channelMonitorV3: {
    title: 'Channel status',
    description: 'Passive aggregation · streaming text traffic only',
    updatedTo: 'Updated to {time}',
    partialCoverage: 'Partial history coverage',
    loadFailed: 'Failed to load channel status',
    cacheRate: 'Cache rate',
    successRate: 'Availability',
    ttft: 'First token',
    samples: '{count} requests',
    healthScore: 'Health score',
    healthy: 'Healthy',
    warning: 'Watch',
    critical: 'Critical',
    unknown: 'Insufficient samples',
    allModels: 'All models',
    userRate: 'User rate',
    unknownGroup: 'Unknown group',
    timelineTooltip: '{time} · Availability {availability} · Cache {cache} · First token {ttft}',
    emptyTitle: 'No channel data',
    emptyDescription: 'There is no passive-monitor data for this time range yet',
    summary: 'Availability {success} · Cache {cache}',
    ranges: { '90m': '90m', '24h': '24h', '7d': '7d', '30d': '30d' },
  },
}
