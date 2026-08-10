import { beforeEach, describe, expect, it, vi } from 'vitest'

const issued = vi.fn()

vi.mock('../api/episode', () => ({
  getStreamUrl: (epId: number, token: string) => `/api/episodes/${epId}/stream?media_token=${encodeURIComponent(token)}`,
  getSubtitleTracks: vi.fn().mockResolvedValue([]),
  getSubtitleUrl: (epId: number, track: number, token: string) =>
    `/api/episodes/${epId}/subtitles?track=${track}&media_token=${encodeURIComponent(token)}`,
  requestMediaToken: (epId: number) => {
    issued(epId)
    return Promise.resolve({ token: `media-${epId}`, expires_at: 'future' })
  },
}))

import { getStreamUrl, getSubtitleUrl, requestMediaToken } from '../api/episode'

describe('WatchView media token semantics', () => {
  beforeEach(() => {
    issued.mockClear()
  })

  it('issues a fresh per-episode media token', async () => {
    await requestMediaToken(3)
    await requestMediaToken(7)
    expect(issued).toHaveBeenCalledTimes(2)
    expect(issued).toHaveBeenNthCalledWith(1, 3)
    expect(issued).toHaveBeenNthCalledWith(2, 7)
  })

  it('stream URL carries only the media token for the episode', () => {
    const url = getStreamUrl(7, 'media-7')
    expect(url).toBe('/api/episodes/7/stream?media_token=media-7')
  })

  it('subtitle URL reuses the same episode media token', () => {
    const url = getSubtitleUrl(7, 3, 'media-7')
    expect(url).toContain('media_token=media-7')
    expect(url).toContain('track=3')
  })

  it('an episode B token never appears in episode A URL', () => {
    const urlA = getStreamUrl(7, 'media-7')
    expect(urlA).not.toContain('media-3')
  })
})