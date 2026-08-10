import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AnimeListView from './AnimeListView.vue'
import { useAuthStore } from '../stores/auth'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('../api/anime', () => ({
  listAnimes: vi.fn(),
}))
vi.mock('../api/library', () => ({
  scanLibrary: vi.fn(),
}))
function mountList(isAdmin: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  if (isAdmin) {
    const store = useAuthStore()
    store.setSession('tok', { id: 1, username: 'u', is_admin: true, created_at: '' })
  }
  return mount(AnimeListView, { global: { plugins: [pinia] } })
}

describe('AnimeListView admin controls', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { listAnimes } = await import('../api/anime')
    ;(listAnimes as unknown as ReturnType<typeof vi.fn>).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
  })

  it('hides library-scan and add buttons for ordinary users', async () => {
    const wrapper = mountList(false)
    await flushPromises()
    expect(wrapper.text()).not.toContain('库扫描')
    expect(wrapper.text()).not.toContain('添加番剧')
    wrapper.unmount()
  })

  it('shows library-scan and add buttons for admins', async () => {
    const wrapper = mountList(true)
    await flushPromises()
    expect(wrapper.text()).toContain('库扫描')
    expect(wrapper.text()).toContain('添加番剧')
    wrapper.unmount()
  })
})