<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import { listAnimes } from '../api/anime'
import {
  ConfirmInboxError,
  confirmInbox,
  getLibraryScan,
  listUnidentified,
  startLibraryScan,
} from '../api/library'
import InboxFileCard, { type InboxCardError, type InboxConfirmPayload } from '../components/InboxFileCard.vue'
import { useAuthStore } from '../stores/auth'
import type { Anime } from '../types/anime'
import type { ConfirmInboxRequest, LibraryScanResult, UnidentifiedFile } from '../types/library'

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
const confirmingPath = ref<string | null>(null)
const inboxItems = ref<UnidentifiedFile[]>([])
const inboxTotal = ref(0)
// 确认失败只渲染在对应卡片内（key 见 inboxKey），不再写页面底部 scanError。
const cardErrors = ref<Record<string, InboxCardError>>({})

let searchTimer: ReturnType<typeof setTimeout> | undefined
// loadSerial 序列化 load()：搜索防抖、翻页、扫描完成刷新等入口可能并发触发，
// 过期响应到达时不得覆盖新数据（对齐 WatchView.vue 的机制）。
let loadSerial = 0
let scanSerial = 0

const SCAN_POLL_INTERVAL_MS = 1000
const SCAN_POLL_TIMEOUT_MS = 15 * 60 * 1000

function wait(ms: number) {
  return new Promise<void>((resolve) => {
    setTimeout(resolve, ms)
  })
}

const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize))

