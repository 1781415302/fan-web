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
      console.error(`API 请求失败（${error.response.status}）`, error.response.data)
    }
    return Promise.reject(error)
  },
)

export function unwrap<T>(response: AxiosResponse<ApiResponse<T>>): T {
  const result = response.data
  if (result.code !== 0) {
    throw new ApiError(result.code, result.message)
  }
  return result.data
}

function handleUnauthorized(response: AxiosResponse<unknown>) {
  if (response.status !== 401 && !isUnauthenticatedResponse(response.data)) {
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

function redirectToLogin() {
  if (window.location.pathname !== '/login') {
    window.location.assign('/login')
  }
}

export default api
