import { createRouter, createWebHistory } from 'vue-router'

import { useAuthStore } from '../stores/auth'
import { getSetupStatus } from '../api/setup'

declare module 'vue-router' {
  interface RouteMeta {
    requiresAuth?: boolean
    requiresAdmin?: boolean
  }
}

const router = createRouter({
  history: createWebHistory(),
  routes: [
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
      meta: { requiresAuth: true },
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
  ],
})

router.beforeEach(async (to) => {
  const authStore = useAuthStore()

  let configured = true
  try {
    configured = await getSetupStatus()
  } catch {
    // 请求失败（例如网络不可用）时按已初始化处理，避免误跳转。
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

function getSafeRedirect(value: unknown) {
  if (typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')) {
    return value
  }
  return { name: 'home' }
}

export default router
