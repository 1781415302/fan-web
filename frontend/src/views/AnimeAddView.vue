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
    <div class="page-heading">
      <p class="eyebrow">库管理</p>
      <h1 id="add-title">添加番剧</h1>
      <p class="page-desc">从 Bangumi 获取番剧信息，再关联本地视频目录。</p>
    </div>

    <form class="search-section" @submit.prevent="handleSearch">
      <div class="search-bar">
        <input v-model="keyword" type="search" aria-label="Bangumi 搜索关键词" placeholder="输入番剧名称搜索 Bangumi..." />
        <button type="submit" class="primary-btn" :disabled="searching">{{ searching ? '搜索中...' : '搜索' }}</button>
      </div>
      <p v-if="searchError" class="error-msg" role="alert">{{ searchError }}</p>
    </form>

    <div v-if="!searching && keyword.trim() && results.length === 0 && !searchError" class="empty-state">没有找到相关番剧</div>
    <div v-if="results.length" class="search-results" aria-label="Bangumi 搜索结果">
      <button
        v-for="item in results"
        :key="item.id"
        type="button"
        class="search-item"
        :class="{ selected: selected?.id === item.id }"
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
      </button>
    </div>

    <form v-if="selected" class="confirm-section" @submit.prevent="handleCreate">
      <h2>确认添加</h2>
      <div class="confirm-info">
        <img v-if="selected.cover" :src="selected.cover" :alt="`${displayName(selected)}封面`" class="confirm-cover" />
        <div>
          <p class="confirm-name">{{ displayName(selected) }}</p>
          <p class="confirm-meta">Bangumi ID：{{ selected.id }}</p>
        </div>
      </div>
      <div class="form-field">
        <label for="file-path">文件目录名</label>
        <input id="file-path" v-model="filePath" type="text" placeholder="留空扫描根目录，如 Re_Zero" />
        <p class="form-hint">视频根目录下的相对目录名，不能填写绝对路径或 ..。</p>
      </div>
      <p v-if="createError" class="error-msg" role="alert">{{ createError }}</p>
      <button type="submit" class="primary-btn" :disabled="creating">{{ creating ? '添加中...' : '确认添加' }}</button>
    </form>
  </section>
</template>

<style scoped>
.add-page { max-width: 820px; margin: 0 auto; padding-bottom: 40px; }
.page-heading { margin-bottom: 24px; }
.eyebrow { color: var(--primary-hover-color); font-size: 13px; margin-bottom: 2px; }
h1 { color: var(--text-color); font-size: 24px; }
.page-desc { margin-top: 4px; color: var(--text-secondary); font-size: 14px; }
.search-bar { display: flex; gap: 10px; margin-bottom: 16px; }
.search-bar input { flex: 1; min-width: 0; height: 38px; padding: 0 12px; border: 1px solid var(--border-color); border-radius: 4px; background: var(--surface-color); color: var(--text-color); outline: none; }
.search-bar input:focus { border-color: var(--primary-color); }
.primary-btn { min-height: 38px; padding: 0 18px; border: 0; border-radius: 4px; background: var(--primary-color); color: #fff; font-size: 14px; font-weight: 600; cursor: pointer; white-space: nowrap; }
.primary-btn:hover { background: var(--primary-hover-color); }
.primary-btn:disabled { opacity: 0.6; cursor: wait; }
.error-msg { margin: 8px 0; color: #f87171; font-size: 14px; }
.empty-state { padding: 40px 0; color: var(--text-secondary); text-align: center; }
.search-results { display: flex; flex-direction: column; gap: 10px; }
.search-item { display: flex; width: 100%; gap: 12px; padding: 12px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--surface-color); color: inherit; text-align: left; cursor: pointer; }
.search-item:hover, .search-item.selected { border-color: var(--primary-color); }
.result-cover { flex: 0 0 60px; width: 60px; aspect-ratio: 2 / 3; object-fit: cover; border-radius: 4px; }
.placeholder-cover { display: flex; align-items: center; justify-content: center; background: var(--surface-hover); color: var(--text-secondary); font-size: 11px; }
.result-info { display: flex; min-width: 0; flex-direction: column; }
.result-name { color: var(--text-color); font-size: 15px; }
.result-sub, .result-meta { margin-top: 2px; color: var(--text-secondary); font-size: 13px; }
.result-summary { display: -webkit-box; overflow: hidden; margin-top: 6px; color: var(--text-secondary); font-size: 13px; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }
.confirm-section { margin-top: 32px; padding: 20px; border: 1px solid var(--border-color); border-radius: 8px; background: var(--surface-color); }
.confirm-section h2 { margin-bottom: 16px; font-size: 18px; }
.confirm-info { display: flex; gap: 14px; margin-bottom: 16px; }
.confirm-cover { width: 80px; aspect-ratio: 2 / 3; object-fit: cover; border-radius: 4px; }
.confirm-name { color: var(--text-color); font-size: 16px; font-weight: 600; }
.confirm-meta { margin-top: 4px; color: var(--text-secondary); font-size: 13px; }
.form-field { margin-bottom: 14px; }
.form-field label { display: block; margin-bottom: 4px; color: var(--text-secondary); font-size: 14px; }
.form-field input { width: 100%; height: 38px; padding: 0 12px; border: 1px solid var(--border-color); border-radius: 4px; background: var(--bg-color); color: var(--text-color); outline: none; }
.form-field input:focus { border-color: var(--primary-color); }
.form-hint { margin-top: 4px; color: var(--text-secondary); font-size: 12px; }
@media (max-width: 600px) { .search-bar { flex-direction: column; } .primary-btn { width: 100%; } .result-cover { flex-basis: 48px; width: 48px; } .confirm-cover { width: 64px; } }
</style>
