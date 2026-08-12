<script setup lang="ts">
import { ref } from 'vue'

import { ApiError } from '../api'
import { checkUpdate, performUpdate, type UpdateCheckData } from '../api/update'

const checking = ref(false)
const updating = ref(false)
const result = ref<UpdateCheckData | null>(null)
const errorMessage = ref('')
const successMessage = ref('')

async function handleCheck() {
  checking.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    result.value = await checkUpdate()
  } catch (error: unknown) {
    errorMessage.value = error instanceof ApiError ? error.message : '检查更新失败'
  } finally {
    checking.value = false
  }
}

async function handleUpdate() {
  if (!result.value?.has_update) return
  if (!window.confirm(`确定更新到 ${result.value.latest_version} 吗？更新后服务将自动重启。`)) return
  updating.value = true
  errorMessage.value = ''
  successMessage.value = ''
  try {
    const data = await performUpdate()
    successMessage.value = `${data.message} ${data.hint ?? ''}`.trim()
  } catch (error: unknown) {
    // 即使请求超时/失败，服务端仍可能继续下载并重启，明确提示避免用户立即重试
    // （两个并发 PerformUpdate 会以 O_TRUNC 写同一临时文件，导致校验失败）。
    errorMessage.value = (error instanceof ApiError ? error.message : '更新失败') + '，服务端可能仍在更新，请稍候'
  } finally {
    updating.value = false
  }
}
</script>

<template>
  <section class="update-page" aria-labelledby="update-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">System</p>
        <h1 id="update-title">系统更新</h1>
        <p class="page-description">从 GitHub Releases 检查并更新服务器版本。仅管理员可操作。</p>
      </div>
      <button type="button" class="primary-btn" :disabled="checking || updating" @click="handleCheck">
        {{ checking ? '检查中...' : '检查更新' }}
      </button>
    </header>

    <p v-if="errorMessage" class="error-msg" role="alert">{{ errorMessage }}</p>
    <p v-if="successMessage" class="success-msg" role="status">{{ successMessage }}</p>

    <section v-if="result" class="update-panel" aria-label="更新信息">
      <div class="panel-heading">
        <div>
          <p class="section-kicker">Update status</p>
          <h2>版本信息</h2>
        </div>
        <span class="panel-note">{{ result.has_update ? '有新版本' : '已是最新' }}</span>
      </div>
      <div class="version-grid">
        <div class="version-item">
          <span>当前版本</span>
          <strong>{{ result.current_version }}</strong>
        </div>
        <div class="version-item">
          <span>最新版本</span>
          <strong>{{ result.latest_version || '—' }}</strong>
        </div>
      </div>
      <p v-if="result.error" class="hint-text">{{ result.error }}</p>
      <div v-if="result.release_notes" class="release-notes">
        <h3>更新内容</h3>
        <pre>{{ result.release_notes }}</pre>
      </div>
      <div v-if="result.has_update" class="panel-actions">
        <button type="button" class="primary-btn" :disabled="updating" @click="handleUpdate">
          {{ updating ? '更新中...' : '立即更新' }}
        </button>
        <span v-if="result.download_size" class="panel-note">安装包约 {{ (result.download_size / 1024 / 1024).toFixed(1) }} MB</span>
      </div>
      <p v-else-if="!result.error" class="hint-text">当前已是最新版本，无需更新。</p>
    </section>

    <section v-else class="update-panel empty">
      <p class="hint-text">点击"检查更新"获取最新版本信息。</p>
    </section>
  </section>
</template>

<style scoped>
.update-page { max-width: 1120px; margin: 0 auto; padding-bottom: 32px; }
.page-heading { display: flex; align-items: end; justify-content: space-between; gap: 24px; padding: 12px 0 28px; border-bottom: 1px solid var(--border-color); }
h1 { color: var(--text-color); font-size: 36px; font-weight: 700; line-height: 1.15; }
.page-description { margin-top: 10px; color: var(--text-secondary); font-size: 15px; }
.update-panel { margin-top: 24px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-sm); padding: 22px; }
.update-panel.empty { text-align: center; padding: 40px 22px; }
.panel-heading { display: flex; align-items: end; justify-content: space-between; gap: 16px; }
.section-kicker { margin-bottom: 5px; color: var(--text-muted-color); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.panel-heading h2 { color: var(--text-color); font-size: 20px; }
.panel-note { color: var(--text-muted-color); font-size: 12px; }
.version-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 20px; }
.version-item { padding: 14px 16px; border: 1px solid var(--border-color); border-radius: var(--radius-sm); background: var(--surface-muted-color); }
.version-item span { display: block; color: var(--text-muted-color); font-size: 12px; }
.version-item strong { display: block; margin-top: 6px; color: var(--text-color); font-size: 16px; word-break: break-all; }
.release-notes { margin-top: 20px; padding-top: 16px; border-top: 1px solid var(--border-color); }
.release-notes h3 { color: var(--text-color); font-size: 14px; margin-bottom: 8px; }
.release-notes pre { white-space: pre-wrap; word-break: break-word; background: var(--surface-muted-color); border: 1px solid var(--border-color); border-radius: var(--radius-sm); padding: 12px; font-size: 13px; color: var(--text-secondary); max-height: 360px; overflow: auto; }
.panel-actions { display: flex; align-items: center; gap: 12px; margin-top: 20px; }
.hint-text { margin-top: 16px; color: var(--text-secondary); font-size: 14px; }
.error-msg { margin-top: 16px; color: var(--danger-color); font-size: 14px; }
.success-msg { margin-top: 16px; color: #16a34a; font-size: 14px; }
</style>
