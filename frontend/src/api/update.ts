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

// 服务端在请求内同步下载整包并做 SHA256 校验后才返回（下载客户端超时 120s），
// 数十 MB 安装包在慢速链路下必然超过全局 10s 超时，这里单独覆盖为长超时，
// 否则 axios 会以 ECONNABORTED 中止并造成"假失败"。
const UPDATE_TIMEOUT_MS = 300_000

export async function performUpdate(): Promise<{ message: string; hint: string }> {
  const response = await api.post<ApiResponse<{ message: string; hint: string }>>(
    '/admin/update/perform',
    undefined,
    { timeout: UPDATE_TIMEOUT_MS },
  )
  return unwrap(response)
}

export async function getVersion(): Promise<string> {
  const response = await api.get<ApiResponse<{ version: string }>>('/version')
  const data = unwrap(response)
  return data.version
}
