<script setup lang="ts">
import { computed, ref, watch } from 'vue'

import { ApiError } from '../api'
import { searchBangumi } from '../api/bangumi'
import type { BangumiSearchItem } from '../types/anime'
import type { ConfirmErrorData, MatchCandidate, UnidentifiedFile } from '../types/library'

// 单个收件箱文件卡片：候选按钮（年份/话数）、手动搜索、集号偏移、卡片内联错误。
// 卡片只负责交互与收集参数；同目录批量的 file_names 计算与 confirmInbox 调用由父视图完成，
// 因为它需要看到整份收件箱列表。

export interface InboxCardError {
  message: string
  reason?: ConfirmErrorData['reason']
  file_names?: string[]
}

export interface InboxConfirmPayload {
  bangumiId: number
  // siblings：同 file_path 且候选含该条目的全部收件箱文件；single：仅此文件。
  scope: 'siblings' | 'single'
  episodeOffset?: number
}

const props = defineProps<{
  file: UnidentifiedFile
  // 该条目在同目录下会被一并提交的文件数（含本文件）。
  siblingCount: (bangumiId: number) => number
  confirming: boolean
  error: InboxCardError | null
}>()

const emit = defineEmits<{
  confirm: [payload: InboxConfirmPayload]
}>()

const showSearch = ref(false)
const searchKeyword = ref('')
const searching = ref(false)
const searchError = ref('')
const searchResults = ref<BangumiSearchItem[]>([])
const searched = ref(false)
const showOffset = ref(false)
const offsetInput = ref('')
const offsetError = ref('')

const candidates = computed<MatchCandidate[]>(() => props.file.candidates ?? [])

// 失败为 review_required（含偏移不适用）后展开偏移输入；其它原因不自动展开。
watch(
  () => props.error,
  (next) => {
    if (next?.reason === 'review_required') showOffset.value = true
  },
)

function candidateLabel(candidate: MatchCandidate) {
  return candidate.name_cn || candidate.name
}

// 旧收件箱数据没有 year / ep_count（读出为缺省或 0），缺失时整段不显示。
function candidateMeta(candidate: MatchCandidate) {
  const parts: string[] = []
  if (candidate.year && candidate.year > 0) parts.push(`${candidate.year}年`)
  if (candidate.ep_count && candidate.ep_count > 0) parts.push(`${candidate.ep_count}话`)
  return parts.join(' · ')
}

function searchItemLabel(item: BangumiSearchItem) {
  return item.name_cn || item.name
}

function batchLabel(bangumiId: number) {
  return `归入此番剧（同目录 ${props.siblingCount(bangumiId)} 个文件）`
}

// 偏移只在输入框展开时提交；空值视为不传，非法值在本地拦截，不发请求。
function readOffset(): { ok: true; value?: number } | { ok: false } {
  offsetError.value = ''
  if (!showOffset.value) return { ok: true }
  const raw = offsetInput.value.toString().trim()
  if (raw === '') return { ok: true }
  const value = Number(raw)
  if (!Number.isInteger(value) || value < 0) {
    offsetError.value = '集号偏移必须是不小于 0 的整数'
    return { ok: false }
  }
  return { ok: true, value }
}

function submit(bangumiId: number, scope: InboxConfirmPayload['scope']) {
  if (props.confirming) return
  const offset = readOffset()
  if (!offset.ok) return
  const payload: InboxConfirmPayload = { bangumiId, scope }
  if (offset.value !== undefined) payload.episodeOffset = offset.value
  emit('confirm', payload)
}

function toggleSearch() {
  showSearch.value = !showSearch.value
}

async function handleSearch() {
  const value = searchKeyword.value.trim()
  if (!value) return
  searching.value = true
  searchError.value = ''
  searchResults.value = []
  searched.value = false
  try {
    searchResults.value = await searchBangumi(value)
    searched.value = true
  } catch (e: unknown) {
    searchError.value = e instanceof ApiError ? e.message : '搜索失败'
  } finally {
    searching.value = false
  }
}
</script>

