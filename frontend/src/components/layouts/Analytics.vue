<script setup lang="ts">
import{ useShell } from '../../composables/useShell'
import ThemeSwitcher from '../ThemeSwitcher.vue'

const { appStore, authStore, handleLogout } = useShell()
</script>

<template>
  <div class="app-layout modern-shell">
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
            <router-link to="/admin/update" class="nav-link">系统更新</router-link>
          </template>
        </nav>

        <div class="navbar-session">
          <ThemeSwitcher />
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