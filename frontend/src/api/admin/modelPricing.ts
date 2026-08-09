/**
 * Admin model pricing API endpoints.
 * The server exposes only models currently authorized by active channels.
 */

import { apiClient } from '../client'

export type ModelPricingFieldKey =
  | 'input_cost_per_token'
  | 'input_cost_per_token_priority'
  | 'output_cost_per_token'
  | 'output_cost_per_token_priority'
  | 'cache_creation_input_token_cost'
  | 'cache_creation_input_token_cost_priority'
  | 'cache_creation_input_token_cost_above_1hr'
  | 'cache_read_input_token_cost'
  | 'cache_read_input_token_cost_priority'
  | 'long_context_input_token_threshold'
  | 'long_context_input_cost_multiplier'
  | 'long_context_output_cost_multiplier'
  | 'output_cost_per_image'
  | 'output_cost_per_image_token'
  | 'input_cost_per_image_token'

export type ModelPricingFieldValues = Partial<Record<ModelPricingFieldKey, number>>

export interface LiteLLMModelPricing extends ModelPricingFieldValues {
  supports_service_tier?: boolean
  litellm_provider?: string
  mode?: string
  supports_prompt_caching?: boolean
}

export type ModelPricingOverride = ModelPricingFieldValues

export interface ModelPricingItem {
  name: string
  platform: string
  built_in: LiteLLMModelPricing | null
  override: ModelPricingOverride
  effective: LiteLLMModelPricing | null
}

export interface ModelPricingListResponse {
  models: ModelPricingItem[]
}

export type ModelPricingUpdate = ModelPricingFieldValues

type Mutation = 'update' | 'reset'

const operationKeys = new Map<string, string>()

function getCurrentAdminID(): string | null {
  try {
    const rawUser = globalThis.localStorage?.getItem('auth_user')
    if (!rawUser) return null

    const user = JSON.parse(rawUser) as { id?: unknown }
    if (typeof user.id !== 'number' || !Number.isSafeInteger(user.id) || user.id <= 0) return null
    return String(user.id)
  } catch {
    return null
  }
}

function mutationScope(mutation: Mutation, model: string): { storageKey: string; adminID: string } | null {
  const adminID = getCurrentAdminID()
  if (!adminID) return null
  return {
    adminID,
    storageKey: `sub2api:admin:model-pricing:${mutation}:${adminID}:${model.toLowerCase()}`
  }
}

function getStoredMutationKey(storageKey: string): string | null {
  try {
    return globalThis.sessionStorage?.getItem(storageKey) ?? null
  } catch {
    return null
  }
}

function storeMutationKey(storageKey: string, key: string | null): void {
  try {
    if (key) globalThis.sessionStorage?.setItem(storageKey, key)
    else globalThis.sessionStorage?.removeItem(storageKey)
  } catch {
    // In-memory retry protection still works when browser storage is unavailable.
  }
}

function getMutationKey(mutation: Mutation, model: string): { scope: string | null; key: string } {
  const scope = mutationScope(mutation, model)
  const storedKey = scope ? operationKeys.get(scope.storageKey) ?? getStoredMutationKey(scope.storageKey) : null
  const key = storedKey ?? `model-pricing-${mutation}-${model}-${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${Math.random().toString(36).slice(2)}`}`

  if (scope) {
    operationKeys.set(scope.storageKey, key)
    storeMutationKey(scope.storageKey, key)
  }

  return { scope: scope?.storageKey ?? null, key }
}

function clearMutationKey(scope: string | null): void {
  if (!scope) return
  operationKeys.delete(scope)
  storeMutationKey(scope, null)
}

export async function list(): Promise<ModelPricingListResponse> {
  const { data } = await apiClient.get<ModelPricingListResponse>('/admin/model-pricing')
  return data
}

export async function update(model: string, payload: ModelPricingUpdate): Promise<ModelPricingItem> {
  const mutation = getMutationKey('update', model)
  const { data } = await apiClient.put<ModelPricingItem>(
    `/admin/model-pricing/${encodeURIComponent(model)}`,
    payload,
    { headers: { 'Idempotency-Key': mutation.key } }
  )
  clearMutationKey(mutation.scope)
  return data
}

export async function remove(model: string): Promise<void> {
  const mutation = getMutationKey('reset', model)
  await apiClient.delete(`/admin/model-pricing/${encodeURIComponent(model)}`, {
    headers: { 'Idempotency-Key': mutation.key }
  })
  clearMutationKey(mutation.scope)
}

const modelPricingAPI = { list, update, remove }

export default modelPricingAPI