<template>
  <div class="inbox-card">
    <div class="inbox-card-head">
      <code>{{ file.file_name }}</code>
      <span>{{ file.reason }}</span>
    </div>

    <ul v-if="candidates.length > 0" class="candidate-picks">
      <li v-for="candidate in candidates" :key="candidate.id" class="candidate-row">
        <span class="candidate-info">
          <span class="candidate-name">{{ candidateLabel(candidate) }}</span>
          <span v-if="candidateMeta(candidate)" class="candidate-meta">{{ candidateMeta(candidate) }}</span>
          <span class="candidate-score">{{ candidate.score.toFixed(2) }}</span>
        </span>
        <span class="candidate-actions">
          <button
            type="button"
            class="action-btn candidate-btn"
            :disabled="confirming"
            @click="submit(candidate.id, 'siblings')"
          >
            {{ batchLabel(candidate.id) }}
          </button>
          <button
            type="button"
            class="candidate-single-btn"
            :disabled="confirming"
            @click="submit(candidate.id, 'single')"
          >
            仅此文件
          </button>
        </span>
      </li>
    </ul>

    <div v-if="error" class="inbox-card-error" role="alert">
      <p class="inbox-card-error-msg">{{ error.message }}</p>
      <ul v-if="error.file_names && error.file_names.length > 0" class="inbox-card-error-files">
        <li v-for="name in error.file_names" :key="name"><code>{{ name }}</code></li>
      </ul>
    </div>

    <div class="inbox-card-tools">
      <button type="button" class="inbox-tool-btn search-toggle-btn" @click="toggleSearch">
        {{ showSearch ? '收起搜索' : '手动搜索条目' }}
      </button>
      <button v-if="!showOffset" type="button" class="inbox-tool-btn offset-toggle-btn" @click="showOffset = true">
        填集号偏移
      </button>
    </div>

    <label v-if="showOffset" class="offset-field">
      <span>集号偏移</span>
      <input
        v-model="offsetInput"
        class="offset-input"
        type="number"
        min="0"
        step="1"
        inputmode="numeric"
        placeholder="0"
      />
    </label>
    <p v-if="offsetError" class="inbox-card-error-msg" role="alert">{{ offsetError }}</p>

    <div v-if="showSearch" class="manual-search">
      <form class="manual-search-form" @submit.prevent="handleSearch">
        <input
          v-model="searchKeyword"
          class="manual-search-input"
          type="search"
          placeholder="输入 Bangumi 条目名"
          aria-label="手动搜索条目"
        />
        <button type="submit" class="inbox-tool-btn manual-search-btn" :disabled="searching">
          {{ searching ? '搜索中...' : '搜索' }}
        </button>
      </form>
      <p v-if="searchError" class="inbox-card-error-msg" role="alert">{{ searchError }}</p>
      <p v-else-if="searched && searchResults.length === 0" class="manual-search-empty">没有找到条目</p>
      <ul v-if="searchResults.length > 0" class="candidate-picks manual-results">
        <li v-for="item in searchResults" :key="`search-${item.id}`" class="candidate-row manual-result-row">
          <span class="candidate-info">
            <span class="candidate-name">{{ searchItemLabel(item) }}</span>
            <span v-if="item.eps_count > 0" class="candidate-meta">{{ item.eps_count }}话</span>
          </span>
          <span class="candidate-actions">
            <button
              type="button"
              class="action-btn manual-pick-btn"
              :disabled="confirming"
              @click="submit(item.id, 'siblings')"
            >
              {{ batchLabel(item.id) }}
            </button>
            <button
              type="button"
              class="candidate-single-btn manual-single-btn"
              :disabled="confirming"
              @click="submit(item.id, 'single')"
            >
              仅此文件
            </button>
          </span>
        </li>
      </ul>
    </div>
  </div>
</template>

<style scoped>
.inbox-card {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
}

.inbox-card-head {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 14px;
}

.inbox-card-head code,
.inbox-card-error-files code {
  overflow-wrap: anywhere;
  color: var(--text-color);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.candidate-picks {
  display: flex;
  flex-direction: column;
  gap: 8px;
  width: 100%;
  padding: 0;
  list-style: none;
}

.candidate-row {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: 8px 12px;
}

.candidate-info {
  display: flex;
  flex-wrap: wrap;
  align-items: baseline;
  gap: 4px 8px;
  min-width: 0;
}

.candidate-name {
  color: var(--text-color);
  font-weight: 600;
}

.candidate-meta,
.candidate-score,
.manual-search-empty {
  color: var(--text-muted-color);
  font-size: 12px;
}

.candidate-actions,
.inbox-card-tools {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.candidate-btn,
.manual-pick-btn {
  min-height: 36px;
  padding: 0 12px;
  font-size: 13px;
}

.candidate-single-btn,
.inbox-tool-btn {
  min-height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  font-size: 13px;
  cursor: pointer;
  transition: background-color 180ms ease-out, color 180ms ease-out, border-color 180ms ease-out;
}

.candidate-single-btn:hover:not(:disabled),
.inbox-tool-btn:hover:not(:disabled) {
  border-color: var(--accent-color);
  background: var(--surface-hover);
  color: var(--text-color);
}

.candidate-single-btn:disabled,
.inbox-tool-btn:disabled {
  cursor: not-allowed;
  opacity: 0.4;
}

.inbox-card-error {
  padding: 8px 10px;
  border-left: 3px solid var(--danger-color);
  border-radius: var(--radius-sm);
  background: var(--surface-muted-color);
}

.inbox-card-error-msg {
  color: var(--danger-color);
  font-size: 13px;
}

.inbox-card-error-files {
  margin-top: 4px;
  padding-left: 18px;
}

.offset-field {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--text-secondary);
  font-size: 13px;
}

.offset-input,
.manual-search-input {
  min-height: 36px;
  padding: 0 10px;
  border: 1px solid var(--border-strong-color);
  border-radius: var(--radius-sm);
  outline: none;
  background: var(--surface-color);
  color: var(--text-color);
  font-size: 14px;
}

.offset-input {
  width: 96px;
}

.manual-search {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.manual-search-form {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.manual-search-input {
  flex: 1;
  min-width: 180px;
}

.offset-input:focus,
.manual-search-input:focus {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 3px var(--accent-glow);
}
</style>
