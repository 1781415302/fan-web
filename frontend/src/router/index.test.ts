import { createMemoryHistory } from 'vue-router'
import { createPinia, setActivePinia } from 'pinia'

import { describe, expect, it, beforeEach, vi } from 'vitest'

import { createAppRouter, getSafeRedirect } from './index'
import { useAuthStore } from '../stores/auth'
import { TOKEN_STORAGE_KEY } from '../api'

async function go(router: import('vue-router').Router, path: string) {
  await router.push(path)
  await router.isReady()
  return router.currentRoute.value
}

function makeRouter(setup: () => Promise<boolean>) {
  return createAppRouter(createMemoryHistory(), vi.fn(setup))
}

describe('router guards', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    window.localStorage.clear()
  })

  it('redirects to /setup when not initialized', async () => {
    const router = makeRouter(async () => false)
    const route = await go(router, '/animes')
    expect(route.name).toBe('setup')
  })

  it('redirects away from /setup when initialized', async () => {
    const router = makeRouter(async () => true)
    const route = await go(router, '/setup')
    expect(route.name).toBe('login')
  })

  it('redirects unauthenticated user to login with redirect query', async () => {
    const router = makeRouter(async () => true)
    const route = await go(router, '/animes')
    expect(route.name).toBe('login')
    expect(route.query.redirect).toBe('/animes')
  })

  it('redirects non-admin away from admin route', async () => {
    const store = useAuthStore()
    store.setSession('token', { id: 2, username: 'normal', is_admin: false, created_at: '' })
    const router = makeRouter(async () => true)
    const route = await go(router, '/admin/users')
    expect(route.name).toBe('home')
  })

  it('redirects non-admin away from anime-add route', async () => {
    const store = useAuthStore()
    store.setSession('token', { id: 2, username: 'normal', is_admin: false, created_at: '' })
    const router = makeRouter(async () => true)
    const route = await go(router, '/animes/new')
    expect(route.name).toBe('home')
  })

  it('allows admin to reach anime-add route', async () => {
    const store = useAuthStore()
    store.setSession('token', { id: 1, username: 'root', is_admin: true, created_at: '' })
    const router = makeRouter(async () => true)
    const route = await go(router, '/animes/new')
    expect(route.name).toBe('anime-add')
  })

  it('allows admin to reach admin route', async () => {
    const store = useAuthStore()
    store.setSession('token', { id: 1, username: 'root', is_admin: true, created_at: '' })
    const router = makeRouter(async () => true)
    const route = await go(router, '/admin/users')
    expect(route.name).toBe('admin-users')
  })

  it('treats setup status failure as initialized', async () => {
    const router = makeRouter(async () => {
      throw new Error('network down')
    })
    const route = await go(router, '/setup')
    // 请求失败按已初始化处理 -> /setup 被重定向到 login。
    expect(route.name).toBe('login')
  })

  it('keeps token in localStorage during router navigation', async () => {
    const store = useAuthStore()
    store.setSession('tok', { id: 1, username: 'root', is_admin: true, created_at: '' })
    expect(window.localStorage.getItem(TOKEN_STORAGE_KEY)).toBe('tok')
  })
})

describe('getSafeRedirect', () => {
  it('accepts single-slash internal paths', () => {
    expect(getSafeRedirect('/animes')).toBe('/animes')
    expect(getSafeRedirect('/animes/3/watch/9')).toBe('/animes/3/watch/9')
  })

  it('rejects protocol-relative //host', () => {
    expect(getSafeRedirect('//evil.com')).toEqual({ name: 'home' })
  })

  it('rejects full urls', () => {
    expect(getSafeRedirect('https://evil.com')).toEqual({ name: 'home' })
  })

  it('rejects non-string values', () => {
    expect(getSafeRedirect(42)).toEqual({ name: 'home' })
    expect(getSafeRedirect(null)).toEqual({ name: 'home' })
    expect(getSafeRedirect(undefined)).toEqual({ name: 'home' })
  })
})