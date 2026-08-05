import { onMounted } from 'vue'
import { useRouter } from 'vue-router'

import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { useThemeStore } from '../stores/theme'

export function useShell() {
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const themeStore = useThemeStore()
  const router = useRouter()

  onMounted(() => themeStore.initialize())

  async function handleLogout() {
    await authStore.logout()
    await router.replace({ name: 'login' })
  }

  return { appStore, authStore, themeStore, handleLogout }
}