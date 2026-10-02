import { apiClient } from './client'
import type { UsageDisplayConfig } from '@/types/usageDisplay'
import { normalizeUsageDisplayConfig } from '@/utils/usageDisplay'

export async function getUsageDisplayConfig(): Promise<UsageDisplayConfig> {
  const { data } = await apiClient.get<UsageDisplayConfig>('/settings/usage-display')
  return normalizeUsageDisplayConfig(data)
}

export async function getAdminUsageDisplayConfig(): Promise<UsageDisplayConfig> {
  const { data } = await apiClient.get<UsageDisplayConfig>('/admin/settings/usage-display')
  return normalizeUsageDisplayConfig(data)
}

export async function updateAdminUsageDisplayConfig(
  config: UsageDisplayConfig,
): Promise<UsageDisplayConfig> {
  const { data } = await apiClient.put<UsageDisplayConfig>('/admin/settings/usage-display', config)
  return normalizeUsageDisplayConfig(data)
}
