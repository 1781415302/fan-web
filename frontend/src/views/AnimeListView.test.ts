import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../api'
import { useAuthStore } from '../stores/auth'
import type { ScanJob, UnidentifiedFile } from '../types/library'
import AnimeListView from './AnimeListView.vue'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn() }),
}))

vi.mock('../api/anime', () => ({
  listAnimes: vi.fn(),
  createAnime: vi.fn(),
  scanAnime: vi.fn(),
}))
vi.mock('../api/library', async (importOriginal) => {
  // 保留真实的 ConfirmInboxError（视图用 instanceof 区分 reason），只替换网络函数。
  const actual = await importOriginal<typeof import('../api/library')>()
  return {
    ...actual,
    startLibraryScan: vi.fn(),
    getLibraryScan: vi.fn(),
    listUnidentified: vi.fn(),
    confirmInbox: vi.fn(),
  }
})
vi.mock('../api/bangumi', () => ({
  searchBangumi: vi.fn(),
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

async function mockedBangumiApi() {
  return import('../api/bangumi')
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

function doneJob(unidentified: UnidentifiedFile[]): ScanJob {
  return {
    state: 'done',
    result: {
      total_files: unidentified.length,
      skipped: 0,
      new_animes: 0,
      new_episodes: 0,
      unidentified,
    },
  }
}

async function mockUnidentifiedPage(unidentified: UnidentifiedFile[]) {
  const { listUnidentified } = await mockedLibraryApi()
  asMock(listUnidentified).mockResolvedValue({
    items: unidentified,
    total: unidentified.length,
    page: 1,
    page_size: 100,
  })
}

async function scanAsAdmin(unidentified: UnidentifiedFile[]) {
  const { startLibraryScan, getLibraryScan } = await mockedLibraryApi()
  asMock(startLibraryScan).mockResolvedValue({ state: 'running' })
  asMock(getLibraryScan).mockResolvedValue(doneJob(unidentified))
  const wrapper = mountList(true)
  await flushPromises()
  await mockUnidentifiedPage(unidentified)
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
    await mockUnidentifiedPage([])
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

describe('AnimeListView scan polling', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    asMock(listAnimes).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    asMock(createAnime).mockResolvedValue(sampleAnime)
    asMock(scanAnime).mockResolvedValue({ scanned: 0, episodes: [] })
    await mockUnidentifiedPage([])
  })

  it('renders unidentified only after GET scan returns done', async () => {
    const files = threeSiblingFiles()
    const { startLibraryScan, getLibraryScan, listUnidentified } = await mockedLibraryApi()
    asMock(startLibraryScan).mockResolvedValue({ state: 'running' })
    let resolveGet!: (job: ScanJob) => void
    asMock(getLibraryScan).mockReturnValue(
      new Promise((resolve) => {
        resolveGet = resolve
      }),
    )
    asMock(listUnidentified).mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      page_size: 100,
    })

    const wrapper = mountList(true)
    await flushPromises()
    await mockUnidentifiedPage(files)
    const scanBtn = wrapper.findAll('button').find((btn) => btn.text().includes('库扫描'))
    expect(scanBtn).toBeTruthy()
    await scanBtn!.trigger('click')
    await flushPromises()

    expect(startLibraryScan).toHaveBeenCalledTimes(1)
    expect(getLibraryScan).toHaveBeenCalledTimes(1)
    const callsWhileRunning = asMock(listUnidentified).mock.calls.length
    expect(wrapper.text()).not.toContain('ep01.mkv')

    resolveGet(doneJob(files))
    await flushPromises()

    expect(asMock(listUnidentified).mock.calls.length).toBeGreaterThan(callsWhileRunning)
    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('ep02.mkv')
    expect(wrapper.text()).toContain('ep03.mkv')
    expect(wrapper.text()).toContain('无法识别')
    wrapper.unmount()
  })

  it('loads persisted unidentified on admin enter without scanning', async () => {
    const files = threeSiblingFiles()
    await mockUnidentifiedPage(files)
    const wrapper = mountList(true)
    await flushPromises()
    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('无法识别文件')
    expect(wrapper.text()).toContain('（3）')
    wrapper.unmount()
  })
})

const confirmSuccess = {
  anime: sampleAnime,
  created: true,
  scanned: 3,
  episodes: [],
}

function cardFor(wrapper: ReturnType<typeof mountList>, fileName: string) {
  const card = wrapper.findAll('.inbox-card').find((item) => item.find('code').text() === fileName)
  expect(card).toBeTruthy()
  return card!
}

