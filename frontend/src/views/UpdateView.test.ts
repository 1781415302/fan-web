import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it, vi } from 'vitest'

import UpdateView from './UpdateView.vue'

const mocks = vi.hoisted(() => ({
  checkUpdate: vi.fn(),
  performUpdate: vi.fn(),
}))

vi.mock('../api/update', () => ({
  checkUpdate: mocks.checkUpdate,
  performUpdate: mocks.performUpdate,
}))

function mountUpdate() {
  const pinia = createPinia()
  setActivePinia(pinia)
  return mount(UpdateView, {
    global: { plugins: [pinia] },
  })
}

describe('UpdateView stale_old', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    vi.stubGlobal('confirm', vi.fn(() => true))
  })

  it('hides perform when stale_old is set', async () => {
    mocks.checkUpdate.mockResolvedValue({
      has_update: true,
      current_version: 'v1.3.0',
      latest_version: 'v1.3.1',
      release_notes: 'notes',
      stale_old: true,
    })
    const wrapper = mountUpdate()
    await wrapper.get('button').trigger('click')
    await flushPromises()

    expect(wrapper.text()).toContain('检测到更新残留备份')
    expect(wrapper.text()).not.toContain('立即更新')
    wrapper.unmount()
  })

  it('allows perform when there is an update and no stale backup', async () => {
    mocks.checkUpdate.mockResolvedValue({
      has_update: true,
      current_version: 'v1.3.0',
      latest_version: 'v1.3.1',
      release_notes: '',
      stale_old: false,
      download_size: 1024,
    })
    mocks.performUpdate.mockResolvedValue({ message: '更新完成', hint: '请稍候' })
    const wrapper = mountUpdate()
    await wrapper.get('button').trigger('click')
    await flushPromises()

    const perform = wrapper.findAll('button').find((button) => button.text().includes('立即更新'))
    expect(perform).toBeTruthy()
    await perform!.trigger('click')
    await flushPromises()
    expect(mocks.performUpdate).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
