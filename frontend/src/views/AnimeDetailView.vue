<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../api'
import { deleteAnime, getAnime, listEpisodes, scanAnime, updateAnime } from '../api/anime'
import { getAnimeProgress } from '../api/progress'
import { useAuthStore } from '../stores/auth'
import type { Anime, Episode } from '../types/anime'
import type { AnimeProgress } from '../types/progress'

const route = useRoute()
const router = useRouter()
const authStore = useAuthStore()
const animeId = computed(() => Number(route.params.id))
const anime = ref<Anime | null>(null)
const episodes = ref<Episode[]>([])
const progressByEpisode = ref(new Map<number, AnimeProgress>())
const loading = ref(true)
const error = ref('')
const scanError = ref('')
const scanMessage = ref('')
const scanning = ref(false)
const saving = ref(false)
const showEdit = ref(false)
const coverFailed = ref(false)
const showFullSummary = ref(false)
const editForm = reactive({ title: '', title_cn: '', summary: '', ep_count: 0, file_path: '' })

async function load() {
  if (!Number.isInteger(animeId.value) || animeId.value <= 0) {
    error.value = '无效的番剧 ID'
    loading.value = false
    return
  }
  loading.value = true
  error.value = ''
  scanError.value = ''
  scanMessage.value = ''
  try {
    const [currentAnime, currentEpisodes, currentProgress] = await Promise.all([
      getAnime(animeId.value),
      listEpisodes(animeId.value),
      getAnimeProgress(animeId.value),
    ])
    anime.value = currentAnime
    episodes.value = currentEpisodes
    progressByEpisode.value = new Map(currentProgress.map((progress) => [progress.episode_id, progress]))
    coverFailed.value = false
    showFullSummary.value = false
  } catch (e: unknown) {
    error.value = e instanceof ApiError ? e.message : '加载番剧失败'
    anime.value = null
    episodes.value = []
    progressByEpisode.value = new Map()
  } finally {
    loading.value = false
  }
}

function openEdit() {
  if (!anime.value) return
  editForm.title = anime.value.title
  editForm.title_cn = anime.value.title_cn
  editForm.summary = anime.value.summary
  editForm.ep_count = anime.value.ep_count
  editForm.file_path = anime.value.file_path
  showEdit.value = true
}

async function saveEdit() {
  saving.value = true
  error.value = ''
  try {
    await updateAnime(animeId.value, { ...editForm })
    showEdit.value = false
    await load()
  } catch (e: unknown) {
    error.value = e instanceof ApiError ? e.message : '保存失败'
  } finally {
    saving.value = false
  }
}

async function handleScan() {
  scanning.value = true
  scanError.value = ''
  scanMessage.value = ''
  try {
    const result = await scanAnime(animeId.value)
    episodes.value = result.episodes
    await refreshProgress()
    scanMessage.value = `扫描完成，共识别 ${result.scanned} 集`
  } catch (e: unknown) {
    scanError.value = e instanceof ApiError ? e.message : '扫描失败'
  } finally {
    scanning.value = false
  }
}

async function handleDelete() {
  if (!window.confirm('确定删除这部番剧吗？关联的集数也会一并删除。')) return
  try {
    await deleteAnime(animeId.value)
    await router.replace({ name: 'anime-list' })
  } catch (e: unknown) {
    error.value = e instanceof ApiError ? e.message : '删除失败'
  }
}

function displayTitle() {
  return anime.value?.title_cn || anime.value?.title || ''
}

const watchedCount = computed(() => {
  let count = 0
  for (const progress of progressByEpisode.value.values()) {
    if (progress.watched) count += 1
  }
  return count
})

const progressTotal = computed(() => {
  if (anime.value?.ep_count && anime.value.ep_count > 0) return anime.value.ep_count
  return episodes.value.length
})

const progressPercent = computed(() => {
  if (progressTotal.value <= 0) return 0
  return Math.min(100, Math.max(0, Math.round((watchedCount.value / progressTotal.value) * 100)))
})

function episodeStatus(episode: Episode) {
  const progress = progressByEpisode.value.get(episode.id)
  if (progress?.watched) return '已看'
  if ((progress?.position || 0) > 0) return '进行中'
  return '未看'
}

const continueEpisode = computed(() => {
  const inProgress = episodes.value.find((episode) => {
    const progress = progressByEpisode.value.get(episode.id)
    return !progress?.watched && (progress?.position || 0) > 0
  })
  return inProgress || episodes.value.find((episode) => !progressByEpisode.value.get(episode.id)?.watched) || null
})

