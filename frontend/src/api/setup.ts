import api, { type ApiResponse, unwrap } from '.'
import type { LoginData } from '../types/auth'

export interface SetupSubmitData {
  username: string
  password: string
  videoRootPath: string
  port?: number
}

export async function getSetupStatus(): Promise<boolean> {
  const response = await api.get<ApiResponse<{ configured: boolean }>>('/setup/status')
  const data = unwrap(response)
  return data.configured
}

export async function submitSetup(payload: SetupSubmitData): Promise<LoginData> {
  const response = await api.post<ApiResponse<LoginData>>('/setup', {
    admin_username: payload.username,
    admin_password: payload.password,
    video_root_path: payload.videoRootPath,
    port: payload.port,
  })
  return unwrap(response)
}