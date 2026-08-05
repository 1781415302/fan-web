<script setup lang="ts">
import { computed } from 'vue'

import { useThemeStore } from './stores/theme'
import Analytics from './components/layouts/Analytics.vue'
import CinemaLayout from './components/layouts/CinemaLayout.vue'
import GlassLayout from './components/layouts/GlassLayout.vue'
import FluentLayout from './components/layouts/FluentLayout.vue'

const themeStore = useThemeStore()

const layout = computed(() => {
  switch (themeStore.ui) {
    case 'cinema':
      return CinemaLayout
    case 'glass':
      return GlassLayout
    case 'apple':
      return FluentLayout
    default:
      return Analytics
  }
})
</script>

<template>
  <component :is="layout">
    <router-view />
  </component>
</template>