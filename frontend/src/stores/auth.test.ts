import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { useAuthStore } from './auth'
import { ApiError, TOKEN_STORAGE_KEY, resetLoginRedirect } from '../api'

vi.mock('../api', () => ({
  TOKEN_STORAGE_KEY: 'fan_web_token',
  ApiError: class ApiError extends Error {
    readonly code: number
    constructor(code: number, message: string) {
      super(message)
      this.code = code
    }
  },
  unwrap: (response: { data: { code: number; message: string; data: unknown } }) => {
    if (response.data.code !== 0) throw new ApiError(response.data.code, response.data.message)
    return response.data.data
  },
  resetLoginRedirect: vi.fn(),
  default: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))

import api from '../api'

const mockedGet = vi.mocked(api.get)
const mockedPost = vi.mocked(api.post)
const mockedResetLoginRedirect = vi.mocked(resetLoginRedirect)

function newStore() {
  setActivePinia(createPinia())
  return useAuthStore()
}

describe('auth store', () => {
  beforeEach(() => {
    window.localStorage.clear()
    mockedGet.mockReset()
    mockedPost.mockReset()
    mockedResetLoginRedirect.mockReset()
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

  it('clears session when /auth/me reports unauthenticated code 2001', async () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'bad')
    mockedGet.mockRejectedValue(new ApiError(2001, 'unauthorized'))
    const store = newStore()
    await store.initialize()
    expect(store.token).toBeNull()
    expect(store.user).toBeNull()
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBeNull()
  })

  it('keeps a valid token when /auth/me fails with a network error', async () => {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, 'tok')
    mockedGet.mockRejectedValue(new Error('network down'))
    const store = newStore()
    await store.initialize()
    expect(store.token).toBe('tok')
    expect(store.user).toBeNull()
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('tok')
    // 未初始化为已失败状态，后续导航会重试恢复会话。
    expect(store.initialized).toBe(false)
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

  // 登录是 SPA 内跳转，不发生整页刷新；若不复闩锁，本次会话之后再收到
  // 2001 就不会再跳登录页，用户会卡在失效会话里。
  it('resets the unauthorized redirect latch on login and setSession', async () => {
    mockedPost.mockResolvedValue({
      data: {
        code: 0,
        message: 'ok',
        data: { token: 'fresh-token', user: { id: 1, username: 'alice', is_admin: false, created_at: '' } },
      },
    })
    const store = newStore()
    await store.login('alice', 'secret')
    expect(mockedResetLoginRedirect).toHaveBeenCalledTimes(1)

    store.setSession('another-token', { id: 2, username: 'bob', is_admin: false, created_at: '' })
    expect(mockedResetLoginRedirect).toHaveBeenCalledTimes(2)
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
