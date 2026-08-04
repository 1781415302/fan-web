import { TOKEN_STORAGE_KEY } from './index'

export function getStreamUrl(episodeId: number): string {
  const token = localStorage.getItem(TOKEN_STORAGE_KEY) || ''
  return `/api/episodes/${episodeId}/stream?token=${encodeURIComponent(token)}`
}
