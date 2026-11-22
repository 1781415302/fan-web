import { flushPromises, mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { ApiError } from '../api'
import { useAuthStore } from '../stores/auth'
import type { APIKeyItem, CreateAPIKeyResponse } from '../types/apiKey'
import AdminUsersView from './AdminUsersView.vue'

const mocks = vi.hoisted(() => ({
  apiGet: vi.fn(),
  apiPost: vi.fn(),
  apiDelete: vi.fn(),
  listAPIKeys: vi.fn(),
  createAPIKey: vi.fn(),
  deleteAPIKey: vi.fn(),
}))

vi.mock('../api', async (importOriginal) => {
  // 保留真实的 ApiError / unwrap，只替换用户管理用到的 Axios 实例方法。
  const actual = await importOriginal<typeof import('../api')>()
  return {
    ...actual,
    default: { get: mocks.apiGet, post: mocks.apiPost, delete: mocks.apiDelete },
  }
})

vi.mock('../api/apiKeys', () => ({
  listAPIKeys: mocks.listAPIKeys,
  createAPIKey: mocks.createAPIKey,
  deleteAPIKey: mocks.deleteAPIKey,
}))

const RAW_KEY = `fw_${'ab12'.repeat(16)}`

const existingKey: APIKeyItem = {
  id: 1,
  name: '旧下载机',
  key_prefix: 'fw_9f8e7d6c...beef',
  created_by: 1,
  last_used_at: '2026-11-20T08:00:00Z',
  created_at: '2026-11-01T08:00:00Z',
}

const unusedKey: APIKeyItem = {
  id: 2,
  name: '备用',
  key_prefix: 'fw_00112233...4455',
  created_by: 1,
  last_used_at: null,
  created_at: '2026-11-02T08:00:00Z',
}

const createdKey: CreateAPIKeyResponse = {
  id: 3,
  name: 'Hermes',
  key_prefix: `${RAW_KEY.slice(0, 11)}...${RAW_KEY.slice(-4)}`,
  created_by: 1,
  last_used_at: null,
  created_at: '2026-11-21T08:00:00Z',
  api_key: RAW_KEY,
}

function mountAdmin() {
  const pinia = createPinia()
  setActivePinia(pinia)
  useAuthStore().setSession('tok', { id: 1, username: 'admin', is_admin: true, created_at: '' })
  return mount(AdminUsersView, { global: { plugins: [pinia] } })
}

function storageValues(storage: Storage | undefined) {
  if (!storage) return []
  const values: string[] = []
  for (let index = 0; index < storage.length; index++) {
    const key = storage.key(index)
    if (key !== null) values.push(key, storage.getItem(key) ?? '')
  }
  return values
}

function keysPanel(wrapper: ReturnType<typeof mountAdmin>) {
  return wrapper.get('.api-keys-panel')
}

function keyRow(wrapper: ReturnType<typeof mountAdmin>, name: string) {
  const row = keysPanel(wrapper)
    .findAll('.api-key-row')
    .find((item) => item.text().includes(name))
  expect(row).toBeTruthy()
  return row!
}

async function createKey(wrapper: ReturnType<typeof mountAdmin>, name: string) {
  await wrapper.get('#api-key-name').setValue(name)
  await wrapper.get('form.api-key-form').trigger('submit')
  await flushPromises()
}

describe('AdminUsersView API keys', () => {
  let writeText: ReturnType<typeof vi.fn>
  let storageWrites: unknown[][]

  beforeEach(() => {
    vi.clearAllMocks()
    window.localStorage.clear()
    window.sessionStorage?.clear()
    mocks.apiGet.mockResolvedValue({
      data: { code: 0, message: 'ok', data: [{ id: 1, username: 'admin', is_admin: true, created_at: '' }] },
    })
    mocks.listAPIKeys.mockResolvedValue([existingKey, unusedKey])
    mocks.createAPIKey.mockResolvedValue(createdKey)
    mocks.deleteAPIKey.mockResolvedValue(undefined)
    writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    vi.stubGlobal('confirm', vi.fn(() => true))

    // 记录所有 Storage 写入，事后断言从未出现明文 Key。
    storageWrites = []
    const record = (...args: unknown[]) => {
      storageWrites.push(args)
    }
    const localSetItem = window.localStorage.setItem.bind(window.localStorage)
    vi.spyOn(window.localStorage, 'setItem').mockImplementation((key: string, value: string) => {
      record(key, value)
      localSetItem(key, value)
    })
    const protoSetItem = Storage.prototype.setItem
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(function (this: Storage, key: string, value: string) {
      record(key, value)
      protoSetItem.call(this, key, value)
    })
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    Reflect.deleteProperty(navigator, 'clipboard')
  })

  function expectRawKeyNeverStored() {
    expect(storageWrites.some((args) => args.some((arg) => String(arg).includes(RAW_KEY)))).toBe(false)
    expect(storageValues(window.localStorage).some((value) => value.includes(RAW_KEY))).toBe(false)
    expect(storageValues(window.sessionStorage).some((value) => value.includes(RAW_KEY))).toBe(false)
  }

  it('lists keys with masked prefix, created time and a never-used placeholder', async () => {
    const wrapper = mountAdmin()
    await flushPromises()

    expect(mocks.listAPIKeys).toHaveBeenCalledTimes(1)
    expect(keysPanel(wrapper).text()).toContain('API 密钥（外部工具接入）')
    const rows = keysPanel(wrapper).findAll('.api-key-row')
    expect(rows).toHaveLength(2)
    expect(keyRow(wrapper, '旧下载机').text()).toContain('fw_9f8e7d6c...beef')
    expect(keyRow(wrapper, '旧下载机').text()).not.toContain('从未使用')
    expect(keyRow(wrapper, '备用').text()).toContain('fw_00112233...4455')
    expect(keyRow(wrapper, '备用').text()).toContain('从未使用')
    wrapper.unmount()
  })

  it('shows the raw key once, copies it, and drops it after dismiss without touching storage', async () => {
    const wrapper = mountAdmin()
    await flushPromises()

    await createKey(wrapper, '  Hermes  ')

    expect(mocks.createAPIKey).toHaveBeenCalledWith('Hermes')
    const banner = keysPanel(wrapper).get('.api-key-banner')
    expect(banner.get('.api-key-secret').text()).toBe(RAW_KEY)
    // 新密钥进入列表时只展示掩码，表格中不出现明文。
    expect(keyRow(wrapper, 'Hermes').text()).toContain(createdKey.key_prefix)
    expect(keysPanel(wrapper).get('table').text()).not.toContain(RAW_KEY)
    expect((wrapper.get('#api-key-name').element as HTMLInputElement).value).toBe('')

    await banner.get('.api-key-copy-btn').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenCalledWith(RAW_KEY)
    expect(banner.text()).toContain('已复制到剪贴板')

    await keysPanel(wrapper).get('.api-key-dismiss-btn').trigger('click')
    expect(keysPanel(wrapper).find('.api-key-banner').exists()).toBe(false)
    expect(wrapper.html()).not.toContain(RAW_KEY)
    expectRawKeyNeverStored()
    wrapper.unmount()
    expectRawKeyNeverStored()
  })

  it('shows a fallback hint when clipboard write fails', async () => {
    writeText.mockRejectedValue(new Error('denied'))
    const wrapper = mountAdmin()
    await flushPromises()
    await createKey(wrapper, 'Hermes')

    await keysPanel(wrapper).get('.api-key-copy-btn').trigger('click')
    await flushPromises()

    expect(keysPanel(wrapper).get('.api-key-banner').text()).toContain('复制失败')
    expect(keysPanel(wrapper).get('.api-key-secret').text()).toBe(RAW_KEY)
    wrapper.unmount()
  })

  it.each([
    ['empty', ''],
    ['whitespace only', '   '],
    ['65 characters', 'a'.repeat(65)],
    ['65 CJK characters', '密'.repeat(65)],
  ])('rejects a %s name before calling the API', async (_label, name) => {
    const wrapper = mountAdmin()
    await flushPromises()

    await createKey(wrapper, name)

    expect(mocks.createAPIKey).not.toHaveBeenCalled()
    expect(keysPanel(wrapper).find('.api-key-form .form-error').exists()).toBe(true)
    expect(keysPanel(wrapper).find('.api-key-banner').exists()).toBe(false)
    wrapper.unmount()
  })

  it('accepts a 64-character CJK name (counted by characters, not bytes)', async () => {
    const wrapper = mountAdmin()
    await flushPromises()

    await createKey(wrapper, '密'.repeat(64))

    expect(mocks.createAPIKey).toHaveBeenCalledWith('密'.repeat(64))
    expect(keysPanel(wrapper).find('.api-key-form .form-error').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows the server error when creation fails', async () => {
    mocks.createAPIKey.mockRejectedValue(new ApiError(1001, '名称长度需为 1 到 64 个字符'))
    const wrapper = mountAdmin()
    await flushPromises()

    await createKey(wrapper, 'Hermes')

    expect(keysPanel(wrapper).get('.api-key-form .form-error').text()).toContain('名称长度需为 1 到 64 个字符')
    expect(keysPanel(wrapper).find('.api-key-banner').exists()).toBe(false)
    wrapper.unmount()
  })

  it('revokes a key after confirmation and removes it from the list', async () => {
    const wrapper = mountAdmin()
    await flushPromises()

    await keyRow(wrapper, '旧下载机').get('button').trigger('click')
    await flushPromises()

    expect(window.confirm).toHaveBeenCalledTimes(1)
    expect(mocks.deleteAPIKey).toHaveBeenCalledWith(1)
    expect(keysPanel(wrapper).findAll('.api-key-row')).toHaveLength(1)
    expect(keysPanel(wrapper).text()).not.toContain('旧下载机')
    wrapper.unmount()
  })

  it('does not revoke when the confirmation is cancelled', async () => {
    vi.stubGlobal('confirm', vi.fn(() => false))
    const wrapper = mountAdmin()
    await flushPromises()

    await keyRow(wrapper, '旧下载机').get('button').trigger('click')
    await flushPromises()

    expect(mocks.deleteAPIKey).not.toHaveBeenCalled()
    expect(keysPanel(wrapper).findAll('.api-key-row')).toHaveLength(2)
    wrapper.unmount()
  })

  it('keeps the row and shows an error when revocation fails', async () => {
    mocks.deleteAPIKey.mockRejectedValue(new ApiError(1002, 'API Key 不存在'))
    const wrapper = mountAdmin()
    await flushPromises()

    await keyRow(wrapper, '旧下载机').get('button').trigger('click')
    await flushPromises()

    expect(keysPanel(wrapper).get('.api-key-error').text()).toContain('API Key 不存在')
    expect(keysPanel(wrapper).findAll('.api-key-row')).toHaveLength(2)
    wrapper.unmount()
  })

  it('shows an error when the key list fails to load', async () => {
    mocks.listAPIKeys.mockRejectedValue(new ApiError(9999, '密钥列表读取失败'))
    const wrapper = mountAdmin()
    await flushPromises()

    expect(keysPanel(wrapper).get('.api-key-error').text()).toContain('密钥列表读取失败')
    expect(keysPanel(wrapper).findAll('.api-key-row')).toHaveLength(0)
    wrapper.unmount()
  })
})