const summaryExpandable = computed(() => {
  const summary = anime.value?.summary || ''
  return summary.split(/\r?\n/).length > 6 || summary.length > 260
})

function openEpisode(episodeID: number) {
  void router.push({ name: 'watch', params: { id: animeId.value, epId: episodeID } })
}

async function refreshProgress() {
  try {
    const currentProgress = await getAnimeProgress(animeId.value)
    progressByEpisode.value = new Map(currentProgress.map((progress) => [progress.episode_id, progress]))
  } catch (e: unknown) {
    if (!scanError.value) scanError.value = e instanceof ApiError ? e.message : '刷新观看进度失败'
  }
}

watch(animeId, () => void load(), { immediate: true })
</script>

<template>
  <section class="detail-page" aria-labelledby="detail-title">
    <p v-if="error" class="error-msg" role="alert">{{ error }}</p>
    <div v-if="loading" class="empty-state" aria-live="polite">加载中...</div>
    <div v-else-if="!anime" class="empty-state">番剧不存在</div>
    <template v-else>
      <header class="detail-hero">
        <div class="detail-cover-wrap">
          <img
            v-if="anime.cover && !coverFailed"
            :src="anime.cover"
            :alt="`${displayTitle()}封面`"
            class="detail-cover"
            @error="coverFailed = true"
          />
          <div v-else class="detail-cover placeholder-cover">无封面</div>
        </div>
        <div class="detail-info">
          <div class="detail-kicker-row">
            <p class="eyebrow">番剧详情</p>
            <span class="detail-state">本地收藏</span>
          </div>
          <h1 id="detail-title">{{ displayTitle() }}</h1>
          <p v-if="anime.title_cn && anime.title" class="sub-title">{{ anime.title }}</p>
          <p class="meta">{{ anime.ep_count > 0 ? `全${anime.ep_count}话` : '集数未知' }}<span v-if="anime.file_path"> · {{ anime.file_path }}</span></p>
          <div class="detail-stats" aria-label="番剧统计">
            <div class="detail-stat">
              <span>观看进度</span>
              <strong>{{ watchedCount }} / {{ progressTotal }}</strong>
            </div>
            <div class="detail-stat">
              <span>当前状态</span>
              <strong>{{ continueEpisode ? `第 ${continueEpisode.ep_number} 话` : '已看完' }}</strong>
            </div>
            <div class="detail-stat">
              <span>已入库集数</span>
              <strong>{{ episodes.length }} 集</strong>
            </div>
          </div>
          <div class="detail-progress">
            <div class="progress-meta">
              <span>观看完成度</span>
              <strong>{{ progressPercent }}%</strong>
            </div>
            <div
              class="progress-track"
              role="progressbar"
              aria-label="观看完成度"
              aria-valuemin="0"
              aria-valuemax="100"
              :aria-valuenow="progressPercent"
            >
              <span class="progress-fill" :style="{ width: `${progressPercent}%` }"></span>
            </div>
          </div>
          <div class="actions">
            <button v-if="continueEpisode" type="button" class="primary-btn" @click="openEpisode(continueEpisode.id)">
              继续播放
            </button>
            <template v-if="authStore.isAdmin">
              <button type="button" class="action-btn" @click="openEdit">编辑信息</button>
              <button type="button" class="action-btn" :disabled="scanning" @click="handleScan">
                {{ scanning ? '扫描中...' : '扫描文件' }}
              </button>
              <button type="button" class="action-btn danger" @click="handleDelete">删除番剧</button>
            </template>
          </div>
          <p v-if="scanMessage" class="scan-msg" role="status">{{ scanMessage }}</p>
          <p v-if="scanError" class="error-msg" role="alert">{{ scanError }}</p>
        </div>
      </header>

      <section class="detail-summary" aria-labelledby="summary-title">
        <div class="section-heading">
          <div>
            <p class="section-kicker">Synopsis</p>
            <h2 id="summary-title">简介</h2>
          </div>
          <button v-if="summaryExpandable" type="button" class="summary-toggle" @click="showFullSummary = !showFullSummary">
            {{ showFullSummary ? '收起简介' : '展开简介' }}
          </button>
        </div>
        <p class="summary" :class="{ 'summary-collapsed': summaryExpandable && !showFullSummary }">
          {{ anime.summary || '暂无简介' }}
        </p>
      </section>

      <form v-if="showEdit && authStore.isAdmin" class="edit-form" @submit.prevent="saveEdit">
        <div class="edit-heading">
          <div>
            <p class="section-kicker">Metadata</p>
            <h2 id="edit-form-title">编辑番剧</h2>
          </div>
          <button type="button" class="edit-dismiss" @click="showEdit = false">取消编辑</button>
        </div>
        <div class="edit-grid">
          <div class="form-field"><label for="edit-title">原名</label><input id="edit-title" v-model="editForm.title" type="text" required /></div>
          <div class="form-field"><label for="edit-title-cn">中文标题</label><input id="edit-title-cn" v-model="editForm.title_cn" type="text" /></div>
          <div class="form-field edit-field-wide"><label for="edit-summary">简介</label><textarea id="edit-summary" v-model="editForm.summary" rows="5"></textarea></div>
          <div class="form-field"><label for="edit-ep-count">总集数</label><input id="edit-ep-count" v-model.number="editForm.ep_count" type="number" min="0" /></div>
          <div class="form-field"><label for="edit-file-path">文件目录</label><input id="edit-file-path" v-model="editForm.file_path" type="text" placeholder="留空则扫描根目录" /></div>
        </div>
        <div class="form-actions">
          <button type="submit" class="primary-btn" :disabled="saving">{{ saving ? '保存中...' : '保存修改' }}</button>
          <button type="button" class="action-btn" @click="showEdit = false">取消</button>
        </div>
      </form>

      <section class="episode-section" aria-labelledby="episodes-title">
        <div class="episode-heading">
          <div>
            <p class="section-kicker">Episodes</p>
            <h2 id="episodes-title">集数列表</h2>
          </div>
          <span class="episode-count">{{ episodes.length }} 集已入库</span>
          <button v-if="continueEpisode" type="button" class="primary-btn" @click="openEpisode(continueEpisode.id)">
            从第 {{ continueEpisode.ep_number }} 话继续
          </button>
        </div>
        <div v-if="episodes.length === 0" class="empty-state">暂无集数，请扫描文件</div>
        <div v-else class="episode-grid">
          <button
            v-for="episode in episodes"
            :key="episode.id"
            type="button"
            class="episode-tile"
            :class="`tile-${episodeStatus(episode)}`"
            :aria-label="`第 ${episode.ep_number} 话，${episodeStatus(episode)}`"
            @click="openEpisode(episode.id)"
          >
            <span class="episode-number">第 {{ episode.ep_number }} 话</span>
            <span class="episode-status" :class="`status-${episodeStatus(episode)}`">{{ episodeStatus(episode) }}</span>
          </button>
        </div>
      </section>
    </template>
  </section>
