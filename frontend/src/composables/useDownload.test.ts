import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../api'
import type { Episode } from '../types/anime'
import { useDownload } from './useDownload'

const mocks = vi.hoisted(() => ({
  requestMediaToken: vi.fn(),
  buildDownloadUrl: vi.fn(
    (episodeId: number, token: string) =>
      `/api/episodes/${episodeId}/download?media_token=${encodeURIComponent(token)}`,
  ),
}))

vi.mock('../api/episode', () => ({
  requestMediaToken: mocks.requestMediaToken,
  buildDownloadUrl: mocks.buildDownloadUrl,
}))

function makeEpisode(): Episode {
  return { id: 9, anime_id: 1, ep_number: 3, title: '', file_path: 'ep3.mp4', duration: 0 }
}

describe('useDownload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.requestMediaToken.mockResolvedValue({ token: 'media-9', expires_at: '2026-09-06T00:00:00Z' })
  })

  it('opens the download URL in a new tab and resets downloadingId', async () => {
    const openSpy = vi.spyOn(window, 'open').mockReturnValue({} as WindowProxy)
    const { downloadingId, downloadEpisode } = useDownload()

    const promise = downloadEpisode(makeEpisode())
    expect(downloadingId.value).toBe(9)
    await promise

    expect(mocks.requestMediaToken).toHaveBeenCalledWith(9)
    expect(mocks.buildDownloadUrl).toHaveBeenCalledWith(9, 'media-9')
    expect(openSpy).toHaveBeenCalledWith('/api/episodes/9/download?media_token=media-9', '_blank')
    expect(downloadingId.value).toBeNull()
  })

  it('rethrows ApiError from requestMediaToken and resets downloadingId', async () => {
    vi.spyOn(window, 'open').mockReturnValue({} as WindowProxy)
    mocks.requestMediaToken.mockRejectedValueOnce(new ApiError(1002, '集数不存在'))
    const { downloadingId, downloadEpisode } = useDownload()

    let caught: unknown
    try {
      await downloadEpisode(makeEpisode())
    } catch (error: unknown) {
      caught = error
    }
    expect(caught).toBeInstanceOf(ApiError)
    expect((caught as ApiError).message).toBe('集数不存在')
    expect(downloadingId.value).toBeNull()
  })

  it('throws when the browser blocks the download window', async () => {
    vi.spyOn(window, 'open').mockReturnValue(null)
    const { downloadingId, downloadEpisode } = useDownload()

    await expect(downloadEpisode(makeEpisode())).rejects.toThrow('浏览器阻止了下载窗口')
    expect(downloadingId.value).toBeNull()
  })
})
