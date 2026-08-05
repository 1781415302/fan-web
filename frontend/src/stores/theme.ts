import { defineStore } from 'pinia'

export type UiStyle = 'modern' | 'cinema' | 'glass' | 'apple'

const STORAGE_KEY = 'fan_web_ui'

export const UI_ACCENT: Record<UiStyle, string> = {
  modern: '#22c55e',
  cinema: '#E11D48',
  glass: '#5E6AD2',
  apple: '#0A84FF',
}

export const useThemeStore = defineStore('theme', {
  state: () => ({
    ui: 'modern' as UiStyle,
    initialized: false,
  }),
  getters: {
    accentColor(state): string {
      return UI_ACCENT[state.ui]
    },
  },
  actions: {
    initialize() {
      if (this.initialized) return
      const saved = window.localStorage.getItem(STORAGE_KEY)
      const valid = saved && isUiStyle(saved)
      this.ui = valid ? (saved as UiStyle) : 'modern'
      applyUiStyle(this.ui)
      this.initialized = true
    },
    setUi(style: UiStyle) {
      if (!isUiStyle(style)) return
      this.ui = style
      window.localStorage.setItem(STORAGE_KEY, style)
      applyUiStyle(style)
    },
  },
})

function isUiStyle(value: string): value is UiStyle {
  return value === 'modern' || value === 'cinema' || value === 'glass' || value === 'apple'
}

function applyUiStyle(style: UiStyle) {
  document.documentElement.dataset.ui = style
}
