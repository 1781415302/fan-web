import api, { type ApiResponse, unwrap } from './index'
import type { BangumiSearchItem } from '../types/anime'

export async function searchBangumi(keyword: string): Promise<BangumiSearchItem[]> {
  const response = await api.get<ApiResponse<BangumiSearchItem[]>>('/bangumi/search', {
    params: { keyword },
  })
  return unwrap(response)
}
