<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'

import { ApiError } from '../api'
import type { LoginData } from '../types/auth'
import { submitSetup } from '../api/setup'
import { useAuthStore } from '../stores/auth'

const router = useRouter()
const authStore = useAuthStore()

const username = ref('')
const password = ref('')
const confirmPassword = ref('')
const videoRootPath = ref('')
const port = ref('')
const loading = ref(false)
const errorMessage = ref('')

async function handleSubmit() {
  errorMessage.value = ''

  if (!username.value.trim() || !password.value) {
    errorMessage.value = '请填写管理员用户名和密码'
    return
  }
  if (username.value.trim().length > 64) {
    errorMessage.value = '用户名最多 64 个字符'
    return
  }
  if (password.value.length < 8) {
    errorMessage.value = '密码至少 8 个字符'
    return
  }
  if (new TextEncoder().encode(password.value).length > 72) {
    errorMessage.value = '密码最多 72 字节'
    return
  }
  if (password.value !== confirmPassword.value) {
    errorMessage.value = '两次输入的密码不一致'
    return
  }
  if (!videoRootPath.value.trim()) {
    errorMessage.value = '请填写视频根目录路径'
    return
  }
  const parsedPort = port.value === '' ? undefined : Number(port.value)
  if (parsedPort !== undefined && (!Number.isInteger(parsedPort) || parsedPort < 1 || parsedPort > 65535)) {
    errorMessage.value = '端口号需为 1-65535 之间的整数'
    return
  }

  loading.value = true
  try {
    const session: LoginData = await submitSetup({
      username: username.value.trim(),
      password: password.value,
      videoRootPath: videoRootPath.value.trim(),
      port: parsedPort,
    })

    authStore.setSession(session.token, session.user)
    await router.replace({ name: 'home' })
  } catch (error: unknown) {
    errorMessage.value = error instanceof ApiError ? error.message : '初始化失败，请稍后重试'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <section class="auth-page" aria-labelledby="setup-title">
    <div class="auth-panel">
      <div class="auth-heading">
        <span class="auth-mark" aria-hidden="true">FW</span>
        <div>
          <p class="auth-kicker">First-run setup</p>
          <h1 id="setup-title">初始化</h1>
          <p class="auth-description">首次运行，请配置管理员账号与视频库。</p>
        </div>
      </div>

      <form class="setup-form" @submit.prevent="handleSubmit">
        <div class="form-field">
          <label for="username">管理员用户名</label>
          <input id="username" v-model="username" name="username" type="text" autocomplete="username" maxlength="64" required autofocus />
        </div>

        <div class="form-field">
          <label for="password">密码</label>
          <input id="password" v-model="password" name="password" type="password" autocomplete="new-password" required />
          <p class="field-hint">至少 8 个字符，最多 72 字节。</p>
        </div>

        <div class="form-field">
          <label for="confirm-password">确认密码</label>
          <input id="confirm-password" v-model="confirmPassword" name="confirm-password" type="password" autocomplete="new-password" required />
        </div>

        <div class="form-field">
          <label for="video-root">视频根目录（服务器上的绝对路径）</label>
          <input
            id="video-root"
            v-model="videoRootPath"
            name="video-root"
            type="text"
            placeholder="/home/user/anime"
            autocomplete="off"
            spellcheck="false"
            required
          />
          <p class="field-hint">浏览器无法直接选择服务器本地目录，请手动输入。</p>
        </div>

        <div class="form-field">
          <label for="port">服务器端口（可选，留空用默认）</label>
          <input id="port" v-model="port" name="port" type="number" inputmode="numeric" min="1" max="65535" placeholder="8080" />
        </div>

        <p v-if="errorMessage" class="form-error" role="alert" aria-live="assertive">{{ errorMessage }}</p>
        <button class="primary-btn auth-submit" type="submit" :disabled="loading" :aria-busy="loading">
          {{ loading ? '初始化中...' : '完成并进入' }}
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
  border: 1px solid var(--primary-soft-border);
  border-radius: var(--radius-sm);
  background: var(--primary-soft-bg);
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

.setup-form {
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
  box-shadow: 0 0 0 4px var(--accent-glow);
}

.field-hint {
  margin: 2px 0 0;
  color: var(--text-muted-color);
  font-size: 12px;
  line-height: 1.5;
}

.form-error {
  margin: -2px 0 0;
  padding: 11px 12px;
  border: 1px solid var(--danger-soft-border);
  border-radius: var(--radius-sm);
  background: var(--danger-soft-bg);
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