import api, { TOKEN_STORAGE_KEY, type ApiResponse, unwrap } from './index'

export function getStreamUrl(episodeId: number): string {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY) || ''
  return `/api/episodes/${episodeId}/stream?token=${encodeURIComponent(token)}`
}

export interface SubtitleTrack {
  track_number: number
  name: string
  language: string
  label: string
}

export async function getSubtitleTracks(episodeId: number): Promise<SubtitleTrack[]> {
  const response = await api.get<ApiResponse<SubtitleTrack[]>>(
    `/episodes/${episodeId}/subtitles`,
  )
  return unwrap(response)
}

export function getSubtitleUrl(episodeId: number, trackNumber: number): string {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY) || ''
  return `/api/episodes/${episodeId}/subtitles?track=${trackNumber}&token=${encodeURIComponent(token)}`
}
