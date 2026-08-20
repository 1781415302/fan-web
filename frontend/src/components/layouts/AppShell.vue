<script setup lang="ts">
import { computed } from 'vue'

import { useShell } from '../../composables/useShell'
import { useThemeStore } from '../../stores/theme'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const themeStore = useThemeStore()
const {
  appStore,
  authStore,
  handleLogout,
  bangumiPanelOpen,
  bangumiLinked,
  bangumiSuffix,
  bangumiTokenDraft,
  bangumiLoading,
  bangumiSyncing,
  bangumiError,
  bangumiMessage,
  openBangumiPanel,
  closeBangumiPanel,
  bindBangumi,
  unbindBangumi,
  syncBangumiProgress,
} = useShell()

// 固定外壳：切换 UI 风格只替换导航（chrome）与内容区样式类，
// <main> 与其中的 router-view 始终保持挂载，页面（播放器、列表状态）不被卸载。
const shellClass = computed(() => {
  switch (themeStore.ui) {
    case 'cinema':
      return 'app-shell cinema-shell'
    case 'glass':
      return 'app-shell glass-shell'
    case 'apple':
      return 'app-shell fluent-shell'
    default:
      return 'app-shell modern-shell'
  }
})

const contentClass = computed(() => {
  switch (themeStore.ui) {
    case 'cinema':
      return 'cinema-stage'
    case 'glass':
      return 'glass-content'
    case 'apple':
      return 'fluent-content'
    default:
      return 'content'
  }
})
</script>

