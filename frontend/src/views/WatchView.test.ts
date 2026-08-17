import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import WatchView from './WatchView.vue'

const mocks = vi.hoisted(() => ({
  route: null as { params: { id: string; epId: string } } | null,
  push: vi.fn(),
  getAnime: vi.fn(),
  listEpisodes: vi.fn(),
  getAnimeProgress: vi.fn(),
  reportProgress: vi.fn(),
  getSubtitleTracks: vi.fn(),
  requestMediaToken: vi.fn(),
  artOptions: [] as Array<Record<string, unknown>>,
  artOn: [] as Array<{ event: string; handler: () => void }>,
  switchQuality: vi.fn(async (_url: string) => undefined),
  lastPlayer: null as {
    currentTime: number
    emit: (event: string) => void
  } | null,
  destroy: vi.fn(),
}))

vi.mock('vue-router', async () => {
  const vue = await vi.importActual<typeof import('vue')>('vue')
  const route = vue.reactive({ params: { id: '1', epId: '3' } })
  mocks.route = route
  return {
    useRoute: () => route,
    useRouter: () => ({ push: mocks.push }),
  }
})

vi.mock('../api/anime', () => ({
  getAnime: mocks.getAnime,
  listEpisodes: mocks.listEpisodes,
}))

vi.mock('../api/progress', () => ({
  getAnimeProgress: mocks.getAnimeProgress,
  reportProgress: mocks.reportProgress,
}))

vi.mock('../api/episode', () => ({
  getStreamUrl: (episodeId: number, token: string) =>
    `/api/episodes/${episodeId}/stream?media_token=${encodeURIComponent(token)}`,
  getSubtitleTracks: mocks.getSubtitleTracks,
  getSubtitleUrl: (episodeId: number, track: number, token: string) =>
    `/api/episodes/${episodeId}/subtitles?track=${track}&media_token=${encodeURIComponent(token)}`,
  requestMediaToken: mocks.requestMediaToken,
}))

vi.mock('artplayer', () => ({
  default: class MockArtplayer {
    playing = false
    currentTime = 0
    duration = 0
    playbackRate = 1
    fullscreen = false
    fullscreenWeb = false
    template = {
      $player: {
        getBoundingClientRect: () => ({ height: 540 }),
      },
    }
    subtitle = {
      switch: vi.fn(async () => undefined),
    }
    private handlers = new Map<string, Array<() => void>>()

    constructor(options: Record<string, unknown>) {
      mocks.artOptions.push(options)
      mocks.lastPlayer = this
    }

    cssVar(_name: string, _value: string) {}
    on(event: string, handler: () => void) {
      mocks.artOn.push({ event, handler })
      const list = this.handlers.get(event) ?? []
      list.push(handler)
      this.handlers.set(event, list)
    }
    emit(event: string) {
      for (const handler of this.handlers.get(event) ?? []) {
        handler()
      }
    }
    async switchQuality(url: string) {
      return mocks.switchQuality(url)
    }
    destroy() {
      mocks.destroy()
    }
  },
}))

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason?: unknown) => void
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise
    reject = rejectPromise
  })
  return { promise, resolve, reject }
}

function mountWatch() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(WatchView, {
    global: {
      plugins: [pinia],
      stubs: { RouterLink: true },
    },
  })
}

