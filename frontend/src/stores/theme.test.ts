import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { useThemeStore } from './theme'

const STORAGE_KEY = 'fan_web_ui'

function newStore() {
  setActivePinia(createPinia())
  return useThemeStore()
}

beforeEach(() => {
  window.localStorage.clear()
  delete document.documentElement.dataset.ui
})

describe('theme store', () => {
  it('uses modern and updates dataset when no cache', () => {
    const store = newStore()
    store.initialize()
    expect(store.ui).toBe('modern')
    expect(document.documentElement.dataset.ui).toBe('modern')
  })

  it('restores valid cache and falls back to modern for invalid', () => {
    window.localStorage.setItem(STORAGE_KEY, 'glass')
    const valid = newStore()
    valid.initialize()
    expect(valid.ui).toBe('glass')
    expect(document.documentElement.dataset.ui).toBe('glass')

    window.localStorage.setItem(STORAGE_KEY, 'unknown-style')
    const invalid = newStore()
    invalid.initialize()
    expect(invalid.ui).toBe('modern')
    expect(document.documentElement.dataset.ui).toBe('modern')
  })

  it('setUi updates store dataset and localStorage', () => {
    const store = newStore()
    store.setUi('apple')
    expect(store.ui).toBe('apple')
    expect(document.documentElement.dataset.ui).toBe('apple')
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('apple')
  })
})