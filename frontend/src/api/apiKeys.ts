import api, { type ApiResponse, unwrap } from './index'
import type { APIKeyItem, CreateAPIKeyResponse } from '../types/apiKey'

// 外部工具（如 Hermes）接入用的 API Key 管理，仅管理员 JWT 可访问。
// 明文 api_key 只在 createAPIKey 当次响应中返回，调用方不得持久化。

export async function listAPIKeys(): Promise<APIKeyItem[]> {
  const response = await api.get<ApiResponse<APIKeyItem[]>>('/admin/api-keys')
  return unwrap(response)
}

export async function createAPIKey(name: string): Promise<CreateAPIKeyResponse> {
  const response = await api.post<ApiResponse<CreateAPIKeyResponse>>('/admin/api-keys', { name })
  return unwrap(response)
}

export async function deleteAPIKey(id: number): Promise<void> {
  const response = await api.delete<ApiResponse<null>>(`/admin/api-keys/${id}`)
  unwrap(response)
}
