import api, { type ApiResponse, unwrap } from './index'
import type { BangumiSearchItem } from '../types/anime'

export interface BangumiLink {
  linked: boolean
  suffix?: string
}

export interface BangumiSyncResult {
  animes: number
  episodes_marked: number
}

export async function searchBangumi(keyword: string): Promise<BangumiSearchItem[]> {
  const response = await api.get<ApiResponse<BangumiSearchItem[]>>('/bangumi/search', {
    params: { keyword },
  })
  return unwrap(response)
}

export async function getBangumiLink(): Promise<BangumiLink> {
  const response = await api.get<ApiResponse<BangumiLink>>('/me/bangumi')
  return unwrap(response)
}

export async function putBangumiToken(accessToken: string): Promise<BangumiLink> {
  const response = await api.put<ApiResponse<BangumiLink>>('/me/bangumi', {
    access_token: accessToken,
  })
  return unwrap(response)
}

export async function deleteBangumiToken(): Promise<BangumiLink> {
  const response = await api.delete<ApiResponse<BangumiLink>>('/me/bangumi')
  return unwrap(response)
}

// 入站同步会逐部拉取 Bangumi 章节收藏，库较大时远超全局 10s。
// 契约要求本接口单独 120s，禁止走默认超时。
const BANGUMI_SYNC_TIMEOUT_MS = 120_000

export async function syncBangumi(): Promise<BangumiSyncResult> {
  const response = await api.post<ApiResponse<BangumiSyncResult>>(
    '/me/bangumi/sync',
    undefined,
    { timeout: BANGUMI_SYNC_TIMEOUT_MS },
  )
  return unwrap(response)
}
