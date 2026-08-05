<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import { createAnime } from '../api/anime'
import { searchBangumi } from '../api/bangumi'
import type { BangumiSearchItem } from '../types/anime'

const router = useRouter()
const keyword = ref('')
const searching = ref(false)
const searchError = ref('')
const results = ref<BangumiSearchItem[]>([])
const selected = ref<BangumiSearchItem | null>(null)
const filePath = ref('')
const creating = ref(false)
const createError = ref('')
const failedCovers = ref(new Set<number>())

async function handleSearch() {
  const value = keyword.value.trim()
  if (!value) return
  searching.value = true
  searchError.value = ''
  results.value = []
  selected.value = null
  try {
    results.value = await searchBangumi(value)
  } catch (e: unknown) {
    searchError.value = e instanceof ApiError ? e.message : '搜索失败'
  } finally {
    searching.value = false
  }
}

function selectItem(item: BangumiSearchItem) {
  selected.value = item
  createError.value = ''
}

async function handleCreate() {
  if (!selected.value) return
  creating.value = true
  createError.value = ''
  try {
    const anime = await createAnime(selected.value.id, filePath.value.trim())
    await router.replace({ name: 'anime-detail', params: { id: anime.id } })
  } catch (e: unknown) {
    createError.value = e instanceof ApiError ? e.message : '添加失败'
  } finally {
    creating.value = false
  }
}

function displayName(item: BangumiSearchItem) {
  return item.name_cn || item.name
}

function markCoverFailed(id: number) {
  failedCovers.value = new Set(failedCovers.value).add(id)
}
</script>

<template>
  <section class="add-page" aria-labelledby="add-title">
    <header class="page-heading">
      <p class="eyebrow">Library intake</p>
      <h1 id="add-title">添加番剧</h1>
      <p class="page-desc">从 Bangumi 获取番剧信息，再关联本地视频目录。</p>
    </header>

    <form class="search-section" @submit.prevent="handleSearch">
      <div class="search-bar">
        <label for="bangumi-search" class="search-label">搜索 Bangumi</label>
        <div class="search-control">
          <input id="bangumi-search" v-model="keyword" type="search" placeholder="输入番剧名称或关键词" />
          <button type="submit" class="primary-btn" :disabled="searching">{{ searching ? '搜索中...' : '搜索' }}</button>
        </div>
      </div>
      <p v-if="searchError" class="error-msg" role="alert">{{ searchError }}</p>
    </form>

    <section v-if="results.length" class="results-section" aria-labelledby="results-title">
      <div class="results-heading">
        <div>
          <p class="section-kicker">Search results</p>
          <h2 id="results-title">选择番剧</h2>
        </div>
        <span>{{ results.length }} 条结果</span>
      </div>
      <div class="search-results">
        <button
          v-for="item in results"
          :key="item.id"
          type="button"
          class="search-item"
          :class="{ selected: selected?.id === item.id }"
          :aria-pressed="selected?.id === item.id"
          @click="selectItem(item)"
        >
          <img
            v-if="item.cover && !failedCovers.has(item.id)"
            :src="item.cover"
            :alt="`${displayName(item)}封面`"
            class="result-cover"
            loading="lazy"
            @error="markCoverFailed(item.id)"
          />
          <span v-else class="result-cover placeholder-cover">无封面</span>
          <span class="result-info">
            <strong class="result-name">{{ displayName(item) }}</strong>
            <span v-if="item.name_cn && item.name" class="result-sub">{{ item.name }}</span>
            <span class="result-meta">{{ item.eps_count > 0 ? `全${item.eps_count}话` : '集数未知' }}</span>
            <span class="result-summary">{{ item.summary || '暂无简介' }}</span>
          </span>
          <span class="selection-mark" aria-hidden="true">{{ selected?.id === item.id ? '已选择' : '选择' }}</span>
        </button>
      </div>
    </section>

    <div v-if="!searching && keyword.trim() && results.length === 0 && !searchError" class="empty-state">没有找到相关番剧</div>

    <form v-if="selected" class="confirm-section" @submit.prevent="handleCreate">
      <div class="confirm-heading">
        <div>
          <p class="section-kicker">Selected title</p>
          <h2>确认添加</h2>
        </div>
        <span class="selected-badge">已选</span>
      </div>
      <div class="confirm-info">
        <img
          v-if="selected.cover && !failedCovers.has(selected.id)"
          :src="selected.cover"
          :alt="`${displayName(selected)}封面`"
          class="confirm-cover"
          @error="markCoverFailed(selected.id)"
        />
        <span v-else class="confirm-cover placeholder-cover">无封面</span>
        <div>
          <p class="confirm-name">{{ displayName(selected) }}</p>
          <p class="confirm-meta">Bangumi ID：{{ selected.id }}</p>
        </div>
      </div>
      <div class="form-field">
        <label for="file-path">文件目录名 <span class="label-hint">可选</span></label>
        <input id="file-path" v-model="filePath" type="text" placeholder="留空扫描根目录，如 Re_Zero" />
        <p class="form-hint">视频根目录下的相对目录名，不能填写绝对路径或 ..。</p>
      </div>
      <p v-if="createError" class="error-msg" role="alert">{{ createError }}</p>
      <div class="confirm-actions">
        <button type="submit" class="primary-btn" :disabled="creating">{{ creating ? '添加中...' : '确认添加' }}</button>
      </div>
    </form>
  </section>
