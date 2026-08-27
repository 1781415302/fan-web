import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import axios from 'axios'

import api, {
  ApiError,
  TOKEN_STORAGE_KEY,
  type ApiResponse,
  unwrap,
} from '../api'
import type { LoginData, User } from '../types/auth'

export const useAuthStore = defineStore('auth', () => {
  const token = ref<string | null>(localStorage.getItem(TOKEN_STORAGE_KEY))
  const user = ref<User | null>(null)
  const initialized = ref(false)
  let initializePromise: Promise<void> | null = null
  // 后端不可达（网络错误/超时）时，避免每次客户端路由切换都重新发起 /auth/me
  // （默认 10s 超时），导致导航被阻塞。记录最近一次网络错误时间，冷却期内直接复用
  // “未初始化”状态；路由守卫的 `token && !initialized` 分支会放行，待冷却期过后
  // 下一次导航再尝试恢复会话。
  let lastNetworkErrorAt = 0
  const INIT_RETRY_COOLDOWN_MS = 10_000

  const isAuthenticated = computed(() => Boolean(token.value && user.value))
  const isAdmin = computed(() => Boolean(user.value?.is_admin))

  async function initialize() {
    if (initialized.value) {
      return
    }
    if (initializePromise) {
      return initializePromise
    }
    if (lastNetworkErrorAt && Date.now() - lastNetworkErrorAt < INIT_RETRY_COOLDOWN_MS) {
      return
    }

    initializePromise = (async () => {
      if (!token.value) {
        initialized.value = true
        return
      }

      try {
        const response = await api.get<ApiResponse<User>>('/auth/me')
        user.value = unwrap(response)
        initialized.value = true
      } catch (error: unknown) {
        // 只有认证失败（后端明确返回未认证）才清除本地会话；后端对失效
        // token 的响应是 HTTP 200 + code 2001，由成功拦截器统一处理。
        // 网络错误 / 超时（error.response 为 undefined）时 token 仍然有效，
        // 保留会话并保持未初始化，后续导航会再次尝试恢复。
        if (isAuthFailure(error)) {
          clearSession()
          initialized.value = true
        } else {
          // 网络错误 / 超时：保留会话并保持未初始化，记录冷却期避免重复请求。
          lastNetworkErrorAt = Date.now()
        }
      }
    })()

    try {
      await initializePromise
    } finally {
      initializePromise = null
    }
  }

  async function login(username: string, password: string) {
    const response = await api.post<ApiResponse<LoginData>>('/auth/login', {
      username,
      password,
    })
    const data = unwrap(response)
    token.value = data.token
    user.value = data.user
    localStorage.setItem(TOKEN_STORAGE_KEY, data.token)
    initialized.value = true
  }

  function setSession(newToken: string, newUser: User) {
    token.value = newToken
    user.value = newUser
    localStorage.setItem(TOKEN_STORAGE_KEY, newToken)
    initialized.value = true
  }

  async function logout() {
    try {
      if (token.value) {
        await api.post('/auth/logout')
      }
    } catch {
      // 登出时即使网络不可用，也要清理本地登录状态。
    } finally {
      clearSession()
      initialized.value = true
    }
  }

  function clearSession() {
    token.value = null
    user.value = null
    localStorage.removeItem(TOKEN_STORAGE_KEY)
  }

  return {
    token,
    user,
    initialized,
    isAuthenticated,
    isAdmin,
    initialize,
    login,
    logout,
    setSession,
  }
})

function isAuthFailure(error: unknown): boolean {
  if (error instanceof ApiError) {
    return error.code === 2001
  }
  return axios.isAxiosError(error) && error.response?.status === 401
}

export { ApiError }
