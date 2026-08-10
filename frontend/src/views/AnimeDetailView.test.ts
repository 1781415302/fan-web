import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AnimeDetailView from './AnimeDetailView.vue'
import { useAuthStore } from '../stores/auth'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '1' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('../api/anime', () => ({
  getAnime: vi.fn(),
  listEpisodes: vi.fn(),
  scanAnime: vi.fn(),
  updateAnime: vi.fn(),
  deleteAnime: vi.fn(),
}))
vi.mock('../api/progress', () => ({
  getAnimeProgress: vi.fn(),
}))

function mountDetail(isAdmin: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  const store = useAuthStore()
  store.setSession('tok', { id: 1, username: 'u', is_admin: isAdmin, created_at: '' })
  return mount(AnimeDetailView, { global: { plugins: [pinia] } })
}

describe('AnimeDetailView admin controls', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { getAnime, listEpisodes } = await import('../api/anime')
    const { getAnimeProgress } = await import('../api/progress')
    ;(getAnime as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({
      id: 1,
      title: 'Test Anime',
      title_cn: '',
      bangumi_id: 1,
      cover: '',
      summary: 'summary',
      ep_count: 1,
      file_path: 'dir',
      created_at: '',
    })
    ;(listEpisodes as unknown as ReturnType<typeof vi.fn>).mockResolvedValue([])
    ;(getAnimeProgress as unknown as ReturnType<typeof vi.fn>).mockResolvedValue([])
  })

  it('hides edit/scan/delete for ordinary users', async () => {
    const wrapper = mountDetail(false)
    await flushPromises()
    expect(wrapper.text()).not.toContain('编辑信息')
    expect(wrapper.text()).not.toContain('删除番剧')
    wrapper.unmount()
  })

  it('shows edit/scan/delete for admins', async () => {
    const wrapper = mountDetail(true)
    await flushPromises()
    expect(wrapper.text()).toContain('编辑信息')
    expect(wrapper.text()).toContain('删除番剧')
    wrapper.unmount()
  })
})