<template>
  <div :class="shellClass" :data-ui="themeStore.ui">
    <a class="skip-link" href="#main-content">跳转到主要内容</a>

    <!-- 影院深色：左侧导航栏 -->
    <aside v-if="themeStore.ui === 'cinema'" class="cinema-rail" aria-label="主导航">
      <router-link to="/" class="cinema-brand" :aria-label="`${appStore.siteName}首页`">
        <span class="cinema-brand-wordmark">{{ appStore.siteName }}</span>
      </router-link>

      <nav class="cinema-nav" aria-label="主菜单">
        <router-link
          v-if="authStore.initialized && authStore.isAuthenticated"
          to="/animes"
          class="cinema-nav-link"
        >
          番剧库
        </router-link>
        <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.isAdmin">
          <router-link to="/admin/users" class="cinema-nav-link">用户管理</router-link>
          <router-link to="/admin/update" class="cinema-nav-link">系统更新</router-link>
        </template>
      </nav>

      <div class="cinema-rail-foot">
        <ThemeSwitcher />
        <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
          <span class="cinema-user" :title="authStore.user.username">{{ authStore.user.username }}</span>
          <button type="button" class="bangumi-trigger" @click="openBangumiPanel">Bangumi</button>
          <button type="button" class="cinema-logout" @click="handleLogout">退出登录</button>
        </template>
        <router-link v-else-if="authStore.initialized" to="/login" class="cinema-nav-link">
          登录
        </router-link>
        <span v-else class="cinema-user">加载中</span>
      </div>
    </aside>

    <!-- 玻璃拟态：悬浮导航 -->
    <header v-else-if="themeStore.ui === 'glass'" class="glass-dock" aria-label="主导航">
      <div class="glass-dock-inner">
        <router-link to="/" class="glass-brand" :aria-label="`${appStore.siteName}首页`">
          <span class="glass-brand-mark" aria-hidden="true">FW</span>
          <span class="glass-brand-name">{{ appStore.siteName }}</span>
        </router-link>

        <nav class="glass-nav" aria-label="主菜单">
          <router-link
            v-if="authStore.initialized && authStore.isAuthenticated"
            to="/animes"
            class="glass-nav-link"
          >
            番剧库
          </router-link>
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.isAdmin">
            <router-link to="/admin/users" class="glass-nav-link">用户管理</router-link>
            <router-link to="/admin/update" class="glass-nav-link">系统更新</router-link>
          </template>
        </nav>

        <div class="glass-session">
          <ThemeSwitcher />
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <span class="glass-user" :title="authStore.user.username">{{ authStore.user.username }}</span>
            <button type="button" class="bangumi-trigger" @click="openBangumiPanel">Bangumi</button>
            <button type="button" class="glass-logout" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="glass-nav-link">
            登录
          </router-link>
          <span v-else class="glass-user">加载中</span>
        </div>
      </div>
    </header>

    <!-- 苹果浅色：顶部导航 -->
    <header v-else-if="themeStore.ui === 'apple'" class="fluent-nav">
      <div class="fluent-nav-inner">
        <router-link to="/" class="fluent-brand" :aria-label="`${appStore.siteName}首页`">
          {{ appStore.siteName }}
        </router-link>

        <nav class="fluent-links" aria-label="主导航">
          <router-link
            v-if="authStore.initialized && authStore.isAuthenticated"
            to="/animes"
            class="fluent-link"
          >
            番剧库
          </router-link>
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.isAdmin">
            <router-link to="/admin/users" class="fluent-link">用户管理</router-link>
            <router-link to="/admin/update" class="fluent-link">系统更新</router-link>
          </template>
        </nav>

        <div class="fluent-session">
          <ThemeSwitcher />
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <span class="fluent-user" :title="authStore.user.username">{{ authStore.user.username }}</span>
            <button type="button" class="bangumi-trigger" @click="openBangumiPanel">Bangumi</button>
            <button type="button" class="fluent-logout" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="fluent-link">
            登录
          </router-link>
          <span v-else class="fluent-user">加载中</span>
        </div>
      </div>
    </header>

    <!-- 现代深色（默认）：顶部导航栏 -->
    <header v-else class="navbar">
      <div class="navbar-inner">
        <router-link to="/" class="brand" :aria-label="`${appStore.siteName}首页`">
          <span class="brand-mark" aria-hidden="true">FW</span>
          <span class="brand-name">{{ appStore.siteName }}</span>
        </router-link>

        <nav class="navbar-nav" aria-label="主导航">
          <router-link
            v-if="authStore.initialized && authStore.isAuthenticated"
            to="/animes"
            class="nav-link"
          >
            番剧库
          </router-link>
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.isAdmin">
            <router-link to="/admin/users" class="nav-link">用户管理</router-link>
            <router-link to="/admin/update" class="nav-link">系统更新</router-link>
          </template>
        </nav>

        <div class="navbar-session">
          <ThemeSwitcher />
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <span class="user-name" :title="authStore.user.username">{{ authStore.user.username }}</span>
            <button type="button" class="bangumi-trigger" @click="openBangumiPanel">Bangumi</button>
            <button type="button" class="logout-button" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="nav-link">
            登录
          </router-link>
          <span v-else class="nav-placeholder">加载中</span>
        </div>
      </div>
    </header>

    <main id="main-content" :class="contentClass" tabindex="-1">
      <slot />
    </main>

    <div
      v-if="bangumiPanelOpen"
      class="bangumi-backdrop"
      @click.self="closeBangumiPanel"
    >
      <section
        class="bangumi-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby="bangumi-panel-title"
        @keydown.esc="closeBangumiPanel"
      >
        <header class="bangumi-panel-head">
          <div>
            <p class="bangumi-kicker">Bangumi</p>
            <h2 id="bangumi-panel-title">个人令牌</h2>
          </div>
          <button type="button" class="action-btn" @click="closeBangumiPanel">关闭</button>
        </header>

        <p v-if="bangumiLinked" class="bangumi-status">
          已绑定 ···{{ bangumiSuffix }}
        </p>
        <p v-else class="bangumi-status">未绑定</p>

        <p class="bangumi-hint">
          在
          <a
            href="https://next.bgm.tv/demo/access-token"
            target="_blank"
            rel="noopener noreferrer"
          >next.bgm.tv</a>
          签发 Access Token，只同步「看过」。
        </p>

        <form v-if="!bangumiLinked" class="bangumi-form" @submit.prevent="bindBangumi">
          <label class="bangumi-label" for="bangumi-access-token">Access Token</label>
          <input
            id="bangumi-access-token"
            v-model="bangumiTokenDraft"
            type="password"
            name="bangumi-access-token"
            autocomplete="off"
            maxlength="512"
            placeholder="粘贴令牌，绑定后只显示末 4 位"
          />
          <button
            class="primary-btn"
            type="submit"
            :disabled="bangumiLoading || bangumiSyncing"
          >
            {{ bangumiLoading ? '绑定中...' : '绑定' }}
          </button>
        </form>

        <div v-else class="bangumi-actions">
          <button
            type="button"
            class="primary-btn"
            :disabled="bangumiLoading || bangumiSyncing"
            @click="syncBangumiProgress"
          >
            {{ bangumiSyncing ? '同步中...' : '同步进度' }}
          </button>
          <button
            type="button"
            class="action-btn"
            :disabled="bangumiLoading || bangumiSyncing"
            @click="unbindBangumi"
          >
            {{ bangumiLoading ? '处理中...' : '解除绑定' }}
          </button>
        </div>

        <p v-if="bangumiError" class="bangumi-error" role="alert">{{ bangumiError }}</p>
        <p v-if="bangumiMessage" class="bangumi-ok" role="status">{{ bangumiMessage }}</p>
      </section>
    </div>
  </div>
