<script setup lang="ts">
import Artplayer from 'artplayer'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../api'
import {
  getStreamUrl,
  getSubtitleTracks,
  getSubtitleUrl,
  requestMediaToken,
  type SubtitleTrack,
} from '../api/episode'
import { getAnime, listEpisodes } from '../api/anime'
import { getAnimeProgress, reportProgress } from '../api/progress'
import { useThemeStore } from '../stores/theme'
import type { Anime, Episode } from '../types/anime'
import type { AnimeProgress } from '../types/progress'

const themeStore = useThemeStore()

const route = useRoute()
const router = useRouter()

const animeId = computed(() => Number(route.params.id))
const episodeId = computed(() => Number(route.params.epId))
const anime = ref<Anime | null>(null)
const episodes = ref<Episode[]>([])
const progressByEpisode = ref(new Map<number, AnimeProgress>())
const loading = ref(true)
const error = ref('')
const playerError = ref('')
const progressError = ref('')
const statusMessage = ref('')
const currentPosition = ref(0)
const currentDuration = ref(0)
type PlaybackRateOption = 0.5 | 1 | 1.25 | 1.5 | 2
type SubtitleFontSizeOption = 20 | 24 | 28
const subtitleFontSizeOptions: ReadonlyArray<{ value: SubtitleFontSizeOption; label: string }> = [
  { value: 20, label: '小' },
  { value: 24, label: '中' },
  { value: 28, label: '大' },
]
const subtitleFontSizeStorageKey = 'fan_web_subtitle_font_size'
const subtitleFullscreenReferenceHeight = 540
const subtitleFullscreenMaxScale = 2
const playbackRate = ref<PlaybackRateOption>(1)
const subtitleTracks = ref<SubtitleTrack[]>([])
const subtitleFontSize = ref<SubtitleFontSizeOption>(readSubtitleFontSize())
const mediaToken = ref('')
const playerContainer = ref<HTMLDivElement | null>(null)

let player: Artplayer | null = null
let activeEpisode: Episode | null = null
let progressTimer: ReturnType<typeof setInterval> | undefined
let loadSerial = 0
let reportChain: Promise<void> = Promise.resolve()

// 媒体票据有效期 12h（backend mediaTokenTTL），过期后 stream/subtitle 会返回
// 200+JSON 错误体导致播放中断。以下状态用于在过期前预取新票据并重载播放器。
const mediaTokenRefreshMarginMs = 5 * 60_000
let mediaTokenExpiresAt = 0
let mediaTokenRefreshTimer: ReturnType<typeof setTimeout> | undefined
let mediaTokenRefreshInFlight = false
let pendingResumeAfterReload = -1
let pendingResumeShouldPlay = false
let activeSubtitleTrack: number | null = null

const displayTitle = computed(() => anime.value?.title_cn || anime.value?.title || '')
const currentEpisode = computed(() => episodes.value.find((episode) => episode.id === episodeId.value) || null)
const currentIndex = computed(() => episodes.value.findIndex((episode) => episode.id === episodeId.value))
const watchedCount = computed(() => {
  let count = 0
  for (const progress of progressByEpisode.value.values()) {
    if (progress.watched) count += 1
  }
  return count
})
const totalEpisodeCount = computed(() => {
  if (anime.value?.ep_count && anime.value.ep_count > 0) return anime.value.ep_count
  return episodes.value.length
})
const currentProgress = computed(() => progressByEpisode.value.get(episodeId.value))
const currentStatus = computed(() => {
  if (currentProgress.value?.watched) return '已看'
  if ((currentProgress.value?.position || 0) > 0) return '进行中'
  return '未看'
})

