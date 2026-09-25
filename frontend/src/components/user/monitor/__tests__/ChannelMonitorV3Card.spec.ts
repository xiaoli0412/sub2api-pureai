import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import type { MonitorHealth, MonitorMatrixRow, MonitorMetric } from '@/api/channelMonitorV2'
import zhChannelMonitorV3 from '@/i18n/locales/zh/channelMonitorV3'
import zhDashboard from '@/i18n/locales/zh/dashboard'
import ChannelMonitorV3Card from '../ChannelMonitorV3Card.vue'

const v3 = zhChannelMonitorV3.channelMonitorV3
const monitorCommon = zhDashboard.monitorCommon

// `vitest.config.ts` aliases vue-i18n to its runtime-only build, so the message
// compiler is unavailable in tests and the real `t()` would echo keys back.
// Resolve against the actual shipped zh messages instead: the assertions below
// then still prove the component reads keys that exist and mean the right thing.
//
// The factory is hoisted above the imports, so it must import the locale modules
// itself rather than close over module-scope bindings.
vi.mock('vue-i18n', async (importOriginal) => {
  const actual = await importOriginal<typeof import('vue-i18n')>()
  const [v3Module, dashboardModule] = await Promise.all([
    import('@/i18n/locales/zh/channelMonitorV3'),
    import('@/i18n/locales/zh/dashboard'),
  ])
  const messages: Record<string, unknown> = {
    ...v3Module.default,
    monitorCommon: (dashboardModule.default as { monitorCommon: unknown }).monitorCommon,
  }
  const lookup = (path: string): string => {
    const value = path
      .split('.')
      .reduce<unknown>((acc, part) => (acc == null ? acc : (acc as Record<string, unknown>)[part]), messages)
    return typeof value === 'string' ? value : path
  }
  return {
    ...actual,
    useI18n: () => ({ t: lookup, locale: { value: 'zh' } }),
  }
})

function metric(overrides: Partial<MonitorMetric> = {}): MonitorMetric {
  return {
    success_requests: 100,
    error_requests: 0,
    request_count: 100,
    token_count: 0,
    rpm: 0,
    tpm: 0,
    error_rate: 0,
    cache_rate: 0.5,
    cache_rate_numerator: 0,
    cache_rate_denominator: 0,
    ttft: { sample_count: 10, p50_ms: 1200, p95_ms: 3000, avg_ms: 1500 },
    duration: { sample_count: 10, p50_ms: 5000, p95_ms: 9000, avg_ms: 6000 },
    ...overrides,
  }
}

function health(overall: MonitorHealth['overall']): MonitorHealth {
  return { overall, error_rate: overall, ttft: overall, minimum_sample: 50 }
}

function row(overrides: Partial<MonitorMatrixRow> = {}): MonitorMatrixRow {
  return {
    platform: 'anthropic',
    group_id: 7,
    group_name: '主力渠道',
    metrics: metric(),
    health: health('healthy'),
    buckets: [],
    ...overrides,
  }
}

function mountCard(props: Record<string, unknown> = {}) {
  return mount(ChannelMonitorV3Card, {
    props: { row: row(), countdownSeconds: 30, timelineLength: 18, ...props },
  })
}

describe('ChannelMonitorV3Card', () => {
  it('renders the group name and the translated platform label', () => {
    const wrapper = mountCard()
    expect(wrapper.text()).toContain('主力渠道')
    expect(wrapper.text()).toContain(monitorCommon.providers.anthropic)
  })

  it('falls back to the unknown-group label when the row has no name', () => {
    const wrapper = mountCard({ row: row({ group_name: undefined }) })
    expect(wrapper.text()).toContain(v3.unknownGroup)
  })

  // The card answers "how is this channel right now", so the newest completed
  // bucket must win over the selected-range aggregate.
  it('prefers the newest bucket over the range aggregate', () => {
    const wrapper = mountCard({
      row: row({
        // Range aggregate looks terrible...
        metrics: metric({ error_rate: 0.9 }),
        health: health('critical'),
        // ...but the newest bucket is healthy.
        buckets: [
          { bucket_start: '2026-09-24T10:00:00Z', metrics: metric({ error_rate: 0.9 }), health: health('critical') },
          { bucket_start: '2026-09-24T10:02:00Z', metrics: metric({ error_rate: 0 }), health: health('healthy') },
        ],
      }),
    })

    expect(wrapper.text()).toContain('100.0%')
    expect(wrapper.text()).not.toContain('10.0%')
    expect(wrapper.text()).toContain(monitorCommon.status.operational)
  })

  it('picks the newest bucket even when buckets arrive out of order', () => {
    const wrapper = mountCard({
      row: row({
        buckets: [
          { bucket_start: '2026-09-24T10:05:00Z', metrics: metric({ error_rate: 0.5 }), health: health('warning') },
          { bucket_start: '2026-09-24T10:01:00Z', metrics: metric({ error_rate: 0 }), health: health('healthy') },
        ],
      }),
    })
    expect(wrapper.text()).toContain(monitorCommon.status.degraded)
  })

  it('maps health states onto the shared status labels', () => {
    const cases: Array<[MonitorHealth['overall'], string]> = [
      ['healthy', monitorCommon.status.operational],
      ['warning', monitorCommon.status.degraded],
      ['critical', monitorCommon.status.failed],
      // Unknown health has no shared label: the card uses its own wording.
      ['unknown', v3.unknown],
    ]
    for (const [overall, label] of cases) {
      const wrapper = mountCard({ row: row({ health: health(overall), buckets: [] }) })
      expect(wrapper.text()).toContain(label)
    }
  })

  it('formats the user rate multiplier and degrades to a dash', () => {
    expect(mountCard({ userRateMultiplier: 1.5 }).text()).toContain('1.50x')
    expect(mountCard({ userRateMultiplier: null }).text()).toContain('-')
  })

  it('colours the availability number by the latest bucket band', () => {
    const wrapper = mountCard({
      row: row({
        buckets: [{ bucket_start: '2026-09-24T10:00:00Z', metrics: metric({ error_rate: 0.75 }), health: health('critical') }],
        health: health('critical'),
      }),
    })
    // 25% availability is the worst band, so its text colour must be the band's.
    const availability = wrapper.findAll('div').find(el => el.text() === '25.0%')
    expect(availability).toBeDefined()
    expect(availability!.classes().join(' ')).toContain('text-gray-950')
  })
})
