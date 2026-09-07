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
      const media = await requestMediaToken(episode.id)
      if (disposed) return
      const opened = window.open(buildDownloadUrl(episode.id, media.token), '_blank')
      if (opened === null) {
        if (disposed) return
        throw new Error('浏览器阻止了下载窗口')
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
