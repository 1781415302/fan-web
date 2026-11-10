import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import api, { ApiError } from '../api'
import { CONFIRM_INBOX_TIMEOUT_MS, ConfirmInboxError, confirmInbox } from '../api/library'
import type { UnidentifiedFile } from '../types/library'
import InboxFileCard from './InboxFileCard.vue'

vi.mock('../api/bangumi', () => ({
  searchBangumi: vi.fn(),
}))

function asMock(fn: unknown) {
  return fn as unknown as ReturnType<typeof vi.fn>
}

function envelope(data: unknown) {
  return { data, status: 200, statusText: 'OK', headers: {}, config: {} }
}

const request = {
  bangumi_id: 633836,
  file_path: 'Re0/Season4',
  file_names: ['a.mp4', 'b.mp4'],
  episode_offset: 0,
}

describe('confirmInbox API', () => {
  let postSpy: ReturnType<typeof vi.fn>

  beforeEach(() => {
    postSpy = asMock(vi.spyOn(api, 'post'))
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('posts to /library/inbox/confirm with a 600s timeout and returns data', async () => {
    const data = { anime: { id: 1 }, created: true, scanned: 2, episodes: [] }
    postSpy.mockResolvedValue(envelope({ code: 0, message: 'ok', data }))

    await expect(confirmInbox(request)).resolves.toEqual(data)
    expect(CONFIRM_INBOX_TIMEOUT_MS).toBe(600_000)
    expect(postSpy).toHaveBeenCalledWith('/library/inbox/confirm', request, { timeout: 600_000 })
  })

  it('throws ConfirmInboxError with readable reason / message / file_names when code !== 0', async () => {
    postSpy.mockResolvedValue(
      envelope({
        code: 9999,
        message: '这些文件未能归入该条目：b.mp4',
        data: { reason: 'no_match', message: '这些文件未能归入该条目：b.mp4', file_names: ['b.mp4'] },
      }),
    )

    const error = await confirmInbox(request).catch((e: unknown) => e)
    expect(error).toBeInstanceOf(ConfirmInboxError)
    expect(error).toBeInstanceOf(ApiError)
    const confirmError = error as ConfirmInboxError
    expect(confirmError.code).toBe(9999)
    expect(confirmError.reason).toBe('no_match')
    expect(confirmError.message).toBe('这些文件未能归入该条目：b.mp4')
    expect(confirmError.file_names).toEqual(['b.mp4'])
  })

  it('keeps review_required reason from the error data', async () => {
    postSpy.mockResolvedValue(
      envelope({
        code: 9999,
        message: '匹配需要人工确认: 全新目标没有可关联的文件',
        data: { reason: 'review_required', message: '匹配需要人工确认: 全新目标没有可关联的文件' },
      }),
    )

    const error = (await confirmInbox(request).catch((e: unknown) => e)) as ConfirmInboxError
    expect(error.reason).toBe('review_required')
    expect(error.message).toBe('匹配需要人工确认: 全新目标没有可关联的文件')
    expect(error.file_names).toEqual([])
  })

  it('falls back to invalid / upstream reason when error data is missing', async () => {
    postSpy.mockResolvedValueOnce(envelope({ code: 1001, message: '参数错误', data: null }))
    const invalid = (await confirmInbox(request).catch((e: unknown) => e)) as ConfirmInboxError
    expect(invalid).toBeInstanceOf(ConfirmInboxError)
    expect(invalid.reason).toBe('invalid')
    expect(invalid.message).toBe('参数错误')

    postSpy.mockResolvedValueOnce(envelope({ code: 9999, message: '内部错误', data: { reason: 'weird' } }))
    const upstream = (await confirmInbox(request).catch((e: unknown) => e)) as ConfirmInboxError
    expect(upstream.reason).toBe('upstream')
    expect(upstream.message).toBe('内部错误')
  })

  it('rejects a non-envelope body with ApiError(-1)', async () => {
    postSpy.mockResolvedValue(envelope('<html>502</html>'))
    const error = (await confirmInbox(request).catch((e: unknown) => e)) as ApiError
    expect(error).toBeInstanceOf(ApiError)
    expect(error).not.toBeInstanceOf(ConfirmInboxError)
    expect(error.code).toBe(-1)
  })
})

function inboxFile(overrides: Partial<UnidentifiedFile> = {}): UnidentifiedFile {
  return {
    file_name: 'ep19.mp4',
    reason: 'ambiguous',
    file_path: 'Re0/Season4',
    candidates: [
      { id: 547888, name: 'Re:Zero 3rd', name_cn: '丧失篇', score: 1, year: 2024, ep_count: 16 },
      { id: 633836, name: 'Re:Zero 4th', name_cn: '夺还篇', score: 1 },
    ],
    ...overrides,
  }
}

function mountCard(props: Partial<InstanceType<typeof InboxFileCard>['$props']> = {}) {
  return mount(InboxFileCard, {
    props: {
      file: inboxFile(),
      siblingCount: (id: number) => (id === 633836 ? 8 : 1),
      confirming: false,
      error: null,
      ...props,
    },
  })
}

function rowFor(wrapper: ReturnType<typeof mountCard>, label: string) {
  const row = wrapper.findAll('.candidate-row').find((item) => item.text().includes(label))
  expect(row).toBeTruthy()
  return row!
}

describe('InboxFileCard', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('labels the batch button with the sibling count and emits siblings / single scopes', async () => {
    const wrapper = mountCard()
    const row = rowFor(wrapper, '夺还篇')
    expect(row.find('.candidate-btn').text()).toBe('归入此番剧（同目录 8 个文件）')

    await row.find('.candidate-btn').trigger('click')
    await row.find('.candidate-single-btn').trigger('click')

    expect(wrapper.emitted('confirm')).toEqual([
      [{ bangumiId: 633836, scope: 'siblings' }],
      [{ bangumiId: 633836, scope: 'single' }],
    ])
  })

  it('renders year / ep_count and omits them when missing', () => {
    const wrapper = mountCard()
    expect(rowFor(wrapper, '丧失篇').find('.candidate-meta').text()).toBe('2024年 · 16话')
    expect(rowFor(wrapper, '夺还篇').find('.candidate-meta').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('undefined')
  })

  it('tolerates legacy items without a candidates field', () => {
    const legacy = inboxFile()
    delete (legacy as Partial<UnidentifiedFile>).candidates
    const wrapper = mountCard({ file: legacy })
    expect(wrapper.findAll('.candidate-btn')).toHaveLength(0)
    expect(wrapper.text()).toContain('ep19.mp4')
  })

  it('renders the error inline and expands the offset input on review_required', async () => {
    const wrapper = mountCard()
    expect(wrapper.find('.offset-input').exists()).toBe(false)

    await wrapper.setProps({ error: { message: '匹配需要人工确认: 章节冲突', reason: 'review_required' } })

    expect(wrapper.find('.inbox-card-error').text()).toContain('匹配需要人工确认: 章节冲突')
    expect(wrapper.find('.offset-input').exists()).toBe(true)
  })

  it('emits the offset value and blocks invalid offsets locally', async () => {
    const wrapper = mountCard()
    await wrapper.find('.offset-toggle-btn').trigger('click')
    const input = wrapper.find('.offset-input')

    await input.setValue('-3')
    await rowFor(wrapper, '夺还篇').find('.candidate-btn').trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
    expect(wrapper.text()).toContain('集号偏移必须是不小于 0 的整数')

    await input.setValue('77')
    await rowFor(wrapper, '夺还篇').find('.candidate-btn').trigger('click')
    expect(wrapper.emitted('confirm')).toEqual([[{ bangumiId: 633836, scope: 'siblings', episodeOffset: 77 }]])
  })

  it('does not emit while confirming', async () => {
    const wrapper = mountCard({ confirming: true })
    const btn = rowFor(wrapper, '夺还篇').find('.candidate-btn')
    expect(btn.attributes('disabled')).toBeDefined()
    await btn.trigger('click')
    expect(wrapper.emitted('confirm')).toBeUndefined()
  })

  it('searches Bangumi manually and emits confirm for the picked subject', async () => {
    const { searchBangumi } = await import('../api/bangumi')
    asMock(searchBangumi).mockResolvedValue([
      { id: 999, name: 'Manual', name_cn: '手动条目', summary: '', eps_count: 0, cover: '' },
    ])
    const wrapper = mountCard()
    await wrapper.find('.search-toggle-btn').trigger('click')
    await wrapper.find('.manual-search-input').setValue('  手动  ')
    await wrapper.find('.manual-search-form').trigger('submit')
    await flushPromises()

    expect(searchBangumi).toHaveBeenCalledWith('手动')
    const row = wrapper.find('.manual-result-row')
    expect(row.text()).toContain('手动条目')
    expect(row.find('.candidate-meta').exists()).toBe(false)
    await row.find('.manual-single-btn').trigger('click')
    expect(wrapper.emitted('confirm')).toEqual([[{ bangumiId: 999, scope: 'single' }]])
  })

  it('shows manual search errors inline', async () => {
    const { searchBangumi } = await import('../api/bangumi')
    asMock(searchBangumi).mockRejectedValue(new ApiError(9999, 'Bangumi 不可用'))
    const wrapper = mountCard()
    await wrapper.find('.search-toggle-btn').trigger('click')
    await wrapper.find('.manual-search-input').setValue('x')
    await wrapper.find('.manual-search-form').trigger('submit')
    await flushPromises()
    expect(wrapper.find('.manual-search').text()).toContain('Bangumi 不可用')
  })
})