async function load(silent = false) {
  const serial = ++loadSerial
  if (!silent) loading.value = true
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
    // 无论是否静默刷新，只要是"最后一次发起"的加载都必须收起 loading：
    // silent 刷新会 ++loadSerial，若此时普通 load() 还在飞，两边 serial 都不
    // 匹配，loading 会永久停在 true（"正在加载"一直转）。
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

async function loadInbox() {
  if (!authStore.isAdmin) {
    inboxItems.value = []
    inboxTotal.value = 0
    return
  }
  try {
    const listed = await listUnidentified(1, 100)
    inboxItems.value = listed.items
    inboxTotal.value = listed.total
  } catch (e: unknown) {
    inboxItems.value = []
    inboxTotal.value = 0
    scanError.value = e instanceof ApiError ? e.message : '加载未识别文件失败'
  }
}

async function applyScanResult(result: LibraryScanResult) {
  await loadInbox()
  scanResult.value = { ...result, unidentified: inboxItems.value }
  await load(true)
}

async function handleLibraryScan() {
  const serial = ++scanSerial
  scanning.value = true
  scanError.value = ''
  scanResult.value = null
  const startedAt = Date.now()
  try {
    let job = await startLibraryScan()
    while (job.state !== 'done' && job.state !== 'error') {
      if (serial !== scanSerial) return
      if (Date.now() - startedAt >= SCAN_POLL_TIMEOUT_MS) {
        scanError.value = '扫描仍在服务器执行，已停止前端轮询'
        return
      }
      job = await getLibraryScan()
      if (job.state === 'done' || job.state === 'error') break
      await wait(SCAN_POLL_INTERVAL_MS)
    }
    if (serial !== scanSerial) return
    if (job.state === 'error') {
      scanError.value = job.error || '库扫描失败'
      return
    }
    if (!job.result) {
      scanError.value = '扫描结果缺失'
      return
    }
    await applyScanResult(job.result)
  } catch (e: unknown) {
    if (serial !== scanSerial) return
    scanError.value = e instanceof ApiError ? e.message : '库扫描失败'
  } finally {
    if (serial === scanSerial) scanning.value = false
  }
}

function inboxKey(file: UnidentifiedFile) {
  return `${file.file_path}/${file.file_name}`
}

function isConfirming(filePath: string) {
  return confirmingPath.value !== null && confirmingPath.value === filePath
}

// 默认批量范围：同 file_path 且候选列表含该 bangumi id 的全部收件箱文件；
// 被点击的文件本身总是包含在内（手动搜索选中的条目可能不在任何候选里）。
function siblingFileNames(file: UnidentifiedFile, bangumiId: number) {
  const names: string[] = []
  for (const item of inboxItems.value) {
    if (item.file_path !== file.file_path) continue
    if (!(item.candidates ?? []).some((candidate) => candidate.id === bangumiId)) continue
    if (!names.includes(item.file_name)) names.push(item.file_name)
  }
  if (!names.includes(file.file_name)) names.unshift(file.file_name)
  return names
}

function siblingCountFor(file: UnidentifiedFile) {
  return (bangumiId: number) => siblingFileNames(file, bangumiId).length
}

function setCardError(file: UnidentifiedFile, cardError: InboxCardError | null) {
  const next = { ...cardErrors.value }
  if (cardError) next[inboxKey(file)] = cardError
  else delete next[inboxKey(file)]
  cardErrors.value = next
}

function removeConfirmedFiles(filePath: string, fileNames: string[]) {
  const confirmed = new Set(fileNames)
  const isConfirmed = (item: UnidentifiedFile) => item.file_path === filePath && confirmed.has(item.file_name)
  const removed = inboxItems.value.filter(isConfirmed).length
  inboxItems.value = inboxItems.value.filter((item) => !isConfirmed(item))
  inboxTotal.value = Math.max(0, inboxTotal.value - removed)
  const nextErrors = { ...cardErrors.value }
  for (const name of fileNames) delete nextErrors[`${filePath}/${name}`]
  cardErrors.value = nextErrors
}

function toCardError(e: unknown): InboxCardError {
  if (e instanceof ConfirmInboxError) {
    return { message: e.message, reason: e.reason, file_names: e.file_names }
  }
  return { message: e instanceof ApiError ? e.message : '确认失败' }
}

// 收件箱确认走原子接口 confirmInbox：建档 + 限定文件扫描 + 失败回滚都在服务端完成。
// 失败时不再调用 createAnime / scanAnime（旧两步流程已废弃），错误只渲染在该卡片内。
async function confirmCandidate(file: UnidentifiedFile, payload: InboxConfirmPayload) {
  if (isConfirming(file.file_path)) return
  confirmingPath.value = file.file_path
  setCardError(file, null)
  const fileNames = payload.scope === 'single' ? [file.file_name] : siblingFileNames(file, payload.bangumiId)
  const req: ConfirmInboxRequest = {
    bangumi_id: payload.bangumiId,
    file_path: file.file_path,
    file_names: fileNames,
  }
  if (payload.episodeOffset !== undefined) req.episode_offset = payload.episodeOffset
  try {
    await confirmInbox(req)
    removeConfirmedFiles(file.file_path, fileNames)
    if (scanResult.value) {
      scanResult.value = { ...scanResult.value, unidentified: inboxItems.value }
    }
    await load()
  } catch (e: unknown) {
    setCardError(file, toCardError(e))
  } finally {
    if (confirmingPath.value === file.file_path) confirmingPath.value = null
  }
}

onMounted(() => {
  void load()
  void loadInbox()
})
onBeforeUnmount(() => {
  ++loadSerial
  ++scanSerial
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
        <div class="scan-stat" :class="{ 'scan-stat-warning': inboxTotal > 0 }">
          <strong>{{ inboxTotal }}</strong>
          <span>无法识别</span>
        </div>
      </div>
      <p class="scan-skipped">跳过 {{ scanResult.skipped }} 个已关联文件</p>
      <details v-if="inboxItems.length > 0" class="unidentified-details" open>
        <summary>查看无法识别文件（{{ inboxTotal }}）</summary>
        <ul class="unidentified-list">
          <li v-for="file in inboxItems" :key="`${file.file_path}/${file.file_name}-${file.reason}`">
            <InboxFileCard
              :file="file"
              :sibling-count="siblingCountFor(file)"
              :confirming="isConfirming(file.file_path)"
              :error="cardErrors[inboxKey(file)] ?? null"
              @confirm="confirmCandidate(file, $event)"
            />
          </li>
        </ul>
      </details>
    </section>

    <section
      v-if="authStore.isAdmin && !scanResult && inboxItems.length > 0"
      class="scan-result"
      aria-label="未识别文件"
    >
      <details class="unidentified-details" open>
        <summary>无法识别文件（{{ inboxTotal }}）</summary>
        <ul class="unidentified-list">
          <li v-for="file in inboxItems" :key="`inbox-${file.file_path}/${file.file_name}-${file.reason}`">
            <InboxFileCard
              :file="file"
              :sibling-count="siblingCountFor(file)"
              :confirming="isConfirming(file.file_path)"
              :error="cardErrors[inboxKey(file)] ?? null"
              @confirm="confirmCandidate(file, $event)"
            />
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
