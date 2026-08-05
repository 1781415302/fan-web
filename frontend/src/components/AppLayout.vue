<script setup lang="ts">
import { useRouter } from 'vue-router'

import { useAuthStore } from '../stores/auth'
import { useAppStore } from '../stores/app'

const appStore = useAppStore()
const authStore = useAuthStore()
const router = useRouter()

async function handleLogout() {
  await authStore.logout()
  await router.replace({ name: 'login' })
}
</script>

<template>
  <div class="app-layout">
    <a class="skip-link" href="#main-content">跳转到主要内容</a>
    <header class="navbar">
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
          </template>
        </nav>

        <div class="navbar-session">
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <span class="user-name" :title="authStore.user.username">{{ authStore.user.username }}</span>
            <button type="button" class="logout-button" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="nav-link">
            登录
          </router-link>
          <span v-else class="nav-placeholder">加载中</span>
        </div>
      </div>
    </header>

    <main id="main-content" class="content" tabindex="-1">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.app-layout {
  min-height: 100dvh;
  display: flex;
  flex-direction: column;
}

.navbar {
  position: sticky;
  top: 0;
  z-index: 20;
  background-color: rgba(14, 17, 23, 0.92);
  backdrop-filter: blur(18px);
  border-bottom: 1px solid var(--border-color);
}

.navbar-inner {
  display: grid;
  width: min(100%, var(--content-width));
  min-height: 68px;
  grid-template-columns: auto 1fr auto;
  align-items: center;
  gap: 28px;
  margin: 0 auto;
  padding: 10px 28px;
}

.brand {
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

.brand-mark {
  display: inline-flex;
  width: 32px;
  height: 32px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(34, 197, 94, 0.6);
  border-radius: 8px;
  background: rgba(34, 197, 94, 0.14);
  color: var(--primary-hover-color);
  font-size: 11px;
  font-weight: 800;
}

.brand:hover {
  color: var(--primary-hover-color);
}

.navbar-nav,
.navbar-session {
  display: flex;
  align-items: center;
  gap: 4px;
}

.navbar-session {
  justify-content: flex-end;
  gap: 8px;
}

.nav-link,
.user-name,
.logout-button,
.nav-placeholder {
  font-size: 14px;
}

.nav-link {
  display: inline-flex;
  min-height: 44px;
  align-items: center;
  padding: 0 11px;
  border: 1px solid transparent;
  border-radius: var(--radius-sm);
  color: var(--text-secondary);
  text-decoration: none;
  transition: background-color 180ms ease-out, border-color 180ms ease-out, color 180ms ease-out;
}

.nav-link:hover {
  border-color: var(--border-color);
  background: var(--surface-color);
  color: var(--text-color);
}

.nav-link.router-link-active {
  border-color: rgba(34, 197, 94, 0.26);
  background: rgba(34, 197, 94, 0.1);
  color: var(--primary-hover-color);
}

.user-name {
  max-width: 150px;
  overflow: hidden;
  padding: 0 8px;
  color: var(--text-color);
  font-weight: 600;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.logout-button {
  min-height: 40px;
  padding: 0 11px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-sm);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  transition: background-color 180ms ease-out, border-color 180ms ease-out, color 180ms ease-out;
}

.logout-button:hover {
  border-color: rgba(255, 137, 144, 0.55);
  background: rgba(255, 137, 144, 0.08);
  color: var(--danger-color);
}

.nav-placeholder {
  color: var(--text-secondary);
}

.content {
  flex: 1;
  width: min(100%, var(--content-width));
  margin: 0 auto;
  padding: 36px 28px 56px;
}

@media (max-width: 760px) {
  .navbar-inner {
    grid-template-columns: auto 1fr;
    gap: 4px 16px;
    padding: 10px 16px;
  }

  .navbar-nav {
    grid-column: 1 / -1;
    grid-row: 2;
    overflow-x: auto;
    padding-bottom: 2px;
    scrollbar-width: none;
  }

  .navbar-nav::-webkit-scrollbar {
    display: none;
  }

  .navbar-session {
    grid-column: 2;
    grid-row: 1;
    gap: 4px;
  }

  .content {
    padding: 28px 16px 44px;
  }

  .nav-link {
    padding: 0 9px;
  }

  .user-name {
    max-width: 100px;
  }
}

@media (max-width: 420px) {
  .brand-name,
  .user-name {
    display: none;
  }

  .navbar-inner {
    gap: 4px 10px;
  }
}
</style>
