import { createRouter, createWebHistory, type RouterHistory } from 'vue-router'

import { useAuthStore } from '../stores/auth'
import { getSetupStatus } from '../api/setup'

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresAdmin?: boolean
  }
}

export type SetupStatusLoader = () => Promise<boolean>

const routes = [
  {
    path: '/setup',
    name: 'setup',
    component: () => import('../views/SetupView.vue'),
  },
  {
    path: '/login',
    name: 'login',
    component: () => import('../views/LoginView.vue'),
  },
  {
    path: '/',
    name: 'home',
    meta: { requiresAuth: true },
    component: () => import('../views/HomeView.vue'),
  },
  {
    path: '/animes',
    name: 'anime-list',
    meta: { requiresAuth: true },
    component: () => import('../views/AnimeListView.vue'),
  },
  {
    path: '/animes/new',
    name: 'anime-add',
    meta: { requiresAuth: true, requiresAdmin: true },
    component: () => import('../views/AnimeAddView.vue'),
  },
  {
    path: '/animes/:id',
    name: 'anime-detail',
    meta: { requiresAuth: true },
    component: () => import('../views/AnimeDetailView.vue'),
  },
  {
    path: '/animes/:id/watch/:epId',
    name: 'watch',
    meta: { requiresAuth: true },
    component: () => import('../views/WatchView.vue'),
  },
  {
    path: '/admin/users',
    name: 'admin-users',
    meta: { requiresAuth: true, requiresAdmin: true },
    component: () => import('../views/AdminUsersView.vue'),
  },
  {
    path: '/admin/update',
    name: 'admin-update',
    meta: { requiresAuth: true, requiresAdmin: true },
    component: () => import('../views/UpdateView.vue'),
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: { name: 'home' },
  },
]

export function createAppRouter(history: RouterHistory, setupStatus: SetupStatusLoader) {
  const router = createRouter({ history, routes })

  // setup 状态只在初始化流程中才会变化：首次确认已配置后缓存结果，
  // 后续导航直接用缓存，避免每次导航都串行等待网络往返。
  let cachedConfigured: boolean | undefined

  router.beforeEach(async (to) => {
    const authStore = useAuthStore()

    let configured = true
    if (cachedConfigured !== undefined) {
      configured = cachedConfigured
    } else {
      try {
        configured = await setupStatus()
        // 只缓存“已配置”状态；未配置时状态仍可能变化（初始化进行中），
        // 请求失败（例如网络不可用）也不缓存，下次导航会重试。
        if (configured) {
          cachedConfigured = true
        }
      } catch {
        // 请求失败（例如网络不可用）时按已初始化处理，避免误跳转。
      }
    }

    if (!configured && to.name !== 'setup') {
      return { name: 'setup' }
    }
    if (configured && to.name === 'setup') {
      return { name: 'login' }
    }

    await authStore.initialize()

    if (to.name === 'login') {
      if (!authStore.isAuthenticated) {
        return true
      }
      return getSafeRedirect(to.query.redirect)
    }

    if (to.meta.requiresAuth && !authStore.isAuthenticated) {
      if (authStore.token && !authStore.initialized) {
        return true
      }
      return {
        name: 'login',
        query: { redirect: to.fullPath },
      }
    }

    if (to.meta.requiresAdmin && !authStore.isAdmin) {
      return { name: 'home' }
    }

    return true
  })

  return router
}

export function getSafeRedirect(value: unknown) {
  if (typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')) {
    return value
  }
  return { name: 'home' }
}

const router = createAppRouter(createWebHistory(), async () => getSetupStatus())

export default router