function rowFor(card: ReturnType<typeof cardFor>, label: string) {
  const row = card.findAll('.candidate-row').find((item) => item.text().includes(label))
  expect(row).toBeTruthy()
  return row!
}

function lastConfirmRequest(confirmInbox: unknown) {
  const calls = asMock(confirmInbox).mock.calls
  expect(calls.length).toBeGreaterThan(0)
  return calls[calls.length - 1][0]
}

describe('AnimeListView unidentified confirm', () => {
  beforeEach(async () => {
    window.localStorage.clear()
    vi.clearAllMocks()
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    asMock(listAnimes).mockResolvedValue({ items: [], total: 0, page: 1, page_size: 20 })
    asMock(createAnime).mockResolvedValue(sampleAnime)
    asMock(scanAnime).mockResolvedValue({ scanned: 3, episodes: [] })
    const { confirmInbox } = await mockedLibraryApi()
    asMock(confirmInbox).mockResolvedValue(confirmSuccess)
    await mockUnidentifiedPage([])
  })

  it('submits all same-dir files whose candidates contain the id via confirmInbox and dismisses them', async () => {
    const { listAnimes, createAnime, scanAnime } = await mockedAnimeApi()
    const { confirmInbox } = await mockedLibraryApi()
    const wrapper = await scanAsAdmin([
      ...threeSiblingFiles(),
      unidentifiedFile({
        file_name: 'other-dir.mkv',
        reason: 'ambiguous',
        file_path: 'OtherDir',
        candidates: sharedCandidates,
      }),
      unidentifiedFile({
        file_name: 'unrelated.mkv',
        reason: 'ambiguous',
        file_path: 'ShowDir',
        candidates: [{ id: 555, name: 'Unrelated', name_cn: '无关', score: 0.6 }],
      }),
    ])

    const row = rowFor(cardFor(wrapper, 'ep01.mkv'), '候选番剧')
    const batchBtn = row.find('.candidate-btn')
    expect(batchBtn.text()).toBe('归入此番剧（同目录 3 个文件）')

    const callsBeforeConfirm = asMock(listAnimes).mock.calls.length
    await batchBtn.trigger('click')
    await flushPromises()

    expect(confirmInbox).toHaveBeenCalledTimes(1)
    expect(confirmInbox).toHaveBeenCalledWith({
      bangumi_id: 101,
      file_path: 'ShowDir',
      file_names: ['ep01.mkv', 'ep02.mkv', 'ep03.mkv'],
    })
    expect(createAnime).not.toHaveBeenCalled()
    expect(scanAnime).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('ep01.mkv')
    expect(wrapper.text()).not.toContain('ep02.mkv')
    expect(wrapper.text()).not.toContain('ep03.mkv')
    expect(wrapper.text()).toContain('other-dir.mkv')
    expect(wrapper.text()).toContain('unrelated.mkv')
    expect(asMock(listAnimes).mock.calls.length).toBeGreaterThan(callsBeforeConfirm)
    wrapper.unmount()
  })

  it('"仅此文件" submits only the clicked file and keeps its siblings', async () => {
    const { confirmInbox } = await mockedLibraryApi()
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    const row = rowFor(cardFor(wrapper, 'ep02.mkv'), '候选番剧')
    const singleBtn = row.find('.candidate-single-btn')
    expect(singleBtn.text()).toBe('仅此文件')
    await singleBtn.trigger('click')
    await flushPromises()

    expect(confirmInbox).toHaveBeenCalledWith({
      bangumi_id: 101,
      file_path: 'ShowDir',
      file_names: ['ep02.mkv'],
    })
    expect(wrapper.text()).not.toContain('ep02.mkv')
    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('ep03.mkv')
    wrapper.unmount()
  })

  it('renders confirm failure inline in the clicked card and never falls back to createAnime/scanAnime', async () => {
    const { createAnime, scanAnime } = await mockedAnimeApi()
    const { confirmInbox, ConfirmInboxError } = await mockedLibraryApi()
    asMock(confirmInbox).mockRejectedValue(
      new ConfirmInboxError(9999, 'review_required', '匹配需要人工确认: 全新目标没有可关联的文件'),
    )
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    const card = cardFor(wrapper, 'ep01.mkv')
    await rowFor(card, '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()

    const inline = cardFor(wrapper, 'ep01.mkv').find('.inbox-card-error')
    expect(inline.exists()).toBe(true)
    expect(inline.text()).toContain('匹配需要人工确认: 全新目标没有可关联的文件')
    expect(cardFor(wrapper, 'ep02.mkv').find('.inbox-card-error').exists()).toBe(false)
    expect(cardFor(wrapper, 'ep03.mkv').find('.inbox-card-error').exists()).toBe(false)
    // 页面底部的 scanError 不再承载确认错误。
    expect(wrapper.findAll('p.error-msg')).toHaveLength(0)
    expect(createAnime).not.toHaveBeenCalled()
    expect(scanAnime).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('ep01.mkv')
    expect(wrapper.text()).toContain('ep02.mkv')
    expect(wrapper.text()).toContain('ep03.mkv')
    // review_required 后展开偏移输入与手动搜索入口。
    expect(cardFor(wrapper, 'ep01.mkv').find('.offset-input').exists()).toBe(true)
    expect(cardFor(wrapper, 'ep01.mkv').find('.search-toggle-btn').text()).toBe('手动搜索条目')
    wrapper.unmount()
  })

  it('shows no_match missing file names inline', async () => {
    const { confirmInbox, ConfirmInboxError } = await mockedLibraryApi()
    asMock(confirmInbox).mockRejectedValue(
      new ConfirmInboxError(9999, 'no_match', '这些文件未能归入该条目：ep03.mkv', ['ep03.mkv']),
    )
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    await rowFor(cardFor(wrapper, 'ep01.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()

    const inline = cardFor(wrapper, 'ep01.mkv').find('.inbox-card-error')
    expect(inline.text()).toContain('这些文件未能归入该条目：ep03.mkv')
    expect(inline.findAll('.inbox-card-error-files li').map((li) => li.text())).toEqual(['ep03.mkv'])
    // no_match 不自动展开偏移输入。
    expect(cardFor(wrapper, 'ep01.mkv').find('.offset-input').exists()).toBe(false)
    expect(wrapper.text()).toContain('ep03.mkv')
    wrapper.unmount()
  })

  it('renders non-confirm errors (e.g. ApiError) inline as well', async () => {
    const { confirmInbox } = await mockedLibraryApi()
    asMock(confirmInbox).mockRejectedValue(new ApiError(-1, '响应格式错误：未返回有效的 ApiResponse'))
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    await rowFor(cardFor(wrapper, 'ep02.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()

    expect(cardFor(wrapper, 'ep02.mkv').find('.inbox-card-error').text()).toContain('响应格式错误')
    expect(wrapper.findAll('p.error-msg')).toHaveLength(0)
    wrapper.unmount()
  })

  it('passes the episode offset after a review_required failure', async () => {
    const { confirmInbox, ConfirmInboxError } = await mockedLibraryApi()
    asMock(confirmInbox).mockRejectedValueOnce(
      new ConfirmInboxError(9999, 'review_required', '匹配需要人工确认: 章节冲突'),
    )
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    await rowFor(cardFor(wrapper, 'ep01.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()
    expect(lastConfirmRequest(confirmInbox)).not.toHaveProperty('episode_offset')

    await cardFor(wrapper, 'ep01.mkv').find('.offset-input').setValue('77')
    await rowFor(cardFor(wrapper, 'ep01.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()

    expect(confirmInbox).toHaveBeenCalledTimes(2)
    expect(lastConfirmRequest(confirmInbox)).toEqual({
      bangumi_id: 101,
      file_path: 'ShowDir',
      file_names: ['ep01.mkv', 'ep02.mkv', 'ep03.mkv'],
      episode_offset: 77,
    })
    expect(wrapper.text()).not.toContain('ep01.mkv')
    wrapper.unmount()
  })

  it('confirms a manually searched Bangumi subject', async () => {
    const { confirmInbox } = await mockedLibraryApi()
    const { searchBangumi } = await mockedBangumiApi()
    asMock(searchBangumi).mockResolvedValue([
      { id: 633836, name: 'Re:Zero 4th', name_cn: '从零开始的异世界生活 第四季', summary: '', eps_count: 8, cover: '' },
    ])
    const wrapper = await scanAsAdmin([
      unidentifiedFile({
        file_name: 'rezero-19.mp4',
        reason: 'no match',
        file_path: 'Re0/Season4',
        candidates: [],
      }),
      unidentifiedFile({
        file_name: 'rezero-20.mp4',
        reason: 'no match',
        file_path: 'Re0/Season4',
        candidates: [],
      }),
    ])

    const card = cardFor(wrapper, 'rezero-19.mp4')
    await card.find('.search-toggle-btn').trigger('click')
    await card.find('.manual-search-input').setValue('Re Zero')
    await card.find('.manual-search-form').trigger('submit')
    await flushPromises()

    expect(searchBangumi).toHaveBeenCalledWith('Re Zero')
    const result = cardFor(wrapper, 'rezero-19.mp4').find('.manual-result-row')
    expect(result.text()).toContain('从零开始的异世界生活 第四季')
    expect(result.text()).toContain('8话')
    // 搜索结果不在任何候选里：默认批量只含本文件。
    expect(result.find('.manual-pick-btn').text()).toBe('归入此番剧（同目录 1 个文件）')
    await result.find('.manual-pick-btn').trigger('click')
    await flushPromises()

    expect(confirmInbox).toHaveBeenCalledWith({
      bangumi_id: 633836,
      file_path: 'Re0/Season4',
      file_names: ['rezero-19.mp4'],
    })
    expect(wrapper.text()).not.toContain('rezero-19.mp4')
    expect(wrapper.text()).toContain('rezero-20.mp4')
    wrapper.unmount()
  })

  it('shows candidate year / ep_count and degrades when the fields are missing', async () => {
    const wrapper = await scanAsAdmin([
      unidentifiedFile({
        file_name: 'meta.mkv',
        reason: 'ambiguous',
        file_path: 'MetaDir',
        candidates: [
          { id: 547888, name: 'Re:Zero 3rd', name_cn: '丧失篇', score: 1, year: 2024, ep_count: 16 },
          { id: 633836, name: 'Re:Zero 4th', name_cn: '夺还篇', score: 1 },
          { id: 9, name: 'Legacy', name_cn: '', score: 0.5, year: 0, ep_count: 0 },
        ],
      }),
    ])

    const card = cardFor(wrapper, 'meta.mkv')
    expect(rowFor(card, '丧失篇').find('.candidate-meta').text()).toBe('2024年 · 16话')
    expect(rowFor(card, '夺还篇').find('.candidate-meta').exists()).toBe(false)
    expect(rowFor(card, 'Legacy').find('.candidate-meta').exists()).toBe(false)
    expect(card.text()).not.toContain('undefined')
    expect(card.text()).not.toContain('0话')
    expect(rowFor(card, '夺还篇').text()).toContain('1.00')
    wrapper.unmount()
  })

  it('does not show pick buttons when candidates is empty but keeps manual search', async () => {
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
    expect(cardFor(wrapper, 'lonely.mkv').find('.search-toggle-btn').exists()).toBe(true)
    wrapper.unmount()
  })

  it('passes empty file_path through to confirmInbox', async () => {
    const { confirmInbox } = await mockedLibraryApi()
    const wrapper = await scanAsAdmin([
      unidentifiedFile({
        file_name: 'root.mkv',
        reason: 'ambiguous',
        file_path: '',
        candidates: [{ id: 7, name: 'Root Show', name_cn: '根目录番', score: 0.88 }],
      }),
    ])

    await rowFor(cardFor(wrapper, 'root.mkv'), '根目录番').find('.candidate-btn').trigger('click')
    await flushPromises()

    expect(confirmInbox).toHaveBeenCalledWith({ bangumi_id: 7, file_path: '', file_names: ['root.mkv'] })
    expect(wrapper.text()).not.toContain('root.mkv')
    wrapper.unmount()
  })

  it('disables sibling candidate buttons for the same file_path while confirm is in flight', async () => {
    const { confirmInbox } = await mockedLibraryApi()
    let resolveConfirm!: (value: typeof confirmSuccess) => void
    asMock(confirmInbox).mockReturnValue(
      new Promise((resolve) => {
        resolveConfirm = resolve
      }),
    )
    const wrapper = await scanAsAdmin(threeSiblingFiles())

    await rowFor(cardFor(wrapper, 'ep01.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    await flushPromises()

    const buttons = [...wrapper.findAll('.candidate-btn'), ...wrapper.findAll('.candidate-single-btn')]
    expect(buttons.length).toBeGreaterThan(1)
    expect(buttons.every((btn) => btn.attributes('disabled') !== undefined)).toBe(true)
    // 再次点击不会发起第二个请求。
    await rowFor(cardFor(wrapper, 'ep02.mkv'), '候选番剧').find('.candidate-btn').trigger('click')
    expect(confirmInbox).toHaveBeenCalledTimes(1)

    resolveConfirm(confirmSuccess)
    await flushPromises()
    wrapper.unmount()
  })
})
