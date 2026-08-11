<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import { listAnimes } from '../api/anime'
import { scanLibrary } from '../api/library'
import { useAuthStore } from '../stores/auth'
import type { Anime } from '../types/anime'
import type { LibraryScanResult } from '../types/library'

const router = useRouter()
const authStore = useAuthStore()
const animes = ref<Anime[]>([])
const loading = ref(true)
const error = ref('')
const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const failedCovers = ref(new Set<number>())
const scanning = ref(false)
const scanResult = ref<LibraryScanResult | null>(null)
const scanError = ref('')

let searchTimer: ReturnType<typeof setTimeout> | undefined
// loadSerial 序列化 load()：搜索防抖、翻页、扫描完成刷新等入口可能并发触发，
// 过期响应到达时不得覆盖新数据（对齐 WatchView.vue 的机制）。
let loadSerial = 0

const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize))

async function load() {
  const serial = ++loadSerial
  loading.value = true
  error.value = ''
  try {
    const data = await listAnimes(page.value, pageSize, keyword.value.trim())
    if (serial !== loadSerial) return
    animes.value = data.items
    total.value = data.total
    failedCovers.value = new Set()
  } catch (e: unknown) {
    if (serial !== loadSerial) return
    error.value = e instanceof ApiError ? e.message : '加载番剧失败'
  } finally {
    if (serial === loadSerial) loading.value = false
  }
}

function onSearchInput() {
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => {
    page.value = 1
    void load()
  }, 300)
}

function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages() || nextPage === page.value) return
  page.value = nextPage
  void load()
}

function openAnime(id: number) {
  void router.push({ name: 'anime-detail', params: { id } })
}

function displayTitle(anime: Anime) {
  return anime.title_cn || anime.title
}

function markCoverFailed(id: number) {
  failedCovers.value = new Set(failedCovers.value).add(id)
}

function progressPercent(anime: Anime) {
  if (anime.ep_count <= 0) return 0
  return Math.min(100, Math.max(0, ((anime.watched_count ?? 0) / anime.ep_count) * 100))
}

async function handleLibraryScan() {
  scanning.value = true
  scanError.value = ''
  scanResult.value = null
  try {
    scanResult.value = await scanLibrary()
    await load()
  } catch (e: unknown) {
    scanError.value = e instanceof ApiError ? e.message : '库扫描失败'
  } finally {
    scanning.value = false
  }
}

