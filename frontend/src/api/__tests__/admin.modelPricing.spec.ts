import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const { get, put, remove } = vi.hoisted(() => ({
  get: vi.fn(),
  put: vi.fn(),
  remove: vi.fn(),
}))

vi.mock('@/api/client', () => ({
  apiClient: { get, put, delete: remove },
}))

import { list, remove as reset, update } from '@/api/admin/modelPricing'

describe('admin model pricing API', () => {
  beforeEach(() => {
    localStorage.clear()
    sessionStorage.clear()
    localStorage.setItem('auth_user', JSON.stringify({ id: 7 }))
    get.mockReset()
    put.mockReset()
    remove.mockReset()
    vi.spyOn(globalThis.crypto, 'randomUUID').mockReturnValue('11111111-1111-4111-8111-111111111111')
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('lists only through the admin model pricing endpoint', async () => {
    get.mockResolvedValue({ data: { models: [] } })

    await expect(list()).resolves.toEqual({ models: [] })
    expect(get).toHaveBeenCalledWith('/admin/model-pricing')
  })

  it('sends an idempotency key for updates and clears it after success', async () => {
    put.mockResolvedValue({ data: { name: 'gpt-test' } })

    await update('gpt-test', { input_cost_per_token: 1 })

    expect(put).toHaveBeenCalledWith('/admin/model-pricing/gpt-test', { input_cost_per_token: 1 }, {
      headers: { 'Idempotency-Key': 'model-pricing-update-gpt-test-11111111-1111-4111-8111-111111111111' },
    })
    expect(sessionStorage.length).toBe(0)
  })

  it('reuses update and reset keys after an ambiguous failure', async () => {
    put.mockRejectedValueOnce(new Error('timeout'))
    await expect(update('gpt-test', {})).rejects.toThrow('timeout')
    const updateHeaders = put.mock.calls[0][2].headers

    put.mockResolvedValueOnce({ data: { name: 'gpt-test' } })
    await update('gpt-test', {})
    expect(put.mock.calls[1][2].headers).toEqual(updateHeaders)

    remove.mockRejectedValueOnce(new Error('timeout'))
    await expect(reset('gpt-test')).rejects.toThrow('timeout')
    const resetHeaders = remove.mock.calls[0][1].headers

    remove.mockResolvedValueOnce({})
    await reset('gpt-test')
    expect(remove.mock.calls[1][1].headers).toEqual(resetHeaders)
    expect(remove.mock.calls[0][0]).toBe('/admin/model-pricing/gpt-test')
    expect(sessionStorage.length).toBe(0)
  })
})
