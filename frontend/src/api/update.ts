import api, { type ApiResponse, unwrap } from './index'

export interface UpdateCheckData {
  has_update: boolean
  current_version: string
  latest_version: string
  release_notes: string
  download_url?: string
  download_size?: number
  error?: string
}

export async function checkUpdate(): Promise<UpdateCheckData> {
  const response = await api.get<ApiResponse<UpdateCheckData>>('/admin/update/check')
  return unwrap(response)
}

export async function performUpdate(): Promise<{ message: string; hint: string }> {
  const response = await api.post<ApiResponse<{ message: string; hint: string }>>('/admin/update/perform')
  return unwrap(response)
}

export async function getVersion(): Promise<string> {
  const response = await api.get<ApiResponse<{ version: string }>>('/version')
  const data = unwrap(response)
  return data.version
}
