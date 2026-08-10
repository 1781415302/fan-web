import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from './auth'
import { TOKEN_STORAGE_KEY } from '../api'

vi.mock('../api', () => ({
  TOKEN_STORAGE_KEY: 'fan_web_token',
  ApiError: class ApiError extends Error {
    readonly code: number
    constructor(code: number, message: string) {
      super(message)
      this.code = code
    }
  },
  unwrap: (response: { data: { code: number; data: unknown } }) => {
    if (response.data.code !== 0) throw new Error(response.data as unknown as string)
    return response.data.data
  },
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import api from '../api'

const mockedGet = vi.mocked(api.get)
const mockedPost = vi.mocked(api.post)

function newStore() {
  setActivePinia(createPinia())
  return useAuthStore()
}

describe('auth store', () => {
  beforeEach(() => {
    window.localStorage.clear()
    mockedGet.mockReset()
    mockedPost.mockReset()
  })

  it('does not call /auth/me when no token stored', async () => {
    const store = newStore()
    await store.initialize()
    expect(mockedGet).not.toHaveBeenCalled()
    expect(store.initialized).toBe(true)
    expect(store.isAuthenticated).toBe(false)
  })

  it('recovers user when token exists and /auth/me succeeds', async () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'tok')
    mockedGet.mockResolvedValue({
      data: { code: 0, message: 'ok', data: { id: 1, username: 'alice', is_admin: true, created_at: '' } },
    })
    const store = newStore()
    await store.initialize()
    expect(mockedGet).toHaveBeenCalledWith('/auth/me')
    expect(store.user?.username).toBe('alice')
    expect(store.isAdmin).toBe(true)
  })

  it('clears session when /auth/me fails', async () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'bad')
    mockedGet.mockRejectedValue(new Error('unauthorized'))
    const store = newStore()
    await store.initialize()
    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('login stores token user and localStorage', async () => {
    mockedPost.mockResolvedValue({
      data: {
        code: 0,
        message: 'ok',
        data: { token: 'fresh-token', user: { id: 1, username: 'alice', is_admin: false, created_at: '' } },
      },
    })
    const store = newStore()
    await store.login('alice', 'secret')
    expect(store.token).toBe('fresh-token')
    expect(store.user?.username).toBe('alice')
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('fresh-token')
  })

  it('logout clears local state even when api fails', async () => {
    mockedPost.mockRejectedValue(new Error('offline'))
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'tok')
    const store = newStore()
    await store.logout()
    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })
})