describe('WatchView media token flow', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.artOptions.length = 0
    mocks.artOn.length = 0
    mocks.lastPlayer = null
    mocks.route!.params.id = '1'
    mocks.route!.params.epId = '3'
    mocks.getAnime.mockResolvedValue({
      id: 1,
      title: 'Test Anime',
      title_cn: '',
      bangumi_id: 1,
      cover: '',
      summary: '',
      ep_count: 2,
      file_path: '.',
      created_at: '',
    })
    mocks.listEpisodes.mockResolvedValue([
      { id: 3, anime_id: 1, ep_number: 1, title: '', file_path: 'ep1.mp4', duration: 0 },
      { id: 7, anime_id: 1, ep_number: 2, title: '', file_path: 'ep2.mp4', duration: 0 },
    ])
    mocks.getAnimeProgress.mockResolvedValue([])
    mocks.reportProgress.mockResolvedValue(undefined)
    mocks.getSubtitleTracks.mockResolvedValue([])
    mocks.requestMediaToken.mockImplementation(async (episodeId: number) => ({
      token: `media-${episodeId}`,
      expires_at: new Date(Date.now() + 12 * 3600_000).toISOString(),
    }))
  })

  it('creates the player with a fresh token for the selected episode', async () => {
    const wrapper = mountWatch()
    await flushPromises()

    expect(mocks.requestMediaToken).toHaveBeenCalledWith(3)
    expect(mocks.artOptions).toHaveLength(1)
    expect(mocks.artOptions[0]?.url).toBe('/api/episodes/3/stream?media_token=media-3')
    wrapper.unmount()
  })

  it('does not create a player when media-token issuance fails', async () => {
    mocks.requestMediaToken.mockRejectedValueOnce(new Error('ticket failed'))
    const wrapper = mountWatch()
    await flushPromises()

    expect(mocks.artOptions).toHaveLength(0)
    expect(wrapper.text()).toContain('加载观看页失败')
    wrapper.unmount()
  })

  it('ignores a late token response after switching episodes', async () => {
    const first = deferred<{ token: string; expires_at: string }>()
    mocks.requestMediaToken
      .mockImplementationOnce(() => first.promise)
      .mockResolvedValueOnce({ token: 'media-7', expires_at: '2026-08-11T00:00:00Z' })

    const wrapper = mountWatch()
    await nextTick()
    expect(mocks.requestMediaToken).toHaveBeenCalledWith(3)

    mocks.route!.params.epId = '7'
    await nextTick()
    await flushPromises()
    expect(mocks.requestMediaToken).toHaveBeenCalledWith(7)
    expect(mocks.artOptions).toHaveLength(1)
    expect(mocks.artOptions[0]?.url).toBe('/api/episodes/7/stream?media_token=media-7')

    first.resolve({ token: 'late-media-3', expires_at: '2026-08-11T00:00:00Z' })
    await flushPromises()
    expect(mocks.artOptions).toHaveLength(1)
    wrapper.unmount()
  })

  it('records artplayer on() and implements switchQuality', async () => {
    const wrapper = mountWatch()
    await flushPromises()
    expect(mocks.artOn.some((item) => item.event === 'video:error')).toBe(true)
    expect(typeof mocks.lastPlayer?.emit).toBe('function')
    await mocks.lastPlayer?.emit('video:error')
    expect(mocks.switchQuality).toHaveBeenCalledTimes(0)
    wrapper.unmount()
  })

  it('ignores video:error while a token refresh is in flight', async () => {
    const refresh = deferred<{ token: string; expires_at: string }>()
    mocks.requestMediaToken
      .mockResolvedValueOnce({ token: 'media-3', expires_at: new Date(Date.now() + 30_000).toISOString() })
      .mockImplementationOnce(() => refresh.promise)

    const wrapper = mountWatch()
    await flushPromises()
    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(wrapper.text()).not.toContain('视频加载失败')

    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(wrapper.text()).not.toContain('视频加载失败')

    refresh.resolve({ token: 'media-3-next', expires_at: new Date(Date.now() + 12 * 3600_000).toISOString() })
    await flushPromises()
    expect(mocks.switchQuality).toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('视频加载失败')
    wrapper.unmount()
  })

  it('ignores video:error while pendingResumeAfterReload is set', async () => {
    mocks.requestMediaToken
      .mockResolvedValueOnce({ token: 'media-3', expires_at: new Date(Date.now() + 30_000).toISOString() })
      .mockResolvedValueOnce({ token: 'media-3-next', expires_at: new Date(Date.now() + 12 * 3600_000).toISOString() })

    const wrapper = mountWatch()
    await flushPromises()
    mocks.lastPlayer!.currentTime = 40
    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(mocks.switchQuality).toHaveBeenCalled()

    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(wrapper.text()).not.toContain('视频加载失败')
    wrapper.unmount()
  })

  it('resets pending resume when switchQuality fails', async () => {
    mocks.switchQuality.mockRejectedValueOnce(new Error('switch failed'))
    mocks.requestMediaToken
      .mockResolvedValueOnce({ token: 'media-3', expires_at: new Date(Date.now() + 30_000).toISOString() })
      .mockResolvedValueOnce({ token: 'media-3-next', expires_at: new Date(Date.now() + 12 * 3600_000).toISOString() })

    const wrapper = mountWatch()
    await flushPromises()
    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(wrapper.text()).toContain('媒体票据续期失败')

    mocks.requestMediaToken.mockResolvedValueOnce({
      token: 'media-3',
      expires_at: new Date(Date.now() + 12 * 3600_000).toISOString(),
    })
    mocks.lastPlayer?.emit('video:error')
    await flushPromises()
    expect(wrapper.text()).toContain('视频加载失败')
    wrapper.unmount()
  })

  it('skips progress report on destroy when currentTime is under 1s', async () => {
    const wrapper = mountWatch()
    await flushPromises()
    mocks.lastPlayer!.currentTime = 0.4
    wrapper.unmount()
    await flushPromises()
    expect(mocks.reportProgress).not.toHaveBeenCalled()
  })

  it('escapes subtitle track labels only in html assignment', async () => {
    mocks.getSubtitleTracks.mockResolvedValue([
      { track_number: 1, label: 'zh & <b>x</b>' },
    ])
    const wrapper = mountWatch()
    await flushPromises()
    const controls = mocks.artOptions[0]?.controls as Array<{ selector?: Array<{ html?: string }> }>
    const html = controls?.[0]?.selector?.[1]?.html
    expect(html).toBe('zh &amp; &lt;b&gt;x&lt;/b&gt;')
    expect(html).not.toBe('zh & <b>x</b>')
    wrapper.unmount()
  })
})
