import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import AnimeAddView from './AnimeAddView.vue'

const mocks = vi.hoisted(() => ({
  replace: vi.fn(),
  createAnime: vi.fn(),
  searchBangumi: vi.fn(),
}))

vi.mock('vue-router', () => ({
  useRouter: () => ({ replace: mocks.replace }),
}))

vi.mock('../api/anime', () => ({
  createAnime: mocks.createAnime,
}))

vi.mock('../api/bangumi', () => ({
  searchBangumi: mocks.searchBangumi,
}))

const sampleAnime = {
  id: 42,
  title: 'Frieren',
  title_cn: '葬送的芙莉莲',
  bangumi_id: 400602,
  cover: '',
  summary: '',
  ep_count: 28,
  file_path: '',
  created_at: '',
}

// 搜索并选中条目后，用给定目录提交「确认添加」。
async function submitWithPath(filePath: string) {
  const wrapper = mount(AnimeAddView)
  await wrapper.find('#bangumi-search').setValue('Frieren')
  await wrapper.find('form.search-section').trigger('submit')
  await flushPromises()
  await wrapper.find('.search-item').trigger('click')
  await wrapper.find('#file-path').setValue(filePath)
  await wrapper.find('form.confirm-section').trigger('submit')
  await flushPromises()
  return wrapper
}

describe('AnimeAddView relative path validation', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.searchBangumi.mockResolvedValue([
      { id: 400602, name: 'Sousou no Frieren', name_cn: '葬送的芙莉莲', summary: '', eps_count: 28, cover: '' },
    ])
    mocks.createAnime.mockResolvedValue(sampleAnime)
    mocks.replace.mockResolvedValue(undefined)
  })

  it.each([
    ['', ''],
    ['Re_Zero', 'Re_Zero'],
    ['2026/Frieren', '2026/Frieren'],
    ['Season 2/Show', 'Season 2/Show'],
    ['foo..bar', 'foo..bar'],
    ['  2026/Frieren  ', '2026/Frieren'],
  ])('allows %j and submits it to createAnime', async (input, expected) => {
    const wrapper = await submitWithPath(input)

    expect(mocks.createAnime).toHaveBeenCalledTimes(1)
    expect(mocks.createAnime).toHaveBeenCalledWith(400602, expected)
    expect(mocks.replace).toHaveBeenCalledWith({ name: 'anime-detail', params: { id: 42 } })
    expect(wrapper.find('.confirm-section .error-msg').exists()).toBe(false)
    wrapper.unmount()
  })

  it.each([
    ['null byte', 'Show\0Dir'],
    ['leading slash', '/abs'],
    ['leading backslash', '\\abs'],
    ['drive letter', 'C:\\abs'],
    ['drive letter with slash', 'c:/abs'],
    ['leading ..', '../escape'],
    ['inner .. segment', 'a/../b'],
    ['backslash .. segment', 'a\\..\\b'],
    ['bare ..', '..'],
  ])('rejects %s (%j) without calling createAnime', async (_label, input) => {
    const wrapper = await submitWithPath(input)

    expect(mocks.createAnime).not.toHaveBeenCalled()
    expect(mocks.replace).not.toHaveBeenCalled()
    expect(wrapper.find('.confirm-section .error-msg').text()).toContain('相对路径')
    expect(wrapper.find('.confirm-section button[type="submit"]').attributes('disabled')).toBeUndefined()
    wrapper.unmount()
  })
})
