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
    <header class="navbar">
      <div class="navbar-inner">
        <div class="navbar-left">
          <router-link to="/" class="logo">{{ appStore.siteName }}</router-link>
          <router-link
            v-if="authStore.initialized && authStore.isAuthenticated"
            to="/animes"
            class="nav-link"
          >
            番剧
          </router-link>
        </div>
        <nav class="nav-actions" aria-label="主导航">
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <router-link v-if="authStore.isAdmin" to="/admin/users" class="nav-link">
              用户管理
            </router-link>
            <span class="user-name">{{ authStore.user.username }}</span>
            <button type="button" class="logout-button" @click="handleLogout">退出</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="nav-link">
            登录
          </router-link>
          <span v-else class="nav-placeholder">加载中</span>
        </nav>
      </div>
    </header>

    <main class="content">
      <slot />
    </main>
  </div>
</template>

<style scoped>
.app-layout {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
}

.navbar {
  position: sticky;
  top: 0;
  z-index: 10;
  background-color: var(--surface-color);
  border-bottom: 1px solid var(--border-color);
}

.navbar-inner {
  width: min(100%, 1400px);
  height: 56px;
  margin: 0 auto;
  padding: 0 24px;
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.logo {
  color: var(--primary-color);
  font-size: 20px;
  font-weight: 700;
  text-decoration: none;
}

.logo:hover {
  color: var(--primary-hover-color);
}

.nav-actions {
  display: flex;
  align-items: center;
  gap: 16px;
}

.navbar-left {
  display: flex;
  align-items: center;
  gap: 20px;
}

.nav-link,
.user-name,
.logout-button,
.nav-placeholder {
  font-size: 14px;
}

.nav-link {
  color: var(--text-secondary);
  text-decoration: none;
}

.nav-link:hover {
  color: var(--primary-hover-color);
}

.user-name {
  color: var(--text-color);
  font-weight: 600;
}

.logout-button {
  padding: 5px 10px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
}

.logout-button:hover {
  border-color: var(--primary-color);
  color: var(--text-color);
}

.nav-placeholder {
  color: var(--text-secondary);
}

.content {
  flex: 1;
  width: min(100%, 1400px);
  margin: 0 auto;
  padding: 24px;
}

@media (max-width: 768px) {
  .navbar-inner {
    padding: 0 16px;
  }

  .content {
    padding: 16px;
  }

  .nav-actions {
    gap: 10px;
  }

  .navbar-left {
    gap: 12px;
  }
}
</style>
