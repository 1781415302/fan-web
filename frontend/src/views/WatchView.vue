<script setup lang="ts">
import Artplayer from 'artplayer'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../api'
import { getStreamUrl, getSubtitleTracks, getSubtitleUrl, type SubtitleTrack } from '../api/episode'
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
const playbackRate = ref<PlaybackRateOption>(1)
const subtitleTracks = ref<SubtitleTrack[]>([])
const playerContainer = ref<HTMLDivElement | null>(null)

let player: Artplayer | null = null
let activeEpisode: Episode | null = null
let progressTimer: ReturnType<typeof setInterval> | undefined
let loadSerial = 0
let reportChain: Promise<void> = Promise.resolve()

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
        updateProgressState(episode.id, position, watched)
        progressError.value = ''
      } catch (e: unknown) {
        progressError.value = e instanceof ApiError ? e.message : '播放进度保存失败'
      }
    })
  return reportChain
}

function destroyPlayer() {
  stopProgressTimer()
  if (!player) return

  const oldPlayer = player
  if (activeEpisode) {
    void queueProgressReport(oldPlayer, activeEpisode)
  }
  player = null
  activeEpisode = null
  oldPlayer.destroy()
}

const emptySubtitleUrl = 'data:text/vtt;charset=utf-8,WEBVTT%0A%0A'

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
        html: track.label,
        value: track.track_number,
        default: index === 0,
      })),
    ],
    onSelect(this: Artplayer, selector: { value?: string | number }) {
      if (selector.value === 'off') {
        void this.subtitle.switch(emptySubtitleUrl, { name: '关闭字幕', type: 'vtt' })
        return '字幕'
      }
      const track = tracks.find((item) => item.track_number === Number(selector.value))
      if (!track) return '字幕'
      void this.subtitle.switch(getSubtitleUrl(episode.id, track.track_number), {
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
    url: getStreamUrl(episode.id),
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
          url: getSubtitleUrl(episode.id, defaultSubtitle.track_number),
          name: defaultSubtitle.label,
          type: 'vtt',
        }
      : undefined,
    controls: subtitleTracks.value.length ? [createSubtitleControl(episode)] : [],
  })

  player = instance
  activeEpisode = episode
  instance.playbackRate = playbackRate.value
  playerError.value = ''
  progressError.value = ''
  statusMessage.value = currentProgress.value?.position ? '正在恢复播放位置' : '准备播放'

  instance.on('video:loadedmetadata', () => {
    if (player !== instance) return
    currentDuration.value = Number.isFinite(instance.duration) ? instance.duration : 0
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
    if (player === instance) playerError.value = '视频加载失败，请检查视频文件或网络连接'
  })
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
    const [currentAnime, currentEpisodes, progressList, availableSubtitleTracks] = await Promise.all([
      getAnime(animeId.value),
      listEpisodes(animeId.value),
      getAnimeProgress(animeId.value),
      getSubtitleTracks(episodeId.value).catch(() => []),
    ])
    if (serial !== loadSerial) return

    anime.value = currentAnime
    episodes.value = currentEpisodes
    subtitleTracks.value = availableSubtitleTracks
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
