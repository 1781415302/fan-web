import api, { type ApiResponse, unwrap } from './index'

export interface MediaTokenData {
  token: string
  expires_at: string
}

export interface SubtitleTrack {
  track_number: number
  name: string
  language: string
  label: string
}

// requestMediaToken 用登录 JWT（Axios 拦截器自动带）请求当前 episode 的短期媒体票据。
export async function requestMediaToken(episodeId: number): Promise<MediaTokenData> {
  const response = await api.post<ApiResponse<MediaTokenData>>(`/episodes/${episodeId}/media-token`)
  return unwrap(response)
}

// getSubtitleTracks 走 Axios Bearer（登录鉴权）。
export async function getSubtitleTracks(episodeId: number): Promise<SubtitleTrack[]> {
  const response = await api.get<ApiResponse<SubtitleTrack[]>>(`/episodes/${episodeId}/subtitles`)
  return unwrap(response)
}

// getStreamUrl 使用媒体票据；最终 URL 中不出现登录 JWT。
export function getStreamUrl(episodeId: number, mediaToken: string): string {
  return `/api/episodes/${episodeId}/stream?media_token=${encodeURIComponent(mediaToken)}`
}

// getSubtitleUrl 使用同一媒体票据访问 VTT。
export function getSubtitleUrl(episodeId: number, trackNumber: number, mediaToken: string): string {
  return `/api/episodes/${episodeId}/subtitles?track=${trackNumber}&media_token=${encodeURIComponent(mediaToken)}`
}