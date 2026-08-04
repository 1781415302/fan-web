<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../api'
import { deleteAnime, getAnime, listEpisodes, scanAnime, updateAnime } from '../api/anime'
import { getAnimeProgress } from '../api/progress'
import type { Anime, Episode } from '../types/anime'
import type { AnimeProgress } from '../types/progress'

const route = useRoute()
const router = useRouter()
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

function episodeStatus(episode: Episode) {
  const progress = progressByEpisode.value.get(episode.id)
  if (progress?.watched) return '已看'
  if ((progress?.position || 0) > 0) return '进行中'
  return '未看'
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
      <div class="detail-header">
        <img
          v-if="anime.cover && !coverFailed"
          :src="anime.cover"
          :alt="`${displayTitle()}封面`"
          class="detail-cover"
          @error="coverFailed = true"
        />
        <div v-else class="detail-cover placeholder-cover">无封面</div>
        <div class="detail-info">
          <p class="eyebrow">番剧详情</p>
          <h1 id="detail-title">{{ displayTitle() }}</h1>
          <p v-if="anime.title_cn && anime.title" class="sub-title">{{ anime.title }}</p>
          <p class="meta">
            <span>已观看 {{ watchedCount }}/{{ progressTotal }}</span>
            <span> · {{ anime.ep_count > 0 ? `全${anime.ep_count}话` : '集数未知' }}</span>
            <span v-if="anime.file_path"> · 目录：{{ anime.file_path }}</span>
          </p>
          <p class="summary">{{ anime.summary || '暂无简介' }}</p>
          <div class="actions">
            <button type="button" class="action-btn" @click="openEdit">编辑</button>
            <button type="button" class="action-btn" :disabled="scanning" @click="handleScan">
              {{ scanning ? '扫描中...' : '扫描文件' }}
            </button>
            <button type="button" class="action-btn danger" @click="handleDelete">删除</button>
          </div>
          <p v-if="scanMessage" class="scan-msg">{{ scanMessage }}</p>
          <p v-if="scanError" class="error-msg" role="alert">{{ scanError }}</p>
        </div>
      </div>

      <form v-if="showEdit" class="edit-form" @submit.prevent="saveEdit">
        <h2>编辑番剧</h2>
        <div class="form-field"><label for="edit-title">标题</label><input id="edit-title" v-model="editForm.title" type="text" required /></div>
        <div class="form-field"><label for="edit-title-cn">中文标题</label><input id="edit-title-cn" v-model="editForm.title_cn" type="text" /></div>
        <div class="form-field"><label for="edit-summary">简介</label><textarea id="edit-summary" v-model="editForm.summary" rows="4"></textarea></div>
        <div class="form-field"><label for="edit-ep-count">总集数</label><input id="edit-ep-count" v-model.number="editForm.ep_count" type="number" min="0" /></div>
        <div class="form-field"><label for="edit-file-path">文件目录</label><input id="edit-file-path" v-model="editForm.file_path" type="text" placeholder="留空则扫描根目录" /></div>
        <div class="form-actions">
          <button type="submit" class="action-btn" :disabled="saving">{{ saving ? '保存中...' : '保存' }}</button>
          <button type="button" class="action-btn" @click="showEdit = false">取消</button>
        </div>
      </form>

      <div class="episode-section">
          <h2>集数列表（{{ episodes.length }}）</h2>
        <div v-if="episodes.length === 0" class="empty-state">暂无集数，请扫描文件</div>
        <div v-else class="table-scroll">
          <table class="episode-table">
            <thead><tr><th>集数</th><th>文件名</th><th>状态</th><th>操作</th></tr></thead>
            <tbody>
              <tr v-for="episode in episodes" :key="episode.id">
                <td class="ep-num">第 {{ episode.ep_number }} 话</td>
                <td class="ep-file">{{ episode.file_path }}</td>
                <td><span class="episode-status" :class="`status-${episodeStatus(episode)}`">{{ episodeStatus(episode) }}</span></td>
                <td><router-link class="play-link" :to="{ name: 'watch', params: { id: anime.id, epId: episode.id } }">播放</router-link></td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>
    </template>
  </section>
</template>

<style scoped>
.detail-page { padding-bottom: 40px; }
.eyebrow { color: var(--primary-hover-color); font-size: 13px; margin-bottom: 2px; }
.error-msg { margin-bottom: 16px; color: #f87171; font-size: 14px; }
.detail-header { display: flex; gap: 24px; margin-bottom: 32px; }
.detail-cover { flex: 0 0 180px; width: 180px; aspect-ratio: 2 / 3; object-fit: cover; border-radius: 8px; }
.placeholder-cover { display: flex; align-items: center; justify-content: center; background: var(--surface-color); color: var(--text-secondary); font-size: 14px; }
.detail-info { min-width: 0; flex: 1; }
h1 { margin-bottom: 4px; color: var(--text-color); font-size: 24px; overflow-wrap: anywhere; }
.sub-title { margin-bottom: 8px; color: var(--text-secondary); font-size: 14px; }
.meta { margin-bottom: 12px; color: var(--text-secondary); font-size: 14px; }
.summary { max-width: 760px; margin-bottom: 16px; color: var(--text-color); font-size: 14px; line-height: 1.8; white-space: pre-wrap; }
.actions, .form-actions { display: flex; flex-wrap: wrap; gap: 10px; }
.action-btn { padding: 6px 14px; border: 1px solid var(--border-color); border-radius: 4px; background: transparent; color: var(--text-color); font-size: 14px; cursor: pointer; }
.action-btn:hover { border-color: var(--primary-color); }
.action-btn:disabled { opacity: 0.5; cursor: wait; }
.action-btn.danger { border-color: #7f1d1d; color: #f87171; }
.action-btn.danger:hover { border-color: #ef4444; background: #450a0a; }
.scan-msg { margin-top: 8px; color: var(--text-secondary); font-size: 14px; }
.edit-form { margin-bottom: 32px; padding: 20px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--surface-color); }
.edit-form h2, .episode-section h2 { margin-bottom: 16px; font-size: 18px; }
.form-field { margin-bottom: 14px; }
.form-field label { display: block; margin-bottom: 4px; color: var(--text-secondary); font-size: 14px; }
.form-field input, .form-field textarea { width: 100%; padding: 8px 12px; border: 1px solid var(--border-color); border-radius: 4px; background: var(--bg-color); color: var(--text-color); outline: none; font: inherit; }
.form-field input:focus, .form-field textarea:focus { border-color: var(--primary-color); }
.table-scroll { overflow-x: auto; }
.episode-table { width: 100%; min-width: 640px; border-collapse: collapse; }
.episode-table th, .episode-table td { padding: 10px 14px; border-bottom: 1px solid var(--border-color); text-align: left; font-size: 14px; }
.episode-table th { color: var(--text-secondary); font-weight: 600; }
.ep-num { white-space: nowrap; color: var(--primary-hover-color); }
.ep-file { max-width: 520px; color: var(--text-secondary); overflow-wrap: anywhere; }
.episode-status { font-size: 12px; white-space: nowrap; }
.status-已看 { color: #86efac; }
.status-进行中 { color: #facc15; }
.status-未看 { color: var(--text-secondary); }
.play-link { color: var(--primary-hover-color); font-size: 13px; white-space: nowrap; }
.empty-state { padding: 40px 0; color: var(--text-secondary); text-align: center; }
@media (max-width: 640px) { .detail-header { flex-direction: column; } .detail-cover { flex-basis: auto; width: 140px; } }
</style>