onMounted(() => void load())
onBeforeUnmount(() => {
  ++loadSerial
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<template>
  <section class="anime-list-page" aria-labelledby="anime-list-title">
    <header class="page-header">
      <div class="page-heading">
        <p class="eyebrow">Library index</p>
        <h1 id="anime-list-title">番剧库</h1>
        <p class="page-description">浏览已收录内容，快速回到上次观看的位置。</p>
      </div>
      <div v-if="authStore.isAdmin" class="page-actions">
        <button type="button" class="action-btn" :disabled="scanning" @click="handleLibraryScan">
          {{ scanning ? '扫描中...' : '库扫描' }}
        </button>
        <button type="button" class="primary-btn" @click="router.push({ name: 'anime-add' })">添加番剧</button>
      </div>
    </header>

    <div class="library-toolbar">
      <div class="search-field">
        <label for="anime-search" class="search-label">搜索番剧</label>
        <input
          id="anime-search"
          v-model="keyword"
          class="search-input"
          type="search"
          placeholder="输入标题关键词"
          @input="onSearchInput"
        />
      </div>
      <p class="result-count" aria-live="polite">{{ loading ? '正在加载' : `共 ${total} 部` }}</p>
    </div>

    <section v-if="scanResult" class="scan-result" aria-live="polite">
      <div class="scan-result-heading">
        <div>
          <p class="scan-kicker">Library scan</p>
          <h2>库扫描完成</h2>
        </div>
        <button type="button" class="scan-close" @click="scanResult = null">关闭结果</button>
      </div>
      <div class="scan-stats">
        <div class="scan-stat">
          <strong>{{ scanResult.total_files }}</strong>
          <span>视频文件</span>
        </div>
        <div class="scan-stat">
          <strong>{{ scanResult.new_animes }}</strong>
          <span>新增番剧</span>
        </div>
        <div class="scan-stat">
          <strong>{{ scanResult.new_episodes }}</strong>
          <span>新增集数</span>
        </div>
        <div class="scan-stat" :class="{ 'scan-stat-warning': scanResult.unidentified.length > 0 }">
          <strong>{{ scanResult.unidentified.length }}</strong>
          <span>无法识别</span>
        </div>
      </div>
      <p class="scan-skipped">跳过 {{ scanResult.skipped }} 个已关联文件</p>
      <details v-if="scanResult.unidentified.length > 0" class="unidentified-details" open>
        <summary>查看无法识别文件</summary>
        <ul class="unidentified-list">
          <li v-for="file in scanResult.unidentified" :key="`${file.file_name}-${file.reason}`">
            <code>{{ file.file_name }}</code>
            <span>{{ file.reason }}</span>
          </li>
        </ul>
      </details>
    </section>

    <p v-if="error" class="error-msg" role="alert">{{ error }}</p>
    <p v-if="scanError" class="error-msg" role="alert">{{ scanError }}</p>

    <div v-if="loading" class="anime-grid" aria-live="polite" aria-label="正在加载番剧">
      <article v-for="index in 10" :key="index" class="anime-card skeleton-card">
        <div class="skeleton skeleton-cover"></div>
        <div class="card-info">
          <div class="skeleton skeleton-title-line"></div>
          <div class="skeleton skeleton-meta-line"></div>
        </div>
      </article>
    </div>
    <div v-else-if="animes.length === 0" class="empty-state">
      <template v-if="authStore.isAdmin">暂无番剧，点击“添加番剧”开始建立库</template>
      <template v-else>暂无番剧</template>
    </div>
    <div v-else class="anime-grid">
      <button
        v-for="anime in animes"
        :key="anime.id"
        type="button"
        class="anime-card"
        :aria-label="`打开${displayTitle(anime)}`"
        @click="openAnime(anime.id)"
      >
        <span class="card-cover-wrap">
          <img
            v-if="anime.cover && !failedCovers.has(anime.id)"
            :src="anime.cover"
            :alt="`${displayTitle(anime)}封面`"
            class="card-cover"
            loading="lazy"
            @error="markCoverFailed(anime.id)"
          />
          <span v-else class="card-cover placeholder-cover">无封面</span>
        </span>
        <span class="card-info">
          <span class="card-title" :title="displayTitle(anime)">{{ displayTitle(anime) }}</span>
          <span class="card-eps">
            {{ anime.ep_count > 0 ? `已看 ${anime.watched_count ?? 0} / 全${anime.ep_count}话` : `已看 ${anime.watched_count ?? 0} / 集数未知` }}
          </span>
        </span>
        <span
          v-if="anime.ep_count > 0"
          class="progress-track"
          role="progressbar"
          :aria-label="`观看进度 ${progressPercent(anime)}%`"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="progressPercent(anime)"
        >
          <span
            class="progress-fill"
            :class="{ complete: progressPercent(anime) >= 100 }"
            :style="{ width: `${progressPercent(anime)}%` }"
          ></span>
        </span>
      </button>
    </div>

    <div v-if="total > pageSize" class="pagination" aria-label="分页">
      <button type="button" class="pagination-button" :disabled="page <= 1" @click="changePage(page - 1)">
        上一页
      </button>
      <span aria-live="polite">{{ page }} / {{ totalPages() }}</span>
      <button type="button" class="pagination-button" :disabled="page >= totalPages()" @click="changePage(page + 1)">
        下一页
      </button>
    </div>
  </section>
</template>

<style scoped>
.anime-list-page {
  padding-bottom: 24px;
}

.page-header {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 24px;
  padding-bottom: 30px;
  border-bottom: 1px solid var(--border-color);
}

.page-heading h1 {
  color: var(--text-color);
  font-size: 34px;
  font-weight: 700;
  line-height: 1.15;
}

.page-description {
  margin-top: 10px;
  color: var(--text-secondary);
  font-size: 14px;
}

.page-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.library-toolbar {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 20px;
  padding: 24px 0 22px;
}

.search-field {
  width: min(100%, 520px);
}

.search-label {
  display: block;
  margin-bottom: 7px;
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

.search-input {
  width: 100%;
  min-height: 48px;
  padding: 0 14px;
  border: 1px solid var(--border-strong-color);
  border-radius: var(--radius-sm);
  outline: none;
  background: var(--surface-color);
  color: var(--text-color);
  font-size: 16px;
  transition: border-color 180ms ease-out, box-shadow 180ms ease-out, background-color 180ms ease-out;
}

.search-input::placeholder {
  color: var(--text-muted-color);
}

.search-input:focus {
  border-color: var(--accent-color);
  background: var(--surface-raised-color);
  box-shadow: 0 0 0 4px var(--accent-glow);
}

.result-count {
  padding-bottom: 12px;
  color: var(--text-muted-color);
  font-size: 13px;
  white-space: nowrap;
}

.scan-result {
  margin-bottom: 24px;
  padding: 20px;
  border: 1px solid var(--accent-soft-border);
  border-left: 3px solid var(--accent-color);
  border-radius: var(--radius-md);
  background: var(--surface-color);
  box-shadow: var(--shadow-sm);
}

.scan-result-heading {
  display: flex;
  align-items: start;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 20px;
}

.scan-kicker {
  margin-bottom: 5px;
  color: var(--accent-color);
  font-size: 11px;
  font-weight: 700;
  text-transform: uppercase;
}

.scan-result-heading h2 {
  color: var(--text-color);
  font-size: 18px;
}

.scan-close {
  min-height: 40px;
  padding: 0 10px;
  border: 0;
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.scan-close:hover {
  background: var(--surface-hover);
  color: var(--text-color);
}

.scan-stats {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 14px;
}

.scan-stat {
  min-width: 0;
  padding-left: 14px;
  border-left: 1px solid var(--border-color);
}

.scan-stat:first-child {
  padding-left: 0;
  border-left: 0;
}

.scan-stat strong {
  display: block;
  color: var(--text-color);
  font-size: 22px;
  font-weight: 700;
  line-height: 1.2;
}

.scan-stat span,
.scan-skipped {
  color: var(--text-secondary);
  font-size: 13px;
}

.scan-stat-warning strong,
.scan-stat-warning span {
  color: var(--warning-color);
}

.scan-skipped {
  margin-top: 18px;
  padding-top: 14px;
  border-top: 1px solid var(--border-color);
}

.unidentified-details {
  margin-top: 14px;
  border-top: 1px solid var(--border-color);
  color: var(--text-secondary);
  font-size: 13px;
}

.unidentified-details summary {
  padding-top: 14px;
  color: var(--warning-color);
  cursor: pointer;
}

.unidentified-list {
  display: flex;
  flex-direction: column;
  gap: 9px;
  margin-top: 12px;
  padding-left: 18px;
}

.unidentified-list li {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
}

.unidentified-list code {
  overflow-wrap: anywhere;
  color: var(--text-color);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.anime-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(178px, 1fr));
  gap: 22px 16px;
}

.anime-card {
  display: block;
  width: 100%;
  min-width: 0;
  overflow: hidden;
  padding: 0;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  outline: none;
  background: var(--surface-color);
  color: inherit;
  text-align: left;
  cursor: pointer;
  appearance: none;
  transition: border-color 200ms ease-out, background-color 200ms ease-out, box-shadow 200ms ease-out;
}

.anime-card:hover,
.anime-card:focus-visible {
  border-color: var(--accent-soft-border);
  background: var(--surface-raised-color);
  box-shadow: var(--shadow-md);
}

.card-cover-wrap {
  display: block;
  width: 100%;
  aspect-ratio: 2 / 3;
  overflow: hidden;
  background: var(--surface-muted-color);
}

.card-cover {
  display: block;
  width: 100%;
  height: 100%;
  object-fit: cover;
  transition: opacity 200ms ease-out;
}

.anime-card:hover .card-cover,
.anime-card:focus-visible .card-cover {
  opacity: 0.88;
}

.card-info {
  display: block;
  min-height: 76px;
  padding: 12px 12px 8px;
}

.card-title {
  display: -webkit-box;
  min-height: 42px;
  overflow: hidden;
  color: var(--text-color);
  font-size: 14px;
  font-weight: 600;
  line-height: 1.5;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.card-eps {
  display: block;
  margin-top: 6px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.progress-track {
  display: block;
  height: 4px;
  margin: 0 12px 12px;
  overflow: hidden;
  border-radius: 999px;
  background: var(--border-color);
}

.progress-fill {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: var(--accent-color);
  transition: width 240ms ease-out;
}

.progress-fill.complete {
  background: var(--success-color);
}

.skeleton-card {
  cursor: default;
  pointer-events: none;
}

.skeleton-card:hover {
  border-color: var(--border-color);
  background: var(--surface-color);
  box-shadow: none;
}

.skeleton-cover {
  width: 100%;
  aspect-ratio: 2 / 3;
  border-radius: 0;
}

.skeleton-title-line {
  width: 78%;
  height: 14px;
}

.skeleton-meta-line {
  width: 52%;
  height: 12px;
  margin-top: 10px;
}

.pagination {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  margin-top: 34px;
  color: var(--text-secondary);
  font-size: 14px;
}

.pagination-button {
  min-height: 44px;
  padding: 0 14px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-color);
  cursor: pointer;
  transition: background-color 180ms ease-out, border-color 180ms ease-out, color 180ms ease-out;
}

.pagination-button:hover:not(:disabled) {
  border-color: var(--accent-color);
  background: var(--surface-hover);
}

.pagination-button:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

@media (max-width: 760px) {
  .page-header,
  .library-toolbar {
    align-items: stretch;
    flex-direction: column;
  }

  .page-header {
    gap: 20px;
  }

  .page-actions > * {
    flex: 1;
  }

  .search-field {
    width: 100%;
  }

  .result-count {
    padding-bottom: 0;
  }
}

@media (max-width: 520px) {
  .page-heading h1 {
    font-size: 30px;
  }

  .scan-stats {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 16px 0;
  }

  .scan-stat:nth-child(3) {
    padding-left: 0;
    border-left: 0;
  }

  .anime-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 14px 10px;
  }

  .card-info {
    padding-right: 10px;
    padding-left: 10px;
  }

  .progress-track {
    margin-right: 10px;
    margin-left: 10px;
  }
}
</style>