</template>

<style scoped>
.add-page { max-width: 920px; margin: 0 auto; padding-bottom: 32px; }
.page-heading { padding: 12px 0 28px; border-bottom: 1px solid var(--border-color); }
h1 { color: var(--text-color); font-size: 36px; font-weight: 700; line-height: 1.15; }
.page-desc { margin-top: 10px; color: var(--text-secondary); font-size: 15px; }
.search-section { margin-top: 28px; padding: 20px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-sm); }
.search-bar { display: block; }
.search-label { display: block; margin-bottom: 8px; color: var(--text-secondary); font-size: 13px; font-weight: 600; }
.search-control { display: flex; gap: 10px; }
.search-control input { flex: 1; min-width: 0; min-height: 48px; padding: 0 14px; border: 1px solid var(--border-strong-color); border-radius: var(--radius-sm); background: var(--surface-muted-color); color: var(--text-color); outline: none; font-size: 16px; transition: border-color 180ms ease-out, box-shadow 180ms ease-out; }
.search-control input:focus { border-color: var(--accent-color); box-shadow: 0 0 0 4px var(--accent-glow); }
.results-section { margin-top: 32px; }
.results-heading, .confirm-heading { display: flex; align-items: end; justify-content: space-between; gap: 16px; margin-bottom: 14px; }
.section-kicker { margin-bottom: 5px; color: var(--text-muted-color); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.results-heading h2, .confirm-heading h2 { color: var(--text-color); font-size: 21px; }
.results-heading > span { color: var(--text-muted-color); font-size: 13px; }
.search-results { display: flex; flex-direction: column; gap: 10px; }
.search-item { display: flex; width: 100%; min-height: 104px; align-items: center; gap: 14px; padding: 12px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); color: inherit; text-align: left; cursor: pointer; appearance: none; transition: background-color 180ms ease-out, border-color 180ms ease-out, box-shadow 180ms ease-out; }
.search-item:hover, .search-item.selected { border-color: var(--accent-color); background: var(--surface-raised-color); box-shadow: var(--shadow-sm); }
.result-cover { display: flex; width: 68px; height: 96px; flex: 0 0 68px; align-items: center; justify-content: center; overflow: hidden; border-radius: var(--radius-sm); object-fit: cover; color: var(--text-muted-color); font-size: 11px; }
.result-info { display: flex; min-width: 0; flex: 1; flex-direction: column; }
.result-name { color: var(--text-color); font-size: 15px; }
.result-sub, .result-meta { margin-top: 3px; color: var(--text-secondary); font-size: 13px; }
.result-summary { display: -webkit-box; overflow: hidden; margin-top: 7px; color: var(--text-secondary); font-size: 13px; line-height: 1.5; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.selection-mark { min-width: 48px; color: var(--text-muted-color); font-size: 12px; text-align: right; }
.search-item.selected .selection-mark { color: var(--accent-color); }
.confirm-section { margin-top: 32px; padding: 22px; border: 1px solid var(--primary-soft-border); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-md); }
.selected-badge { padding: 5px 9px; border: 1px solid var(--primary-soft-border); border-radius: 999px; background: var(--primary-soft-bg); color: var(--primary-hover-color); font-size: 12px; }
.confirm-info { display: flex; gap: 16px; margin-bottom: 22px; padding-bottom: 18px; border-bottom: 1px solid var(--border-color); }
.confirm-cover { display: flex; width: 84px; height: 126px; flex: 0 0 84px; align-items: center; justify-content: center; overflow: hidden; border-radius: var(--radius-sm); object-fit: cover; color: var(--text-muted-color); font-size: 11px; }
.confirm-name { color: var(--text-color); font-size: 17px; font-weight: 600; }
.confirm-meta { margin-top: 5px; color: var(--text-secondary); font-size: 13px; }
.form-field label { display: block; margin-bottom: 7px; color: var(--text-secondary); font-size: 13px; font-weight: 600; }
.label-hint { color: var(--text-muted-color); font-weight: 400; }
.form-field input { width: 100%; min-height: 46px; padding: 0 12px; border: 1px solid var(--border-strong-color); border-radius: var(--radius-sm); background: var(--surface-muted-color); color: var(--text-color); outline: none; font-size: 16px; }
.form-field input:focus { border-color: var(--accent-color); box-shadow: 0 0 0 4px var(--accent-glow); }
.form-hint { margin-top: 7px; color: var(--text-muted-color); font-size: 12px; }
.confirm-actions { display: flex; justify-content: flex-end; margin-top: 20px; }
@media (max-width: 600px) { h1 { font-size: 32px; } .search-control { flex-direction: column; } .search-control .primary-btn { width: 100%; } .search-item { align-items: flex-start; } .result-cover { width: 56px; height: 80px; flex-basis: 56px; } .selection-mark { display: none; } .confirm-section { padding: 18px; } .confirm-actions .primary-btn { width: 100%; } }
</style>
