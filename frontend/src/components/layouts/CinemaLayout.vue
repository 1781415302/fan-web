<script setup lang="ts">
import { useShell } from '../../composables/useShell'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const { appStore, authStore, handleLogout } = useShell()
</script>

<template>
  <div class="cinema-shell">
    <a class="skip-link" href="#main-content">跳转到主要内容</a>

    <aside class="cinema-rail" aria-label="主导航">
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
          <button type="button" class="cinema-logout" @click="handleLogout">退出登录</button>
        </template>
        <router-link v-else-if="authStore.initialized" to="/login" class="cinema-nav-link">
          登录
        </router-link>
        <span v-else class="cinema-user">加载中</span>
      </div>
    </aside>

    <main id="main-content" class="cinema-stage" tabindex="-1">
      <slot />
    </main>
  </div>
</template>

<style scoped>
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
</style>