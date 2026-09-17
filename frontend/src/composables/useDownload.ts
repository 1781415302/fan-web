import { getCurrentScope, onScopeDispose, ref, type Ref } from 'vue'

import { buildDownloadUrl, requestMediaToken } from '../api/episode'
import type { Episode } from '../types/anime'

export interface UseDownloadResult {
  /** 正在获取下载地址的 episode id；null = 空闲。用于按钮 disabled/防重复点击 */
  readonly downloadingId: Ref<number | null>
  /** 完整下载动作：requestMediaToken → buildDownloadUrl → window.open(_blank)。 */
  downloadEpisode(episode: Episode): Promise<void>
}

export function useDownload(): UseDownloadResult {
  const downloadingId = ref<number | null>(null)

  // 组件卸载后置位；已卸载时不再抛错、不再触发下载（调用方已销毁）。
  let disposed = false
  if (getCurrentScope()) {
    onScopeDispose(() => {
      disposed = true
    })
  }

  async function downloadEpisode(episode: Episode): Promise<void> {
    downloadingId.value = episode.id
    try {
      // 在 await 之前弹出窗口，利用用户手势上下文避免浏览器拦截。
      // 先打开空白窗口，后续 token 获取成功后再跳转下载链接。
      // 部分浏览器（尤其是 Chrome 90+）对 await 后的 window.open
      // 更严格，在此处弹出可确保浏览器认为其由用户手势触发。
      const popup = window.open('', '_blank')
      if (popup === null) {
        if (disposed) return
        throw new Error('浏览器阻止了下载窗口')
      }
      const media = await requestMediaToken(episode.id)
      if (disposed) return
      try {
        popup.location.href = buildDownloadUrl(episode.id, media.token)
      } catch {
        // popup.location.href 赋值可能在跨域场景下抛出 DOMException
        //（如 popup 被关闭或导航失败），降级为普通 window.open。
        if (disposed) return
        const opened = window.open(
          buildDownloadUrl(episode.id, media.token),
          '_blank',
        )
        if (opened === null) {
          throw new Error('浏览器阻止了下载窗口')
        }
      }
    } catch (error: unknown) {
      if (disposed) return
      throw error
    } finally {
      downloadingId.value = null
    }
  }

  return {
    downloadingId,
    downloadEpisode,
  }
}
