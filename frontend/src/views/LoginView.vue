<script setup lang="ts">
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { ApiError } from '../api'
import { useAuthStore } from '../stores/auth'

const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()

const username = ref('')
const password = ref('')
const loading = ref(false)
const errorMessage = ref('')

async function handleSubmit() {
  errorMessage.value = ''
  loading.value = true

  try {
    await authStore.login(username.value, password.value)
    await router.replace(getSafeRedirect(route.query.redirect))
  } catch (error: unknown) {
    errorMessage.value = error instanceof ApiError ? error.message : '登录失败，请稍后重试'
  } finally {
    loading.value = false
  }
}

function getSafeRedirect(value: unknown) {
  if (typeof value === 'string' && value.startsWith('/') && !value.startsWith('//')) {
    return value
  }
  return { name: 'home' }
}
</script>

<template>
  <section class="auth-page" aria-labelledby="login-title">
    <div class="auth-panel">
      <p class="auth-kicker">番剧库</p>
      <h1 id="login-title">登录</h1>
      <form class="login-form" @submit.prevent="handleSubmit">
        <label for="username">用户名</label>
        <input
          id="username"
          v-model="username"
          name="username"
          type="text"
          autocomplete="username"
          required
          autofocus
        />

        <label for="password">密码</label>
        <input
          id="password"
          v-model="password"
          name="password"
          type="password"
          autocomplete="current-password"
          required
        />

        <p v-if="errorMessage" class="form-error" role="alert">{{ errorMessage }}</p>
        <button class="submit-button" type="submit" :disabled="loading">
          {{ loading ? '登录中...' : '登录' }}
        </button>
      </form>
    </div>
  </section>
</template>

<style scoped>
.auth-page {
  min-height: calc(100vh - 104px);
  display: grid;
  place-items: center;
}

.auth-panel {
  width: min(100%, 400px);
  padding: 32px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--surface-color);
}

.auth-kicker {
  margin-bottom: 4px;
  color: var(--primary-color);
  font-size: 14px;
  font-weight: 600;
}

h1 {
  margin-bottom: 24px;
  color: var(--text-color);
  font-size: 28px;
}

.login-form {
  display: grid;
  gap: 10px;
}

label {
  color: var(--text-secondary);
  font-size: 14px;
}

input {
  width: 100%;
  min-height: 42px;
  padding: 8px 12px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: var(--bg-color);
  color: var(--text-color);
  outline: none;
}

input:focus {
  border-color: var(--primary-color);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--primary-color) 25%, transparent);
}

.form-error {
  margin-top: 4px;
  color: #f87171;
  font-size: 14px;
}

.submit-button {
  min-height: 42px;
  margin-top: 10px;
  border: 0;
  border-radius: 4px;
  background: var(--primary-color);
  color: #fff;
  cursor: pointer;
  font-weight: 600;
}

.submit-button:hover {
  background: var(--primary-hover-color);
}

.submit-button:disabled {
  cursor: wait;
  opacity: 0.65;
}

@media (max-width: 480px) {
  .auth-panel {
    padding: 24px;
  }
}
</style>
