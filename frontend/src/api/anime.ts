import api, { type ApiResponse, unwrap } from './index'
import type { Anime, Episode, PaginatedAnimes, ScanResult } from '../types/anime'

export async function listAnimes(
  page: number,
  pageSize: number,
  keyword: string,
): Promise<PaginatedAnimes> {
  const response = await api.get<ApiResponse<PaginatedAnimes>>('/animes', {
    params: { page, page_size: pageSize, keyword: keyword || undefined },
  })
  return unwrap(response)
}

export async function getAnime(id: number): Promise<Anime> {
  const response = await api.get<ApiResponse<Anime>>(`/animes/${id}`)
  return unwrap(response)
}

export async function createAnime(bangumiId: number, filePath: string): Promise<Anime> {
  const response = await api.post<ApiResponse<Anime>>('/animes', {
    bangumi_id: bangumiId,
    file_path: filePath,
  })
  return unwrap(response)
}

export async function updateAnime(
  id: number,
  data: {
    title: string
    title_cn: string
    summary: string
    ep_count: number
    file_path: string
  },
): Promise<void> {
  const response = await api.put<ApiResponse<null>>(`/animes/${id}`, data)
  unwrap(response)
}

export async function deleteAnime(id: number): Promise<void> {
  const response = await api.delete<ApiResponse<null>>(`/animes/${id}`)
  unwrap(response)
}

// 单番扫描仍同步执行，可保留 600s 超时；全库扫描已改作业，不再走这条路径。
const SCAN_TIMEOUT_MS = 600_000

export async function scanAnime(id: number): Promise<ScanResult> {
  const response = await api.post<ApiResponse<ScanResult>>(`/animes/${id}/scan`, undefined, {
    timeout: SCAN_TIMEOUT_MS,
  })
  return unwrap(response)
}

export async function rebindAnime(id: number, bangumiId: number): Promise<Anime> {
  const response = await api.post<ApiResponse<Anime>>(`/animes/${id}/rebind`, {
    bangumi_id: bangumiId,
  })
  return unwrap(response)
}

export async function listEpisodes(animeId: number): Promise<Episode[]> {
  const response = await api.get<ApiResponse<Episode[]>>(`/animes/${animeId}/episodes`)
  return unwrap(response)
}
