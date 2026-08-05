<script setup lang="ts">
import { onMounted, ref } from 'vue'

import { useThemeStore, UI_ACCENT, type UiStyle } from '../stores/theme'

const themeStore = useThemeStore()
const open = ref(false)

const OPTIONS: { value: UiStyle; cn: string; en: string }[] = [
  { value: 'modern', cn: '现代深色', en: 'Modern' },
  { value: 'cinema', cn: '影院深色', en: 'Cinema' },
  { value: 'glass', cn: '玻璃拟态', en: 'Glass' },
  { value: 'apple', cn: '苹果浅色', en: 'Apple' },
]

onMounted(() => themeStore.initialize())

function select(value: UiStyle) {
  themeStore.setUi(value)
  open.value = false
}
</script>

<template>
  <div class="theme-switcher">
    <button
      type="button"
      class="theme-trigger"
      :aria-label="`切换界面风格，当前：${themeStore.ui}`"
      :aria-expanded="open"
      @click="open = !open"
    >
      <span class="theme-dot" :style="{ backgroundColor: UI_ACCENT[themeStore.ui] }" aria-hidden="true"></span>
      <span>{{ OPTIONS.find((option) => option.value === themeStore.ui)?.cn }}</span>
    </button>

    <div v-if="open" class="theme-menu" role="menu">
      <button
        v-for="option in OPTIONS"
        :key="option.value"
        type="button"
        class="theme-option"
        :class="{ active: themeStore.ui === option.value }"
        role="menuitem"
        @click="select(option.value)"
      >
        <span class="theme-dot" :style="{ backgroundColor: UI_ACCENT[option.value] }" aria-hidden="true"></span>
        <span class="theme-option-label">
          <span class="theme-option-cn">{{ option.cn }}</span>
          <span class="theme-option-en">{{ option.en }}</span>
        </span>
      </button>
    </div>
  </div>
</template>
