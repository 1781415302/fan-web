<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import { listAnimes } from '../api/anime'
import type { Anime } from '../types/anime'

const router = useRouter()
const animes = ref<Anime[]>([])
const loading = ref(true)
const error = ref('')
const keyword = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const failedCovers = ref(new Set<number>())

let searchTimer: ReturnType<typeof setTimeout> | undefined

const totalPages = () => Math.max(1, Math.ceil(total.value / pageSize))

async function load() {
  loading.value = true
  error.value = ''
  try {
    const data = await listAnimes(page.value, pageSize, keyword.value.trim())
    animes.value = data.items
    total.value = data.total
    failedCovers.value = new Set()
  } catch (e: unknown) {
    error.value = e instanceof ApiError ? e.message : '加载番剧失败'
  } finally {
    loading.value = false
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

function handleCardKeydown(event: KeyboardEvent, id: number) {
  if (event.key === 'Enter' || event.key === ' ') {
    event.preventDefault()
    openAnime(id)
  }
}

function displayTitle(anime: Anime) {
  return anime.title_cn || anime.title
}

function markCoverFailed(id: number) {
  failedCovers.value = new Set(failedCovers.value).add(id)
}

onMounted(() => void load())
onBeforeUnmount(() => {
  if (searchTimer) clearTimeout(searchTimer)
})
</script>

<template>
  <section class="anime-list-page" aria-labelledby="anime-list-title">
    <div class="page-header">
      <div>
        <p class="eyebrow">库浏览</p>
        <h1 id="anime-list-title">番剧库</h1>
      </div>
      <button type="button" class="primary-btn" @click="router.push({ name: 'anime-add' })">
        添加番剧
      </button>
    </div>

    <input
      v-model="keyword"
      class="search-input"
      type="search"
      aria-label="搜索番剧"
      placeholder="搜索番剧..."
      @input="onSearchInput"
    />

    <p v-if="error" class="error-msg" role="alert">{{ error }}</p>
    <div v-if="loading" class="empty-state" aria-live="polite">加载中...</div>
    <div v-else-if="animes.length === 0" class="empty-state">
      暂无番剧，点击“添加番剧”开始建立库
    </div>
    <div v-else class="anime-grid">
      <article
        v-for="anime in animes"
        :key="anime.id"
        class="anime-card"
        role="link"
        tabindex="0"
        @click="openAnime(anime.id)"
        @keydown="handleCardKeydown($event, anime.id)"
      >
        <img
          v-if="anime.cover && !failedCovers.has(anime.id)"
          :src="anime.cover"
          :alt="`${displayTitle(anime)}封面`"
          class="card-cover"
          loading="lazy"
          @error="markCoverFailed(anime.id)"
        />
        <div v-else class="card-cover placeholder-cover">无封面</div>
        <div class="card-info">
          <p class="card-title" :title="displayTitle(anime)">{{ displayTitle(anime) }}</p>
          <p class="card-eps">
            {{ anime.ep_count > 0 ? `已看 ${anime.watched_count ?? 0} / 全${anime.ep_count}话` : `已看 ${anime.watched_count ?? 0} / 集数未知` }}
          </p>
        </div>
      </article>
    </div>

    <div v-if="total > pageSize" class="pagination" aria-label="分页">
      <button type="button" :disabled="page <= 1" @click="changePage(page - 1)">上一页</button>
      <span>{{ page }} / {{ totalPages() }}</span>
      <button type="button" :disabled="page >= totalPages()" @click="changePage(page + 1)">下一页</button>
    </div>
  </section>
</template>

<style scoped>
.anime-list-page { padding-bottom: 40px; }
.page-header { display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-bottom: 20px; }
.eyebrow { color: var(--primary-hover-color); font-size: 13px; margin-bottom: 2px; }
h1 { color: var(--text-color); font-size: 24px; }
.primary-btn { padding: 8px 16px; border: 0; border-radius: 4px; background: var(--primary-color); color: #fff; font-size: 14px; font-weight: 600; cursor: pointer; white-space: nowrap; }
.primary-btn:hover { background: var(--primary-hover-color); }
.search-input { width: 100%; max-width: 400px; height: 38px; margin-bottom: 20px; padding: 0 12px; border: 1px solid var(--border-color); border-radius: 4px; background: var(--surface-color); color: var(--text-color); outline: none; }
.search-input:focus { border-color: var(--primary-color); }
.error-msg { margin-bottom: 16px; color: #f87171; font-size: 14px; }
.anime-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(160px, 1fr)); gap: 20px; }
.anime-card { overflow: hidden; border: 1px solid transparent; border-radius: 8px; background: var(--surface-color); cursor: pointer; transition: transform 0.15s, border-color 0.15s; }
.anime-card:hover, .anime-card:focus-visible { border-color: var(--primary-color); outline: none; transform: translateY(-3px); }
.card-cover { display: block; width: 100%; aspect-ratio: 2 / 3; object-fit: cover; }
.placeholder-cover { display: flex; align-items: center; justify-content: center; background: var(--surface-hover); color: var(--text-secondary); font-size: 14px; }
.card-info { padding: 8px 10px; }
.card-title { overflow: hidden; color: var(--text-color); font-size: 14px; font-weight: 600; text-overflow: ellipsis; white-space: nowrap; }
.card-eps { margin-top: 2px; color: var(--text-secondary); font-size: 12px; }
.empty-state { padding: 60px 0; color: var(--text-secondary); text-align: center; }
.pagination { display: flex; align-items: center; justify-content: center; gap: 16px; margin-top: 32px; }
.pagination button { padding: 6px 14px; border: 1px solid var(--border-color); border-radius: 4px; background: transparent; color: var(--text-color); cursor: pointer; }
.pagination button:disabled { opacity: 0.4; cursor: not-allowed; }
.pagination span { color: var(--text-secondary); font-size: 14px; }
@media (max-width: 480px) {
  .page-header { align-items: flex-start; }
  .anime-grid { grid-template-columns: repeat(auto-fill, minmax(130px, 1fr)); gap: 12px; }
}
</style>
