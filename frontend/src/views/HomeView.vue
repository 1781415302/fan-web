<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import { listAnimes } from '../api/anime'
import type { Anime } from '../types/anime'

const router = useRouter()
const recentAnimes = ref<Anime[]>([])
const loading = ref(true)
const error = ref('')
const failedCovers = ref(new Set<number>())

function displayTitle(anime: Anime) {
  return anime.title_cn || anime.title
}

function markCoverFailed(id: number) {
  failedCovers.value = new Set(failedCovers.value).add(id)
}

async function loadRecent() {
  loading.value = true
  error.value = ''
  try {
    const data = await listAnimes(1, 10, '')
    recentAnimes.value = data.items
    failedCovers.value = new Set()
  } catch (e: unknown) {
    error.value = e instanceof ApiError ? e.message : '加载最近入库失败'
  } finally {
    loading.value = false
  }
}

function openAnime(id: number) {
  void router.push({ name: 'anime-detail', params: { id } })
}

onMounted(() => void loadRecent())
</script>

<template>
  <section class="home" aria-labelledby="home-title">
    <header class="home-hero">
      <div class="home-intro">
        <p class="eyebrow">Private collection</p>
        <h1 id="home-title" class="home-title">你的番剧库</h1>
        <p class="home-desc">收藏、进度与观看入口，集中在一个安静的空间里。</p>
        <div class="home-actions">
          <router-link class="primary-btn home-action" :to="{ name: 'anime-list' }">进入番剧库</router-link>
        </div>
      </div>

      <aside class="home-hero-panel" aria-label="内容状态">
        <p class="panel-label">当前视图</p>
        <p class="panel-value">最近入库</p>
        <div class="panel-status" aria-live="polite">
          <span class="status-dot" aria-hidden="true"></span>
          <span v-if="loading">正在读取内容</span>
          <span v-else-if="recentAnimes.length">已展示 {{ recentAnimes.length }} 部番剧</span>
          <span v-else>等待内容入库</span>
        </div>
      </aside>
    </header>

    <section class="recent-section" aria-labelledby="recent-title">
      <div class="section-heading">
        <div class="section-heading-copy">
          <p class="section-kicker">Continue exploring</p>
          <h2 id="recent-title">最近入库</h2>
        </div>
        <div class="section-heading-actions">
          <span v-if="!loading" class="section-count">{{ recentAnimes.length }} 部</span>
          <router-link class="section-link" :to="{ name: 'anime-list' }">查看全部</router-link>
        </div>
      </div>

      <p v-if="error" class="error-msg" role="alert">{{ error }}</p>
      <div v-if="loading" class="recent-list" aria-live="polite" aria-label="正在加载最近入库">
        <div v-for="index in 6" :key="index" class="recent-card recent-skeleton">
          <div class="skeleton recent-cover"></div>
          <div class="skeleton recent-title-line"></div>
          <div class="skeleton recent-meta-line"></div>
        </div>
      </div>
      <div v-else-if="recentAnimes.length === 0" class="recent-empty">库里还没有番剧</div>
      <div v-else class="recent-list">
        <button
          v-for="anime in recentAnimes"
          :key="anime.id"
          type="button"
          class="recent-card"
          :aria-label="`打开${displayTitle(anime)}`"
          @click="openAnime(anime.id)"
        >
          <img
            v-if="anime.cover && !failedCovers.has(anime.id)"
            :src="anime.cover"
            :alt="`${displayTitle(anime)}封面`"
            class="recent-cover"
            loading="lazy"
            @error="markCoverFailed(anime.id)"
          />
          <span v-else class="recent-cover placeholder-cover">无封面</span>
          <span class="recent-title" :title="displayTitle(anime)">{{ displayTitle(anime) }}</span>
          <span class="recent-meta">查看详情</span>
        </button>
      </div>
    </section>
  </section>
</template>

<style scoped>
.home {
  max-width: 1180px;
  margin: 0 auto;
  padding-bottom: 24px;
}

.home-hero {
  display: grid;
  min-height: 320px;
  grid-template-columns: minmax(0, 1fr) minmax(220px, 280px);
  align-items: end;
  gap: 48px;
  padding: 52px 0 48px;
  border-bottom: 1px solid var(--border-color);
}

