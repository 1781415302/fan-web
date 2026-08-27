import { createPinia, setActivePinia } from 'pinia'
import { beforeEach, describe, expect, it } from 'vitest'

import { useThemeStore, UI_ACCENT, type UiStyle } from './theme'

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

  it('exposes accentColor from UI_ACCENT for the current ui', () => {
    const store = newStore()
    store.setUi('glass')
    expect(store.accentColor).toBe(UI_ACCENT.glass)
    store.setUi('apple')
    expect(store.accentColor).toBe(UI_ACCENT.apple)
  })

  it('ignores invalid ui style in setUi', () => {
    const store = newStore()
    store.setUi('cinema')
    expect(store.ui).toBe('cinema')
    store.setUi('bogus-style' as unknown as UiStyle)
    expect(store.ui).toBe('cinema')
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe('cinema')
  })
})