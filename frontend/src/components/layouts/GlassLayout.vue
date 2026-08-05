<script setup lang="ts">
import { useShell } from '../../composables/useShell'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const { appStore, authStore, handleLogout } = useShell()
</script>

<template>
  <div class="glass-shell">
    <a class="skip-link" href="#main-content">跳转到主要内容</a>

    <header class="glass-dock" aria-label="主导航">
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
          </template>
        </nav>

        <div class="glass-session">
          <ThemeSwitcher />
          <template v-if="authStore.initialized && authStore.isAuthenticated && authStore.user">
            <span class="glass-user" :title="authStore.user.username">{{ authStore.user.username }}</span>
            <button type="button" class="glass-logout" @click="handleLogout">退出登录</button>
          </template>
          <router-link v-else-if="authStore.initialized" to="/login" class="glass-nav-link">
            登录
          </router-link>
          <span v-else class="glass-user">加载中</span>
        </div>
      </div>
    </header>

    <main id="main-content" class="glass-content" tabindex="-1">
      <slot />
    </main>
  </div>
</template>

<style scoped>
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
</style>