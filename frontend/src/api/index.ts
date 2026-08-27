import axios, { type AxiosResponse, type InternalAxiosRequestConfig } from 'axios'

export const TOKEN_STORAGE_KEY = 'fan_web_token'

export interface ApiResponse<T> {
  code: number
  message: string
  data: T
}

export class ApiError extends Error {
  readonly code: number

  constructor(code: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.code = code
  }
}

const api = axios.create({
  baseURL: '/api',
  timeout: 10_000,
})

api.interceptors.request.use((config: InternalAxiosRequestConfig) => {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY)
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

api.interceptors.response.use(
  (response) => {
    handleUnauthorized(response)
    return response
  },
  (error: unknown) => {
    if (axios.isAxiosError(error) && error.response) {
      if (error.response.status === 401) {
        clearStoredToken()
        redirectToLogin()
      }
      // 不打印完整响应体，避免登录/setup 等接口的敏感字段泄露到控制台或日志。
      console.error(`API 请求失败（${error.response.status}）`)
    }
    return Promise.reject(error)
  },
)

export function unwrap<T>(response: AxiosResponse<ApiResponse<T>>): T {
  const result = response.data as unknown
  // 后端在 200 下可能返回非 ApiResponse（如反代 502 页面、静态错误页），
  // 先校验结构，否则抛出更明确的“响应格式错误”，而非把整段 HTML 当错误信息。
  if (typeof result !== 'object' || result === null || !('code' in result)) {
    throw new ApiError(-1, '响应格式错误：未返回有效的 ApiResponse')
  }
  const apiResult = result as ApiResponse<T>
  if (apiResult.code !== 0) {
    throw new ApiError(apiResult.code, apiResult.message)
  }
  return apiResult.data
}

function handleUnauthorized(response: AxiosResponse<unknown>) {
  // 成功拦截器只处理 2xx 响应（401 不会进入这里，由错误拦截器处理），
  // 因此只需判断是否为“明确未认证”（code 2001）的响应。
  if (!isUnauthenticatedResponse(response.data)) {
    return
  }
  clearStoredToken()
  redirectToLogin()
}

function isUnauthenticatedResponse(data: unknown): boolean {
  if (typeof data !== 'object' || data === null || !('code' in data)) {
    return false
  }
  return data.code === 2001
}

function clearStoredToken() {
  localStorage.removeItem(TOKEN_STORAGE_KEY)
}

// 同一会话内的未认证跳转只触发一次，避免后台轮询/进度上报在 2001 时反复触发整页刷新。
let redirectedToLogin = false

function redirectToLogin() {
  if (redirectedToLogin) {
    return
  }
  redirectedToLogin = true
  if (window.location.pathname === '/login') {
    return
  }
  // 与路由守卫一致地携带 redirect 参数，登录后回到原页面。
  const redirect = window.location.pathname + window.location.search
  window.location.assign(`/login?redirect=${encodeURIComponent(redirect)}`)
}

export default api