</template>

<style scoped>
/* ===== 影院深色（Cinema）===== */
.cinema-shell {
  min-height: 100dvh;
  display: grid;
  grid-template-columns: 232px minmax(0, 1fr);
}

.cinema-rail {
  position: sticky;
  top: 0;
  z-index: 30;
  display: flex;
  height: 100dvh;
  flex-direction: column;
  gap: 28px;
  padding: 30px 20px 24px;
  border-right: 1px solid var(--border-color);
  background: var(--surface-muted-color);
}

.cinema-brand {
  display: flex;
  min-height: 44px;
  align-items: center;
  padding: 0 8px;
  color: var(--text-color);
  text-decoration: none;
}

.cinema-brand-wordmark {
  font-family: 'Cinzel', serif;
  font-size: 19px;
  font-weight: 700;
  letter-spacing: 0.08em;
}

.cinema-nav {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cinema-nav-link {
  display: flex;
  min-height: 42px;
  align-items: center;
  padding: 0 12px;
  border: 1px solid transparent;
  border-left: 3px solid transparent;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 600;
  text-decoration: none;
  letter-spacing: 0.04em;
  transition: background-color 180ms ease-out, color 180ms ease-out, border-color 180ms ease-out;
}

.cinema-nav-link:hover {
  color: var(--text-color);
  background: var(--surface-hover);
}

.cinema-nav-link.router-link-active {
  border-left-color: var(--primary-color);
  background: var(--primary-soft-bg);
  color: var(--primary-hover-color);
}

.cinema-rail-foot {
  display: flex;
  flex-direction: column;
  gap: 10px;
  margin-top: auto;
  align-items: flex-start;
}

.cinema-user {
  padding: 0 12px;
  color: var(--text-muted-color);
  font-size: 13px;
}

.cinema-logout {
  min-height: 40px;
  padding: 0 12px;
  border: 1px solid var(--border-color);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.cinema-logout:hover {
  color: var(--danger-color);
  border-color: var(--danger-soft-border);
  background: var(--danger-soft-bg);
}

.cinema-stage {
  width: min(100%, 1200px);
  margin: 0 auto;
  padding: 44px 36px 64px;
}

@media (max-width: 860px) {
  .cinema-shell {
    grid-template-columns: 1fr;
  }

  .cinema-rail {
    position: sticky;
    height: auto;
    flex-direction: row;
    align-items: center;
    gap: 16px;
    padding: 12px 16px;
    border-right: 0;
    border-bottom: 1px solid var(--border-color);
  }

  .cinema-nav {
    flex-direction: row;
    margin-left: 8px;
    overflow-x: auto;
  }

  .cinema-rail-foot {
    flex-direction: row;
    margin-top: 0;
    margin-left: auto;
    align-items: center;
  }

  .cinema-stage {
    padding: 28px 16px 44px;
  }
}

@media (max-width: 560px) {
  .cinema-brand-wordmark {
    display: none;
  }
}

/* ===== 玻璃拟态（Glass）===== */
.glass-shell {
  min-height: 100dvh;
  padding-top: 26px;
}

.glass-dock {
  position: sticky;
  top: 14px;
  z-index: 30;
  width: min(100% - 32px, 1160px);
  margin: 0 auto;
  border-radius: 20px;
  border: 1px solid var(--border-color);
  background: rgba(255, 255, 255, 0.06);
  backdrop-filter: blur(22px) saturate(160%);
  box-shadow: var(--shadow-md);
}

.glass-dock-inner {
  display: grid;
  min-height: 62px;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 20px;
  padding: 8px 18px;
}

.glass-brand {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  gap: 10px;
  color: var(--text-color);
  font-size: 15px;
  font-weight: 700;
  text-decoration: none;
  white-space: nowrap;
}

.glass-brand-mark {
  display: inline-flex;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(165, 180, 252, 0.4);
  border-radius: 10px;
  background: rgba(94, 106, 210, 0.2);
  color: var(--accent-color);
  font-size: 11px;
  font-weight: 800;
}

.glass-nav,
.glass-session {
  display: flex;
  align-items: center;
  gap: 4px;
}

.glass-session {
  justify-content: flex-end;
  gap: 8px;
}

.glass-nav-link {
  display: inline-flex;
  min-height: 40px;
  align-items: center;
  padding: 0 14px;
  border-radius: 999px;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 600;
  text-decoration: none;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.glass-nav-link:hover {
  color: var(--text-color);
  background: rgba(255, 255, 255, 0.1);
}

.glass-nav-link.router-link-active {
  background: rgba(94, 106, 210, 0.24);
  color: var(--accent-color);
}

.glass-user {
  max-width: 130px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.glass-logout {
  min-height: 38px;
  padding: 0 14px;
  border: 1px solid rgba(255, 255, 255, 0.14);
  border-radius: 999px;
  background: rgba(255, 255, 255, 0.06);
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.glass-logout:hover {
  background: rgba(251, 113, 133, 0.12);
  color: var(--danger-color);
  border-color: rgba(251, 113, 133, 0.45);
}

.glass-content {
  width: min(100% - 40px, 1160px);
  margin: 0 auto;
  padding: 34px 0 64px;
}

@media (max-width: 760px) {
  .glass-shell {
    padding-top: 12px;
  }

  .glass-dock {
    top: 8px;
  }

  .glass-dock-inner {
    grid-template-columns: auto 1fr auto;
    gap: 10px;
    padding: 6px 12px;
  }

  .glass-nav {
    grid-column: 1 / -1;
    grid-row: 2;
    justify-content: center;
    overflow-x: auto;
    padding-bottom: 4px;
    scrollbar-width: none;
  }

  .glass-nav::-webkit-scrollbar {
    display: none;
  }

  .glass-brand-name,
  .glass-user {
    display: none;
  }

  .glass-content {
    width: min(100% - 24px, 1160px);
    padding-top: 22px;
  }
}

/* ===== 苹果浅色（Fluent）===== */
.fluent-shell {
  min-height: 100dvh;
}

.fluent-nav {
  position: sticky;
  top: 0;
  z-index: 20;
  border-bottom: 1px solid var(--border-color);
  background-color: var(--navbar-bg);
  backdrop-filter: saturate(180%) blur(20px);
}

.fluent-nav-inner {
  display: grid;
  width: min(100%, var(--content-width));
  min-height: 60px;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 32px;
  margin: 0 auto;
  padding: 8px 28px;
}

.fluent-brand {
  color: var(--text-color);
  font-size: 16px;
  font-weight: 700;
  letter-spacing: -0.01em;
  text-decoration: none;
}

.fluent-brand:hover {
  color: var(--primary-color);
}

.fluent-links,
.fluent-session {
  display: flex;
  align-items: center;
  gap: 4px;
}

.fluent-session {
  justify-content: flex-end;
  gap: 10px;
}

.fluent-link {
  display: inline-flex;
  min-height: 38px;
  align-items: center;
  padding: 0 14px;
  border-radius: 999px;
  color: var(--text-secondary);
  font-size: 14px;
  font-weight: 500;
  text-decoration: none;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.fluent-link:hover {
  background: var(--surface-hover);
  color: var(--text-color);
}

.fluent-link.router-link-active {
  background: var(--primary-soft-bg);
  color: var(--primary-color);
  font-weight: 600;
}

.fluent-user {
  max-width: 140px;
  overflow: hidden;
  color: var(--text-secondary);
  font-size: 13px;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.fluent-logout {
  min-height: 34px;
  padding: 0 16px;
  border: none;
  border-radius: 999px;
  background: var(--surface-hover);
  color: var(--text-color);
  cursor: pointer;
  font-size: 13px;
  font-weight: 500;
  transition: background-color 180ms ease-out, color 180ms ease-out;
}

.fluent-logout:hover {
  background: var(--danger-soft-bg);
  color: var(--danger-color);
}

.fluent-content {
  width: min(100%, 980px);
  margin: 0 auto;
  padding: 64px 28px 72px;
}

@media (max-width: 760px) {
  .fluent-nav-inner {
    grid-template-columns: auto 1fr auto;
    gap: 8px;
    padding: 8px 16px;
  }

  .fluent-links {
    justify-content: center;
    overflow-x: auto;
  }

  .fluent-user {
    display: none;
  }

  .fluent-content {
    width: min(100%, 980px);
    padding: 40px 16px 56px;
  }
}

/* ===== Bangumi 令牌面板（四套 data-ui 共用变量）===== */
.bangumi-trigger {
  min-height: 38px;
  padding: 0 12px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  font-size: 13px;
  font-weight: 600;
  transition: background-color 180ms ease-out, color 180ms ease-out, border-color 180ms ease-out;
}

.bangumi-trigger:hover {
  color: var(--text-color);
  background: var(--surface-hover);
  border-color: var(--border-strong-color);
}

.bangumi-backdrop {
  position: fixed;
  inset: 0;
  z-index: 80;
  display: grid;
  place-items: center;
  padding: 24px;
  background: rgba(0, 0, 0, 0.48);
}

.bangumi-panel {
  display: grid;
  width: min(100%, 420px);
  gap: 14px;
  padding: 24px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--surface-color);
  box-shadow: var(--shadow-lg);
}

.bangumi-panel-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}

.bangumi-kicker {
  margin-bottom: 4px;
  color: var(--accent-color);
  font-size: 12px;
  font-weight: 700;
  letter-spacing: 0.04em;
  text-transform: uppercase;
}

.bangumi-panel h2 {
  color: var(--text-color);
  font-size: 20px;
  font-weight: 700;
}

.bangumi-status,
.bangumi-hint,
.bangumi-label {
  color: var(--text-secondary);
  font-size: 14px;
  line-height: 1.5;
}

.bangumi-status {
  color: var(--text-color);
  font-weight: 600;
}

.bangumi-hint a {
  color: var(--primary-hover-color);
}

.bangumi-form,
.bangumi-actions {
  display: grid;
  gap: 10px;
}

.bangumi-form input {
  width: 100%;
  min-height: 44px;
  padding: 0 13px;
  border: 1px solid var(--border-strong-color);
  border-radius: var(--radius-sm);
  background: var(--surface-muted-color);
  color: var(--text-color);
  outline: none;
  font-size: 16px;
}

.bangumi-form input:focus {
  border-color: var(--accent-color);
  box-shadow: 0 0 0 4px var(--accent-glow);
}

.bangumi-error,
.bangumi-ok {
  margin: 0;
  padding: 10px 12px;
  border-radius: var(--radius-sm);
  font-size: 14px;
  line-height: 1.45;
}

.bangumi-error {
  border: 1px solid var(--danger-soft-border);
  background: var(--danger-soft-bg);
  color: var(--danger-color);
}

.bangumi-ok {
  border: 1px solid var(--success-border);
  background: var(--primary-soft-bg);
  color: var(--success-color);
}

</style>
