<script setup lang="ts">
import { useShell } from '../../composables/useShell'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const { appStore, authStore, handleLogout } = useShell()
</script>

<template>
  <div class="fluent-shell">
    <a class="skip-link" href="#main-content">跳转到主要内容</a>

    <header class="fluent-nav">
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
            <button type="button" class="fluent-logout" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="fluent-link">
            登录
          </router-link>
          <span v-else class="fluent-user">加载中</span>
        </div>
      </div>
    </header>

    <main id="main-content" class="fluent-content" tabindex="-1">
      <slot />
    </main>
  </div>
</template>

<style scoped>
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
</style>