.home-intro {
  max-width: 720px;
}

.home-title {
  margin: 0 0 16px;
  color: var(--text-color);
  font-size: 48px;
  font-weight: 700;
  line-height: 1.08;
}

.home-desc {
  max-width: 560px;
  color: var(--text-secondary);
  font-size: 17px;
  line-height: 1.7;
}

.home-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  margin-top: 30px;
}

.home-action {
  min-height: 46px;
  padding-right: 20px;
  padding-left: 20px;
}

.home-hero-panel {
  min-height: 158px;
  padding: 22px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-lg);
  background: var(--surface-color);
  box-shadow: var(--shadow-md);
}

.panel-label,
.section-kicker {
  color: var(--text-muted-color);
  font-size: 11px;
  font-weight: 700;
  line-height: 1.4;
  text-transform: uppercase;
}

.panel-value {
  margin-top: 18px;
  color: var(--text-color);
  font-size: 24px;
  font-weight: 700;
}

.panel-status {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 16px;
  color: var(--text-secondary);
  font-size: 13px;
}

.status-dot {
  width: 8px;
  height: 8px;
  flex: 0 0 auto;
  border-radius: 50%;
  background: var(--primary-color);
  box-shadow: 0 0 0 4px var(--primary-glow);
}

.recent-section {
  padding-top: 40px;
}

.section-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 18px;
  margin-bottom: 18px;
}

.section-heading h2 {
  margin-top: 5px;
  color: var(--text-color);
  font-size: 22px;
  font-weight: 700;
}

.section-heading-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}

.section-count {
  color: var(--text-muted-color);
  font-size: 13px;
}

.section-link {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  color: var(--accent-color);
  font-size: 13px;
  font-weight: 600;
  text-decoration: none;
}

.section-link:hover {
  color: var(--accent-hover-text);
}

.recent-list {
  display: flex;
  gap: 16px;
  overflow-x: auto;
  padding: 4px 2px 14px;
  scrollbar-color: var(--border-strong-color) transparent;
}

.recent-card {
  display: flex;
  flex: 0 0 148px;
  min-width: 148px;
  flex-direction: column;
  gap: 8px;
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  text-align: left;
  cursor: pointer;
}

.recent-cover {
  display: flex;
  width: 148px;
  aspect-ratio: 2 / 3;
  align-items: center;
  justify-content: center;
  overflow: hidden;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  object-fit: cover;
  color: var(--text-secondary);
  font-size: 12px;
  transition: border-color 180ms ease-out, box-shadow 180ms ease-out;
}

.recent-card:hover .recent-cover {
  border-color: var(--primary-color);
  box-shadow: 0 0 0 3px var(--primary-glow);
}

.recent-card:hover .recent-title {
  color: var(--primary-hover-color);
}

.recent-title {
  display: -webkit-box;
  min-height: 42px;
  overflow: hidden;
  color: var(--text-color);
  font-size: 13px;
  font-weight: 600;
  line-height: 1.6;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
}

.recent-meta {
  color: var(--text-muted-color);
  font-size: 12px;
}

.recent-title-line {
  width: 88%;
  height: 14px;
}

.recent-meta-line {
  width: 52%;
  height: 12px;
}

.recent-skeleton {
  pointer-events: none;
}

.recent-empty {
  padding: 52px 24px;
  border: 1px dashed var(--border-strong-color);
  border-radius: var(--radius-md);
  background: var(--surface-muted-color);
  color: var(--text-secondary);
  text-align: center;
}

@media (max-width: 720px) {
  .home-hero {
    min-height: 0;
    grid-template-columns: 1fr;
    gap: 28px;
    padding: 36px 0 34px;
  }

  .home-hero-panel {
    min-height: 0;
  }
}

@media (max-width: 480px) {
  .home-title {
    font-size: 36px;
  }

  .home-desc {
    font-size: 16px;
  }

  .section-heading {
    align-items: flex-start;
    flex-direction: column;
    gap: 8px;
  }

  .section-heading-actions {
    width: 100%;
    justify-content: space-between;
  }
}
</style>
