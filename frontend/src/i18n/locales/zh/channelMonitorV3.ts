/**
 * V3 channel-status console.
 *
 * V3 is a group-scoped presentation layer over the passive aggregation
 * pipeline; it owns its own copy so its wording can evolve without touching
 * the V1/V2 views.
 */
export default {
  channelMonitorV3: {
    title: '渠道状态',
    description: '被动聚合 · 仅统计流式文本流量',
    updatedTo: '更新至 {time}',
    partialCoverage: '部分历史覆盖',
    loadFailed: '渠道状态加载失败',
    cacheRate: '缓存率',
    successRate: '可用率',
    ttft: '首 Token',
    samples: '{count} 次请求',
    healthScore: '健康度',
    healthy: '健康',
    warning: '需关注',
    critical: '异常',
    unknown: '样本不足',
    allModels: '全部模型',
    userRate: '用户倍率',
    unknownGroup: '未知分组',
    timelineTooltip: '{time} · 可用率 {availability} · 缓存率 {cache} · 首 Token {ttft}',
    emptyTitle: '暂无渠道数据',
    emptyDescription: '当前时间范围内还没有可展示的被动监控数据',
    summary: '可用率 {success} · 缓存率 {cache}',
    ranges: { '90m': '90m', '24h': '24h', '7d': '7d', '30d': '30d' },
  },
}
