import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../api'
import { useAuthStore } from '../stores/auth'
import type { UnidentifiedFile } from '../types/library'
import AnimeListView from './AnimeListView.vue'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('../api/anime', () => ({
  listAnimes: vi.fn(),
  createAnime: vi.fn(),
  scanAnime: vi.fn(),
}))
vi.mock('../api/library', () => ({
  scanLibrary: vi.fn(),
}))

function unidentifiedFile(
  overrides: Partial<UnidentifiedFile> & Pick<UnidentifiedFile, 'file_name' | 'reason'>,
): UnidentifiedFile {
  return {
    file_path: 'dir',
    candidates: [],
    ...overrides,
  }
}

function mountList(isAdmin: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  if (isAdmin) {
    const store = useAuthStore()
    store.setSession('tok', { id: 1, username: 'u', is_admin: true, created_at: '' })
  }
  return mount(AnimeListView, { global: { plugins: [pinia] } })
}

async function mockedAnimeApi() {
  return import('../api/anime')
}

async function mockedLibraryApi() {
  return import('../api/library')
}

function asMock(fn: unknown) {
  return fn as unknown as ReturnType<typeof vi.fn>
}

const sampleAnime = {
  id: 42,
  title: 'Show',
  title_cn: '番剧',
  bangumi_id: 101,
  cover: '',
  summary: '',
  ep_count: 12,
  file_path: 'ShowDir',
  created_at: '',
}

const sharedCandidates = [
  { id: 101, name: 'Show', name_cn: '候选番剧', score: 0.91 },
  { id: 102, name: 'Other', name_cn: '', score: 0.55 },
]

function threeSiblingFiles(): UnidentifiedFile[] {
  return [
    unidentifiedFile({
      file_name: 'ep01.mkv',
      reason: 'ambiguous',
      file_path: 'ShowDir',
      candidates: sharedCandidates,
    }),
    unidentifiedFile({
      file_name: 'ep02.mkv',
      reason: 'ambiguous',
      file_path: 'ShowDir',
      candidates: sharedCandidates,
    }),
    unidentifiedFile({
      file_name: 'ep03.mkv',
      reason: 'ambiguous',
      file_path: 'ShowDir',
      candidates: sharedCandidates,
    }),
  ]
}

async function scanAsAdmin(unidentified: UnidentifiedFile[]) {
  const { scanLibrary } = await mockedLibraryApi()
  asMock(scanLibrary).mockResolvedValue({
    total_files: unidentified.length,
    skipped: 0,
    new_animes: 0,
    new_episodes: 0,
    unidentified,
  })
  const wrapper = mountList(true)
  await flushPromises()
  const scanBtn = wrapper.findAll('button').find((btn) => btn.text().includes('库扫描'))
  expect(scanBtn).toBeTruthy()
  await scanBtn!.trigger('click')
  await flushPromises()
  return wrapper
}

describe('AnimeListView admin controls', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    asMock(listAnimes).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    asMock(createAnime).mockResolvedValue(sampleAnime)
    asMock(scanAnime).mockResolvedValue({ scanned: 0, episodes: [] })
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

describe('AnimeListView unidentified confirm', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    asMock(listAnimes).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    asMock(createAnime).mockResolvedValue(sampleAnime)
    asMock(scanAnime).mockResolvedValue({ scanned: 3, episodes: [] })
  })

  it('creates then scans and dismisses sibling rows with the same file_path', async () => {
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('ep02.mkv')
    expect(wrapper.text()).toContain('ep03.mkv')
    expect(wrapper.findAll('.candidate-btn').length).toBeGreaterThan(0)

    const callsBeforeConfirm = asMock(listAnimes).mock.calls.length
    const pick = wrapper.findAll('.candidate-btn').find((btn) => btn.text().includes('候选番剧'))
    expect(pick).toBeTruthy()
    await pick!.trigger('click')
    await flushPromises()

    expect(createAnime).toHaveBeenCalledTimes(1)
    expect(createAnime).toHaveBeenCalledWith(101, 'ShowDir')
    expect(scanAnime).toHaveBeenCalledTimes(1)
    expect(scanAnime).toHaveBeenCalledWith(42)
    expect(asMock(createAnime).mock.invocationCallOrder[0]).toBeLessThan(
      asMock(scanAnime).mock.invocationCallOrder[0],
    )
    expect(wrapper.text()).not.toContain('ep01.mkv')
    expect(wrapper.text()).not.toContain('ep02.mkv')
    expect(wrapper.text()).not.toContain('ep03.mkv')
    expect(asMock(listAnimes).mock.calls.length).toBeGreaterThan(callsBeforeConfirm)
    wrapper.unmount()
  })

  it('shows ApiError 1001 and does not scan when createAnime fails', async () => {
    const { createAnime, scanAnime } = await mockedAnimeApi()
    asMock(createAnime).mockRejectedValue(new ApiError(1001, '番剧已存在'))
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    const pick = wrapper.findAll('.candidate-btn').find((btn) => btn.text().includes('候选番剧'))
    await pick!.trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('番剧已存在')
    expect(scanAnime).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('ep02.mkv')
    expect(wrapper.text()).toContain('ep03.mkv')
    wrapper.unmount()
  })

  it('does not show pick buttons when candidates is empty', async () => {
    const wrapper = await scanAsAdmin([
      unidentifiedFile({
        file_name: 'lonely.mkv',
        reason: 'no match',
        file_path: 'EmptyDir',
        candidates: [],
      }),
    ])

    expect(wrapper.text()).toContain('lonely.mkv')
    expect(wrapper.text()).toContain('no match')
    expect(wrapper.findAll('.candidate-btn')).toHaveLength(0)
    wrapper.unmount()
  })

  it('passes empty file_path through to createAnime', async () => {
    const { createAnime, scanAnime } = await mockedAnimeApi()
    const wrapper = await scanAsAdmin([
      unidentifiedFile({
        file_name: 'root.mkv',
        reason: 'ambiguous',
        file_path: '',
        candidates: [{ id: 7, name: 'Root Show', name_cn: '根目录番', score: 0.88 }],
      }),
    ])

    const pick = wrapper.findAll('.candidate-btn').find((btn) => btn.text().includes('根目录番'))
    expect(pick).toBeTruthy()
    await pick!.trigger('click')
    await flushPromises()

    expect(createAnime).toHaveBeenCalledWith(7, '')
    expect(scanAnime).toHaveBeenCalledWith(42)
    expect(wrapper.text()).not.toContain('root.mkv')
    wrapper.unmount()
  })

  it('disables sibling candidate buttons for the same file_path while confirm is in flight', async () => {
    const { createAnime, scanAnime } = await mockedAnimeApi()
    let resolveCreate!: (value: typeof sampleAnime) => void
    asMock(createAnime).mockReturnValue(
      new Promise((resolve) => {
        resolveCreate = resolve
      }),
    )
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    const pick = wrapper.findAll('.candidate-btn').find((btn) => btn.text().includes('候选番剧'))
    await pick!.trigger('click')
    await flushPromises()

    const buttons = wrapper.findAll('.candidate-btn')
    expect(buttons.length).toBeGreaterThan(1)
    expect(buttons.every((btn) => btn.attributes('disabled') !== undefined)).toBe(true)
    expect(scanAnime).not.toHaveBeenCalled()

    resolveCreate(sampleAnime)
    await flushPromises()
    wrapper.unmount()
  })
})
