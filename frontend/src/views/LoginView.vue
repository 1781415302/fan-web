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
      <div class="auth-heading">
        <span class="auth-mark" aria-hidden="true">FW</span>
        <div>
          <p class="auth-kicker">Private library</p>
          <h1 id="login-title">登录</h1>
          <p class="auth-description">进入你的私人番剧库。</p>
        </div>
      </div>
      <form class="login-form" @submit.prevent="handleSubmit">
        <div class="form-field">
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
        </div>

        <div class="form-field">
          <label for="password">密码</label>
          <input
            id="password"
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
          />
        </div>

        <p v-if="errorMessage" class="form-error" role="alert" aria-live="assertive">{{ errorMessage }}</p>
        <button class="primary-btn auth-submit" type="submit" :disabled="loading" :aria-busy="loading">
          {{ loading ? '登录中...' : '登录' }}
        </button>
      </form>
    </div>
  </section>
</template>

<style scoped>
.auth-page {
  min-height: calc(100dvh - 104px);
  display: grid;
  place-items: center;
  padding: 24px 0;
}

.auth-panel {
  width: min(100%, 440px);
  padding: 32px;
  border: 1px solid var(--border-color);
  border-radius: var(--radius-md);
  background: var(--surface-color);
  box-shadow: var(--shadow-md);
}

.auth-heading {
  display: flex;
  align-items: flex-start;
  gap: 16px;
  padding-bottom: 24px;
  border-bottom: 1px solid var(--border-color);
}

.auth-mark {
  display: inline-flex;
  width: 40px;
  height: 40px;
  flex: 0 0 40px;
  align-items: center;
  justify-content: center;
  border: 1px solid rgba(34, 197, 94, 0.62);
  border-radius: var(--radius-sm);
  background: rgba(34, 197, 94, 0.12);
  color: var(--primary-hover-color);
  font-size: 12px;
  font-weight: 800;
}

.auth-kicker {
  margin-bottom: 6px;
  color: var(--accent-color);
  font-size: 12px;
  font-weight: 700;
  line-height: 1.4;
  text-transform: uppercase;
}

h1 {
  color: var(--text-color);
  font-size: 30px;
  font-weight: 700;
  line-height: 1.15;
}

.auth-description {
  margin-top: 8px;
  color: var(--text-secondary);
  font-size: 14px;
}

.login-form {
  display: grid;
  gap: 16px;
  margin-top: 24px;
}

.form-field {
  display: grid;
  gap: 7px;
}

label {
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

input {
  width: 100%;
  min-height: 48px;
  padding: 0 13px;
  border: 1px solid var(--border-strong-color);
  border-radius: var(--radius-sm);
  background: var(--surface-muted-color);
  color: var(--text-color);
  outline: none;
  font-size: 16px;
  transition: border-color 180ms ease-out, box-shadow 180ms ease-out, background-color 180ms ease-out;
}

input:focus {
  border-color: var(--accent-color);
  background: var(--bg-color);
  box-shadow: 0 0 0 4px rgba(115, 217, 207, 0.12);
}

input::placeholder {
  color: var(--text-muted-color);
}

.form-error {
  margin: -2px 0 0;
  padding: 11px 12px;
  border: 1px solid rgba(255, 137, 144, 0.38);
  border-radius: var(--radius-sm);
  background: rgba(255, 137, 144, 0.08);
  color: var(--danger-color);
  font-size: 14px;
  line-height: 1.45;
}

.auth-submit {
  width: 100%;
  margin-top: 2px;
}

@media (max-width: 480px) {
  .auth-panel {
    padding: 24px 20px;
  }
}
</style>
