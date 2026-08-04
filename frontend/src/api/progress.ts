import api, { type ApiResponse, unwrap } from './index'
import type { AnimeProgress, EpisodeProgress } from '../types/progress'

export async function getProgress(episodeId: number): Promise<EpisodeProgress> {
  const response = await api.get<ApiResponse<EpisodeProgress>>(`/progress/${episodeId}`)
  return unwrap(response)
}

export async function reportProgress(
  episodeId: number,
  position: number,
  watched: boolean,
): Promise<void> {
  const response = await api.post<ApiResponse<null>>(`/progress/${episodeId}`, {
    position,
    watched,
  })
  unwrap(response)
}

export async function getAnimeProgress(animeId: number): Promise<AnimeProgress[]> {
  const response = await api.get<ApiResponse<AnimeProgress[]>>(`/progress/anime/${animeId}`)
  return unwrap(response)
}
