import { onMounted, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import {
  deleteBangumiToken,
  getBangumiLink,
  putBangumiToken,
  syncBangumi,
} from '../api/bangumi'
import { useAppStore } from '../stores/app'
import { useAuthStore } from '../stores/auth'
import { useThemeStore } from '../stores/theme'

export function useShell() {
  const appStore = useAppStore()
  const authStore = useAuthStore()
  const themeStore = useThemeStore()
  const router = useRouter()

  const bangumiPanelOpen = ref(false)
  const bangumiLinked = ref(false)
  const bangumiSuffix = ref('')
  const bangumiTokenDraft = ref('')
  const bangumiLoading = ref(false)
  const bangumiLinkLoading = ref(false)
  const bangumiSyncing = ref(false)
  const bangumiError = ref('')
  const bangumiMessage = ref('')

  // 组件卸载后置位，异步回调据此放弃写回已销毁组件的 ref（避免 Vue 告警）。
  let disposed = false
  // 统计进行中的绑定/解绑操作，避免二者并发时一方的 finally 提前把
  // bangumiLoading 置回 false。
  let bangumiOpCount = 0

  onMounted(() => themeStore.initialize())
  onUnmounted(() => {
    disposed = true
  })

  watch(
    () => authStore.isAuthenticated,
    (ok) => {
      if (ok) {
        void loadBangumiLink()
        return
      }
      resetBangumiState()
    },
    { immediate: true },
  )

  function resetBangumiState() {
    bangumiPanelOpen.value = false
    bangumiLinked.value = false
    bangumiSuffix.value = ''
    bangumiTokenDraft.value = ''
    bangumiLoading.value = false
    bangumiSyncing.value = false
    bangumiError.value = ''
    bangumiMessage.value = ''
  }

  function applyBangumiLink(linked: boolean, suffix?: string) {
    bangumiLinked.value = linked
    bangumiSuffix.value = linked ? (suffix ?? '') : ''
    bangumiTokenDraft.value = ''
  }

  async function loadBangumiLink() {
    if (!authStore.isAuthenticated) {
      applyBangumiLink(false)
      return
    }
    // 请求进行中去重：watch(immediate) / openBangumiPanel / syncBangumiProgress 的
    // catch 都可能触发，避免重复请求。
    if (bangumiLinkLoading.value) {
      return
    }
    bangumiLinkLoading.value = true
    try {
      const data = await getBangumiLink()
      if (disposed) return
      applyBangumiLink(data.linked, data.suffix)
    } catch (error: unknown) {
      if (disposed) return
      bangumiError.value = error instanceof ApiError ? error.message : '查询 Bangumi 绑定失败'
    } finally {
      bangumiLinkLoading.value = false
    }
  }

  async function openBangumiPanel() {
    bangumiError.value = ''
    bangumiMessage.value = ''
    bangumiPanelOpen.value = true
    await loadBangumiLink()
  }

  function closeBangumiPanel() {
    bangumiPanelOpen.value = false
    bangumiTokenDraft.value = ''
    bangumiError.value = ''
    bangumiMessage.value = ''
  }

  async function bindBangumi() {
    const token = bangumiTokenDraft.value.trim()
    bangumiError.value = ''
    bangumiMessage.value = ''
    if (!token) {
      bangumiError.value = '请输入 Access Token'
      return
    }
    bangumiLoading.value = true
    bangumiOpCount++
    try {
      const data = await putBangumiToken(token)
      if (disposed) return
      applyBangumiLink(data.linked, data.suffix)
      bangumiMessage.value = data.suffix ? `已绑定 ···${data.suffix}` : '已绑定'
    } catch (error: unknown) {
      if (disposed) return
      bangumiError.value = error instanceof ApiError ? error.message : '绑定失败'
    } finally {
      bangumiOpCount--
      if (bangumiOpCount === 0) bangumiLoading.value = false
    }
  }

  async function unbindBangumi() {
    bangumiError.value = ''
    bangumiMessage.value = ''
    bangumiOpCount++
    bangumiLoading.value = true
    try {
      const data = await deleteBangumiToken()
      if (disposed) return
      applyBangumiLink(data.linked, data.suffix)
      bangumiMessage.value = '已解除绑定'
    } catch (error: unknown) {
      if (disposed) return
      bangumiError.value = error instanceof ApiError ? error.message : '解除绑定失败'
    } finally {
      bangumiOpCount--
      if (bangumiOpCount === 0) bangumiLoading.value = false
    }
  }

  async function syncBangumiProgress() {
    bangumiError.value = ''
    bangumiMessage.value = ''
    bangumiSyncing.value = true
    try {
      const data = await syncBangumi()
      if (disposed) return
      bangumiMessage.value = `已同步 ${data.animes} 部，标记 ${data.episodes_marked} 集`
    } catch (error: unknown) {
      if (disposed) return
      bangumiError.value = error instanceof ApiError ? error.message : '同步失败'
      await loadBangumiLink()
    } finally {
      bangumiSyncing.value = false
    }
  }

  async function handleLogout() {
    resetBangumiState()
    await authStore.logout()
    await router.replace({ name: 'login' })
  }

  return {
    appStore,
    authStore,
    themeStore,
    handleLogout,
    bangumiPanelOpen,
    bangumiLinked,
    bangumiSuffix,
    bangumiTokenDraft,
    bangumiLoading,
    bangumiSyncing,
    bangumiError,
    bangumiMessage,
    openBangumiPanel,
    closeBangumiPanel,
    bindBangumi,
    unbindBangumi,
    syncBangumiProgress,
  }
}