</template>

<style scoped>
.detail-page { max-width: 1180px; margin: 0 auto; padding-bottom: 32px; }
.detail-hero { display: grid; grid-template-columns: 220px minmax(0, 1fr); gap: 34px; padding: 12px 0 40px; border-bottom: 1px solid var(--border-color); }
.detail-cover-wrap { width: 220px; aspect-ratio: 2 / 3; overflow: hidden; border: 1px solid var(--border-color); border-radius: var(--radius-lg); background: var(--surface-color); box-shadow: var(--shadow-lg); }
.detail-cover { display: flex; width: 100%; height: 100%; align-items: center; justify-content: center; object-fit: cover; color: var(--text-muted-color); font-size: 13px; }
.detail-info { min-width: 0; align-self: center; }
.detail-kicker-row { display: flex; align-items: center; gap: 10px; }
.detail-kicker-row .eyebrow { margin-bottom: 0; }
.detail-state { padding: 5px 9px; border: 1px solid var(--primary-soft-border); border-radius: 999px; background: var(--primary-soft-bg); color: var(--primary-hover-color); font-size: 12px; }
h1 { max-width: 780px; margin: 14px 0 8px; color: var(--text-color); font-size: 40px; font-weight: 700; line-height: 1.15; overflow-wrap: anywhere; }
.sub-title { margin-bottom: 10px; color: var(--text-secondary); font-size: 15px; }
.meta { max-width: 800px; margin-bottom: 24px; overflow-wrap: anywhere; color: var(--text-secondary); font-size: 14px; }
.detail-stats { display: grid; max-width: 680px; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 14px; margin-bottom: 22px; }
.detail-stat { min-width: 0; padding-left: 14px; border-left: 1px solid var(--border-color); }
.detail-stat:first-child { padding-left: 0; border-left: 0; }
.detail-stat span { display: block; color: var(--text-muted-color); font-size: 12px; }
.detail-stat strong { display: block; margin-top: 6px; overflow: hidden; color: var(--text-color); font-size: 15px; text-overflow: ellipsis; white-space: nowrap; }
.detail-progress { max-width: 680px; margin-bottom: 24px; }
.progress-meta { display: flex; justify-content: space-between; gap: 12px; margin-bottom: 8px; color: var(--text-secondary); font-size: 12px; }
.progress-meta strong { color: var(--accent-color); }
.progress-track { height: 6px; overflow: hidden; border-radius: 999px; background: var(--border-color); }
.progress-fill { display: block; height: 100%; border-radius: inherit; background: var(--accent-color); transition: width 240ms ease-out; }
.actions, .form-actions { display: flex; flex-wrap: wrap; gap: 10px; }
.scan-msg { margin-top: 12px; color: var(--success-color); font-size: 13px; }
.detail-summary { padding: 32px 0; border-bottom: 1px solid var(--border-color); }
.section-heading { display: flex; align-items: end; justify-content: space-between; gap: 18px; margin-bottom: 14px; }
.section-kicker { margin-bottom: 5px; color: var(--text-muted-color); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.section-heading h2, .edit-heading h2, .episode-heading h2 { color: var(--text-color); font-size: 21px; font-weight: 700; }
.summary { max-width: 820px; color: var(--text-secondary); font-size: 15px; line-height: 1.85; white-space: pre-wrap; }
.summary-collapsed { display: -webkit-box; overflow: hidden; -webkit-box-orient: vertical; -webkit-line-clamp: 6; }
.summary-toggle, .edit-dismiss { min-height: 40px; padding: 0 10px; border: 0; border-radius: var(--radius-sm); background: transparent; color: var(--accent-color); font-size: 13px; cursor: pointer; }
.summary-toggle:hover, .edit-dismiss:hover { background: var(--surface-hover); color: var(--accent-hover-text); }
.edit-form { margin: 32px 0; padding: 22px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-sm); }
.edit-heading { display: flex; align-items: start; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.edit-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; }
.edit-field-wide { grid-column: 1 / -1; }
.form-field label { display: block; margin-bottom: 7px; color: var(--text-secondary); font-size: 13px; font-weight: 600; }
.form-field input, .form-field textarea { width: 100%; min-height: 46px; padding: 10px 12px; border: 1px solid var(--border-strong-color); border-radius: var(--radius-sm); background: var(--surface-muted-color); color: var(--text-color); outline: none; font: inherit; resize: vertical; transition: border-color 180ms ease-out, box-shadow 180ms ease-out; }
.form-field textarea { min-height: 124px; }
.form-field input:focus, .form-field textarea:focus { border-color: var(--accent-color); box-shadow: 0 0 0 4px var(--accent-glow); }
.form-actions { margin-top: 20px; }
.episode-section { padding-top: 32px; }
.episode-heading { display: flex; align-items: end; gap: 18px; margin-bottom: 18px; }
.episode-count { margin-right: auto; color: var(--text-muted-color); font-size: 13px; }
.episode-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(116px, 1fr)); gap: 10px; }
.episode-tile { display: flex; min-height: 72px; flex-direction: column; align-items: flex-start; justify-content: space-between; gap: 8px; padding: 12px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); color: var(--text-color); text-align: left; cursor: pointer; transition: background-color 180ms ease-out, border-color 180ms ease-out, box-shadow 180ms ease-out; }
.episode-tile:hover, .episode-tile:focus-visible { border-color: var(--accent-color); background: var(--surface-raised-color); box-shadow: var(--shadow-sm); outline: none; }
.episode-tile.tile-已看 { border-color: var(--success-border); }
.episode-tile.tile-进行中 { border-color: var(--warning-border); }
.episode-number { font-size: 14px; font-weight: 600; }
.episode-status { font-size: 12px; }
.status-已看 { color: var(--success-color); }
.status-进行中 { color: var(--warning-color); }
.status-未看 { color: var(--text-muted-color); }
@media (max-width: 760px) { .detail-hero { grid-template-columns: 160px minmax(0, 1fr); gap: 24px; } .detail-cover-wrap { width: 160px; } h1 { font-size: 32px; } .episode-heading { align-items: flex-start; flex-wrap: wrap; } .episode-heading .episode-count { margin-right: 0; } }
@media (max-width: 560px) { .detail-hero { display: block; } .detail-cover-wrap { width: 148px; margin-bottom: 24px; } h1 { font-size: 30px; } .detail-stats { gap: 8px; } .detail-stat { padding-left: 8px; } .detail-stat strong { font-size: 13px; } .edit-grid { grid-template-columns: 1fr; } .edit-field-wide { grid-column: auto; } .section-heading { align-items: flex-start; flex-direction: column; gap: 8px; } .episode-heading .primary-btn { width: 100%; } }
</style>