function formatTime(value: number) {
  if (!Number.isFinite(value) || value < 0) return '00:00'
  const seconds = Math.floor(value)
  const hours = Math.floor(seconds / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const remainder = seconds % 60
  if (hours > 0) {
    return `${String(hours).padStart(2, '0')}:${String(minutes).padStart(2, '0')}:${String(remainder).padStart(2, '0')}`
  }
  return `${String(minutes).padStart(2, '0')}:${String(remainder).padStart(2, '0')}`
}

function episodeStatus(episode: Episode) {
  const progress = progressByEpisode.value.get(episode.id)
  if (progress?.watched) return '已看'
  if ((progress?.position || 0) > 0) return '进行中'
  return '未看'
}

function stopProgressTimer() {
  if (progressTimer) {
    clearInterval(progressTimer)
    progressTimer = undefined
  }
}

function startProgressTimer() {
  stopProgressTimer()
  progressTimer = setInterval(() => {
    if (player?.playing && activeEpisode) {
      void queueProgressReport(player, activeEpisode)
    }
  }, 10_000)
}

function updateProgressState(id: number, position: number, watched: boolean) {
  const next = new Map(progressByEpisode.value)
  next.set(id, {
    episode_id: id,
    position,
    watched,
    updated_at: new Date().toISOString(),
  })
  progressByEpisode.value = next
}

function queueProgressReport(instance: Artplayer, episode: Episode, forceWatched = false) {
  const position = Math.max(0, Math.floor(Number.isFinite(instance.currentTime) ? instance.currentTime : 0))
  const duration = Number.isFinite(instance.duration) ? instance.duration : 0
  const watched = forceWatched || (duration > 0 && position / duration >= 0.9)

  reportChain = reportChain
    .catch(() => undefined)
    .then(async () => {
      try {
        await reportProgress(episode.id, position, watched)
        // 服务端 UpsertProgress 的 watched 只会 0→1 不回退；本地覆写时也要取并集，
        // 否则回看（拖回 90% 之前）会错误地把已看状态覆写为未看。
        const existing = progressByEpisode.value.get(episode.id)
        updateProgressState(episode.id, position, existing?.watched || watched)
        progressError.value = ''
      } catch (e: unknown) {
        progressError.value = e instanceof ApiError ? e.message : '播放进度保存失败'
      }
    })
  return reportChain
}

function stopMediaTokenRefresh() {
  if (mediaTokenRefreshTimer) {
    clearTimeout(mediaTokenRefreshTimer)
    mediaTokenRefreshTimer = undefined
  }
}

// 在票据过期前（剩余 5 分钟，最少 1 分钟）安排一次预刷新。
function scheduleMediaTokenRefresh(expiresAtMs: number) {
  stopMediaTokenRefresh()
  if (!Number.isFinite(expiresAtMs) || expiresAtMs <= Date.now()) return
  const delay = Math.max(60_000, expiresAtMs - Date.now() - mediaTokenRefreshMarginMs)
  mediaTokenRefreshTimer = setTimeout(() => {
    mediaTokenRefreshTimer = undefined
    void refreshMediaToken()
  }, delay)
}

// 重取媒体票据，并用新票据重载播放器（保留播放位置与字幕），
// 避免 12h 后票据过期导致播放中断。定时器与 video:error 两条路径共用。
async function refreshMediaToken() {
  if (mediaTokenRefreshInFlight || !player || !activeEpisode) return
  const serial = loadSerial
  const targetEpisodeId = episodeId.value
  mediaTokenRefreshInFlight = true
  try {
    const media = await requestMediaToken(targetEpisodeId)
    if (serial !== loadSerial || episodeId.value !== targetEpisodeId || !player || !activeEpisode) return
    const expiresAtMs = new Date(media.expires_at).getTime()
    scheduleMediaTokenRefresh(expiresAtMs)
    mediaTokenExpiresAt = expiresAtMs
    playerError.value = ''
    if (media.token === mediaToken.value) return

    const instance = player
    const episode = activeEpisode
    const resumeAt = Math.max(0, Math.floor(currentPosition.value) - 3)
    pendingResumeAfterReload = resumeAt
    pendingResumeShouldPlay = instance.playing
    const subtitleTrack = activeSubtitleTrack
    mediaToken.value = media.token
    // 必须用 switchQuality（内部等价 switchUrl(url, art.currentTime)）而非 switchUrl(url, 0)：
    // artplayer 内部在 switchUrl 时注册的 loadedmetadata 处理器晚于视图的处理器，
    // 按注册顺序后执行，会把 currentTime 覆写为 0 导致续期重载后跳回片头；
    // switchQuality 由 artplayer 自己在最后恢复为刷新前的播放位置。
    try {
      await instance.switchQuality(getStreamUrl(episode.id, media.token))
    } catch (e: unknown) {
      pendingResumeAfterReload = -1
      pendingResumeShouldPlay = false
      throw e
    }
    if (subtitleTrack !== null) {
      const label = subtitleTracks.value.find((track) => track.track_number === subtitleTrack)?.label ?? ''
      void instance.subtitle.switch(getSubtitleUrl(episode.id, subtitleTrack, media.token), {
        name: label,
        type: 'vtt',
      })
    }
    playerError.value = ''
    statusMessage.value = '播放票据已续期'
  } catch (e: unknown) {
    pendingResumeAfterReload = -1
    pendingResumeShouldPlay = false
    if (serial === loadSerial) {
      playerError.value = e instanceof ApiError ? `${e.message}，请刷新页面后重试` : '媒体票据续期失败，请刷新页面后重试'
    }
  } finally {
    mediaTokenRefreshInFlight = false
  }
}

function destroyPlayer() {
  stopProgressTimer()
  stopMediaTokenRefresh()
  pendingResumeAfterReload = -1
  pendingResumeShouldPlay = false
  activeSubtitleTrack = null
  if (!player) return

  const oldPlayer = player
  const currentTime = Number.isFinite(oldPlayer.currentTime) ? oldPlayer.currentTime : 0
  if (activeEpisode && currentTime >= 1) {
    void queueProgressReport(oldPlayer, activeEpisode)
  }
  player = null
  activeEpisode = null
  oldPlayer.destroy()
}

const emptySubtitleUrl = 'data:text/vtt;charset=utf-8,WEBVTT%0A%0A'

function isSubtitleFontSize(value: number): value is SubtitleFontSizeOption {
  return subtitleFontSizeOptions.some((option) => option.value === value)
}

function readSubtitleFontSize(): SubtitleFontSizeOption {
  const storedValue = Number(window.localStorage.getItem(subtitleFontSizeStorageKey))
  return isSubtitleFontSize(storedValue) ? storedValue : 20
}

function renderedSubtitleFontSize(instance: Artplayer) {
  if (!instance.fullscreen && !instance.fullscreenWeb) return subtitleFontSize.value

  const playerHeight = instance.template.$player.getBoundingClientRect().height
  const fullscreenScale = Math.min(
    subtitleFullscreenMaxScale,
    Math.max(1, playerHeight / subtitleFullscreenReferenceHeight),
  )
  return Math.round(subtitleFontSize.value * fullscreenScale)
}

function applySubtitleFontSize(instance: Artplayer) {
  instance.cssVar('--art-subtitle-font-size', `${renderedSubtitleFontSize(instance)}px`)
}

function scheduleSubtitleFontSize(instance: Artplayer) {
  window.requestAnimationFrame(() => {
    if (player === instance) applySubtitleFontSize(instance)
  })
}

function createSubtitleSizeControl() {
  return {
    name: 'subtitle-size',
    html: '字号',
    tooltip: '字幕字号',
    position: 'right' as const,
    selector: subtitleFontSizeOptions.map((option) => ({
      html: option.label,
      value: option.value,
      default: subtitleFontSize.value === option.value,
    })),
    onSelect(this: Artplayer, selector: { value?: number | string }) {
      const size = Number(selector.value)
      if (!isSubtitleFontSize(size)) return '字号'
      subtitleFontSize.value = size
      window.localStorage.setItem(subtitleFontSizeStorageKey, String(size))
      applySubtitleFontSize(this)
      return '字号'
    },
  }
}

function createSubtitleControl(episode: Episode) {
  const tracks = subtitleTracks.value
  return {
    name: 'subtitle',
    html: '字幕',
    tooltip: '字幕',
    position: 'right' as const,
    selector: [
      { html: '关闭字幕', value: 'off' },
      ...tracks.map((track, index) => ({
        html: escapeTrackLabel(track.label),
        value: track.track_number,
        default: index === 0,
      })),
    ],
    onSelect(this: Artplayer, selector: { value?: string | number }) {
      if (selector.value === 'off') {
        activeSubtitleTrack = null
        void this.subtitle.switch(emptySubtitleUrl, { name: '关闭字幕', type: 'vtt' })
        return '字幕'
      }
      const track = tracks.find((item) => item.track_number === Number(selector.value))
      if (!track) return '字幕'
      activeSubtitleTrack = track.track_number
      void this.subtitle.switch(getSubtitleUrl(episode.id, track.track_number, mediaToken.value), {
        name: track.label,
        type: 'vtt',
      })
      return '字幕'
    },
  }
}

function createPlayer(episode: Episode) {
  if (!playerContainer.value || !anime.value) return

  const defaultSubtitle = subtitleTracks.value[0]
  const instance = new Artplayer({
    container: playerContainer.value,
    url: getStreamUrl(episode.id, mediaToken.value),
    poster: anime.value.cover || undefined,
    theme: themeStore.accentColor,
    autoplay: false,
    playbackRate: false,
    pip: true,
    fullscreen: true,
    fullscreenWeb: true,
    setting: true,
    hotkey: true,
    mutex: true,
    playsInline: true,
    subtitle: defaultSubtitle
      ? {
          url: getSubtitleUrl(episode.id, defaultSubtitle.track_number, mediaToken.value),
          name: defaultSubtitle.label,
          type: 'vtt',
        }
      : undefined,
    controls: subtitleTracks.value.length
      ? [createSubtitleControl(episode), createSubtitleSizeControl()]
      : [],
  })

  player = instance
  activeEpisode = episode
  activeSubtitleTrack = defaultSubtitle?.track_number ?? null
  instance.playbackRate = playbackRate.value
  applySubtitleFontSize(instance)
  instance.on('resize', () => scheduleSubtitleFontSize(instance))
  playerError.value = ''
  progressError.value = ''
  statusMessage.value = currentProgress.value?.position ? '正在恢复播放位置' : '准备播放'

  instance.on('video:loadedmetadata', () => {
    if (player !== instance) return
    currentDuration.value = Number.isFinite(instance.duration) ? instance.duration : 0
    if (pendingResumeAfterReload >= 0) {
      // 媒体票据续期后的重载：恢复到刷新前的播放位置，而不是服务器保存的旧位置
      const resumePosition = Math.min(pendingResumeAfterReload, Math.max(0, currentDuration.value - 1))
      const shouldPlay = pendingResumeShouldPlay
      pendingResumeAfterReload = -1
      pendingResumeShouldPlay = false
      instance.currentTime = resumePosition
      currentPosition.value = resumePosition
      statusMessage.value = '播放票据已续期，继续播放'
      if (shouldPlay) void instance.play()
      return
    }
    const savedPosition = currentProgress.value?.position || 0
    if (savedPosition > 0 && currentDuration.value > 0) {
      const resumePosition = Math.min(savedPosition, Math.max(0, currentDuration.value - 1))
      instance.currentTime = resumePosition
      currentPosition.value = resumePosition
      statusMessage.value = `已恢复到 ${formatTime(resumePosition)}`
    }
  })

  instance.on('video:timeupdate', () => {
    if (player !== instance) return
    currentPosition.value = Number.isFinite(instance.currentTime) ? instance.currentTime : 0
    currentDuration.value = Number.isFinite(instance.duration) ? instance.duration : 0
  })

  instance.on('video:play', () => {
    if (player !== instance) return
    statusMessage.value = '播放中'
    startProgressTimer()
  })

  instance.on('video:pause', () => {
    if (player !== instance) return
    stopProgressTimer()
    statusMessage.value = '已暂停，进度已保存'
    if (activeEpisode) void queueProgressReport(instance, activeEpisode)
  })

  instance.on('video:ended', () => {
    if (player !== instance) return
    stopProgressTimer()
    currentPosition.value = currentDuration.value
    statusMessage.value = '本集播放完成，已标记为已看'
    if (activeEpisode) void queueProgressReport(instance, activeEpisode, true)
  })

  instance.on('video:error', () => {
    if (player !== instance) return
    if (mediaTokenRefreshInFlight || pendingResumeAfterReload >= 0) {
      return
    }
    // 媒体票据过期（12h）后 stream 返回 200+JSON 错误体，视频解码失败触发 error。
    // 此时尝试重取票据并重载；票据仍在有效期内则按普通加载失败提示。
    if (
      mediaTokenExpiresAt > 0 &&
      Date.now() >= mediaTokenExpiresAt - 60_000 &&
      !mediaTokenRefreshInFlight
    ) {
      void refreshMediaToken()
      return
    }
    playerError.value = '视频加载失败，请检查视频文件或网络连接'
  })
}

function escapeTrackLabel(label: string) {
  return label
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

async function load() {
  const serial = ++loadSerial
  destroyPlayer()
  loading.value = true
  error.value = ''
  playerError.value = ''

  if (!Number.isInteger(animeId.value) || animeId.value <= 0 || !Number.isInteger(episodeId.value) || episodeId.value <= 0) {
    error.value = '无效的观看地址'
    loading.value = false
    return
  }

  try {
    const [currentAnime, currentEpisodes, progressList, availableSubtitleTracks, media] = await Promise.all([
      getAnime(animeId.value),
      listEpisodes(animeId.value),
      getAnimeProgress(animeId.value),
      getSubtitleTracks(episodeId.value).catch(() => []),
      requestMediaToken(episodeId.value),
    ])
    if (serial !== loadSerial) return

    anime.value = currentAnime
    episodes.value = currentEpisodes
    subtitleTracks.value = availableSubtitleTracks
    mediaToken.value = media.token
    mediaTokenExpiresAt = new Date(media.expires_at).getTime()
    scheduleMediaTokenRefresh(mediaTokenExpiresAt)
    progressByEpisode.value = new Map(progressList.map((progress) => [progress.episode_id, progress]))
    const selectedEpisode = currentEpisodes.find((episode) => episode.id === episodeId.value)
    if (!selectedEpisode) {
      error.value = currentEpisodes.length ? '集数不存在' : '暂无可播放集数，请先扫描视频文件'
      loading.value = false
      return
    }

    loading.value = false
    await nextTick()
    if (serial !== loadSerial) return
    createPlayer(selectedEpisode)
  } catch (e: unknown) {
    if (serial !== loadSerial) return
    error.value = e instanceof ApiError ? e.message : '加载观看页失败'
    anime.value = null
    episodes.value = []
  } finally {
    if (serial === loadSerial) loading.value = false
  }
}

function navigateToEpisode(id: number) {
  if (id <= 0 || id === episodeId.value) return
  void router.push({ name: 'watch', params: { id: animeId.value, epId: id } })
}

function onEpisodeSelect(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value)
  navigateToEpisode(value)
}

function onPlaybackRateSelect(event: Event) {
  const value = Number((event.target as HTMLSelectElement).value)
  if (![0.5, 1, 1.25, 1.5, 2].includes(value)) return
  playbackRate.value = value as PlaybackRateOption
  if (player) player.playbackRate = playbackRate.value
}

function changeEpisode(offset: number) {
  const nextEpisode = episodes.value[currentIndex.value + offset]
  if (nextEpisode) navigateToEpisode(nextEpisode.id)
}

watch([animeId, episodeId], () => void load(), { immediate: true })

onBeforeUnmount(() => {
  ++loadSerial
  destroyPlayer()
})
</script>

<template>
  <section class="watch-page" aria-labelledby="watch-title">
    <p v-if="error" class="error-msg" role="alert">{{ error }}</p>
    <div v-if="loading" class="empty-state" aria-live="polite">加载观看页...</div>
    <div v-else-if="!anime || !currentEpisode" class="empty-state">
      <router-link :to="{ name: 'anime-detail', params: { id: animeId } }">返回番剧详情</router-link>
    </div>
    <template v-else>
      <header class="watch-heading">
        <div class="watch-title-block">
          <router-link class="back-link" :to="{ name: 'anime-detail', params: { id: animeId } }">返回番剧详情</router-link>
          <div class="watch-kicker-row">
            <p class="eyebrow">正在观看</p>
            <span class="watch-state">{{ currentStatus }}</span>
          </div>
          <h1 id="watch-title">{{ displayTitle }}</h1>
          <p class="watch-meta">第 {{ currentEpisode.ep_number }} 话 · 已观看 {{ watchedCount }}/{{ totalEpisodeCount }}</p>
        </div>
        <div class="watch-controls">
          <label class="episode-select-label">
            <span>切换集数</span>
            <select :value="episodeId" @change="onEpisodeSelect">
              <option v-for="episode in episodes" :key="episode.id" :value="episode.id">
                第 {{ episode.ep_number }} 话 · {{ episodeStatus(episode) }}
              </option>
            </select>
          </label>
          <label class="episode-select-label">
            <span>播放速度</span>
            <select :value="playbackRate" aria-label="播放速度" @change="onPlaybackRateSelect">
              <option :value="0.5">0.5x</option>
              <option :value="1">1x</option>
              <option :value="1.25">1.25x</option>
              <option :value="1.5">1.5x</option>
              <option :value="2">2x</option>
            </select>
          </label>
        </div>
      </header>

      <div class="watch-layout">
        <section class="player-column" aria-labelledby="player-region-title">
          <h2 id="player-region-title" class="visually-hidden">视频播放器</h2>
          <div class="player-shell">
            <div ref="playerContainer" class="player-container" role="region" aria-label="视频播放器"></div>
            <div class="player-status" aria-live="polite">
              <span class="status-message">{{ statusMessage }}</span>
              <span class="status-time">{{ formatTime(currentPosition) }} / {{ formatTime(currentDuration) }}</span>
            </div>
          </div>
          <p v-if="playerError" class="error-msg" role="alert">{{ playerError }}</p>
          <p v-if="progressError" class="progress-error" role="alert">{{ progressError }}</p>
        </section>

        <aside class="episode-panel" aria-label="集数列表">
          <div class="episode-panel-heading">
            <div>
              <p class="panel-kicker">Episodes</p>
              <h2>集数列表</h2>
            </div>
            <span>{{ watchedCount }}/{{ totalEpisodeCount }} 已看</span>
          </div>
          <div class="episode-list">
            <button
              v-for="episode in episodes"
              :key="episode.id"
              type="button"
              class="episode-item"
              :class="{ active: episode.id === episodeId }"
              @click="navigateToEpisode(episode.id)"
            >
              <span>第 {{ episode.ep_number }} 话</span>
              <span class="episode-state" :class="`state-${episodeStatus(episode)}`">{{ episodeStatus(episode) }}</span>
            </button>
          </div>
        </aside>
      </div>

      <nav class="watch-navigation" aria-label="集数导航">
        <button type="button" class="action-btn" :disabled="currentIndex <= 0" @click="changeEpisode(-1)">上一集</button>
        <router-link class="action-btn" :to="{ name: 'anime-detail', params: { id: animeId } }">详情</router-link>
        <button type="button" class="action-btn" :disabled="currentIndex < 0 || currentIndex >= episodes.length - 1" @click="changeEpisode(1)">下一集</button>
      </nav>
    </template>
  </section>
</template>

<style scoped>
.watch-page { max-width: 1320px; margin: 0 auto; padding-bottom: 32px; }
.watch-heading { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: end; gap: 28px; padding: 8px 0 28px; border-bottom: 1px solid var(--border-color); }
.watch-title-block { min-width: 0; }
.back-link { display: inline-flex; min-height: 40px; align-items: center; margin-bottom: 14px; color: var(--text-secondary); font-size: 13px; text-decoration: none; }
.back-link:hover { color: var(--accent-color); }
.watch-kicker-row { display: flex; align-items: center; gap: 10px; }
.watch-kicker-row .eyebrow { margin-bottom: 0; }
.watch-state { padding: 5px 9px; border: 1px solid var(--accent-soft-border); border-radius: 999px; background: var(--accent-glow); color: var(--accent-color); font-size: 12px; }
h1 { max-width: 860px; margin-top: 12px; color: var(--text-color); font-size: 36px; font-weight: 700; line-height: 1.15; overflow-wrap: anywhere; }
.watch-meta { margin-top: 8px; color: var(--text-secondary); font-size: 14px; }
.watch-controls { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 10px; }
.episode-select-label { display: flex; min-width: 190px; flex-direction: column; gap: 7px; color: var(--text-secondary); font-size: 12px; font-weight: 600; }
.episode-select-label select { min-height: 46px; padding: 0 12px; border: 1px solid var(--border-strong-color); border-radius: var(--radius-sm); background: var(--surface-color); color: var(--text-color); outline: none; cursor: pointer; transition: border-color 180ms ease-out, box-shadow 180ms ease-out; }
.episode-select-label select:focus { border-color: var(--accent-color); box-shadow: 0 0 0 4px var(--accent-glow); }
.watch-layout { display: grid; grid-template-columns: minmax(0, 1fr) 300px; gap: 20px; align-items: start; padding-top: 28px; }
.player-column { min-width: 0; }
.player-shell { overflow: hidden; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-lg); }
.player-container { width: 100%; aspect-ratio: 16 / 9; min-height: 260px; overflow: hidden; background: var(--player-bg); }
.player-status { display: flex; justify-content: space-between; gap: 12px; padding: 12px 16px; border-top: 1px solid var(--border-color); color: var(--text-secondary); font-size: 13px; }
.status-message { color: var(--text-color); }
.status-time { color: var(--text-muted-color); font-variant-numeric: tabular-nums; }
.progress-error { margin-top: 10px; color: var(--danger-color); font-size: 13px; }
.episode-panel { min-width: 0; overflow: hidden; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-sm); }
.episode-panel-heading { display: flex; align-items: end; justify-content: space-between; gap: 8px; padding: 16px; border-bottom: 1px solid var(--border-color); }
.panel-kicker { margin-bottom: 4px; color: var(--text-muted-color); font-size: 10px; font-weight: 700; text-transform: uppercase; }
.episode-panel-heading h2 { color: var(--text-color); font-size: 18px; }
.episode-panel-heading > span { color: var(--text-secondary); font-size: 12px; white-space: nowrap; }
.episode-list { max-height: 548px; overflow-y: auto; padding: 8px; }
.episode-item { display: flex; width: 100%; min-height: 48px; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 10px; border: 1px solid transparent; border-radius: var(--radius-sm); background: transparent; color: var(--text-color); text-align: left; cursor: pointer; transition: background-color 180ms ease-out, border-color 180ms ease-out; }
.episode-item:hover { background: var(--surface-hover); }
.episode-item.active { border-color: var(--primary-soft-border); background: var(--primary-soft-bg); }
.episode-state { flex: 0 0 auto; font-size: 12px; }
.state-已看 { color: var(--success-color); }
.state-进行中 { color: var(--warning-color); }
.state-未看 { color: var(--text-muted-color); }
.watch-navigation { display: flex; justify-content: space-between; gap: 10px; margin-top: 18px; }
.watch-navigation .action-btn { min-width: 96px; }
@media (max-width: 920px) { .watch-layout { grid-template-columns: minmax(0, 1fr); } .episode-panel { order: 2; } .episode-list { display: grid; max-height: 280px; grid-template-columns: repeat(auto-fill, minmax(140px, 1fr)); } }
@media (max-width: 640px) { .watch-heading { grid-template-columns: 1fr; gap: 18px; } .watch-controls { justify-content: stretch; } .episode-select-label { min-width: 0; flex: 1; } h1 { font-size: 30px; } .player-container { min-height: 190px; } .player-status { align-items: flex-start; flex-direction: column; gap: 4px; } .watch-navigation .action-btn { min-width: 0; flex: 1; padding-inline: 8px; } }
</style>
