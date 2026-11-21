<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'

import api, { ApiError, type ApiResponse, unwrap } from '../api'
import { createAPIKey, deleteAPIKey, listAPIKeys } from '../api/apiKeys'
import { useAuthStore } from '../stores/auth'
import type { APIKeyItem, CreateAPIKeyResponse } from '../types/apiKey'
import type { User } from '../types/auth'

const authStore = useAuthStore()
const users = ref<User[]>([])
const loading = ref(true)
const submitting = ref(false)
const deletingID = ref<number | null>(null)
const showCreateForm = ref(false)
const errorMessage = ref('')
const formError = ref('')
const form = reactive({
  username: '',
  password: '',
})

// API Key 名称长度与后端一致：trim 后按字符（rune）计 1..64。
const API_KEY_NAME_MAX = 64
const apiKeys = ref<APIKeyItem[]>([])
const apiKeysLoading = ref(true)
const apiKeyError = ref('')
const apiKeyFormError = ref('')
const apiKeyName = ref('')
const creatingKey = ref(false)
const revokingKeyID = ref<number | null>(null)
// 明文 Key 只保存在组件内存中：关闭横幅或卸载页面即置 null，严禁写入 localStorage / sessionStorage。
const createdKey = ref<CreateAPIKeyResponse | null>(null)
const copyMessage = ref('')

onMounted(() => {
  void loadUsers()
  void loadAPIKeys()
})
onBeforeUnmount(() => {
  createdKey.value = null
  copyMessage.value = ''
})

async function loadUsers() {
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await api.get<ApiResponse<User[]>>('/admin/users')
    users.value = unwrap(response)
  } catch (error: unknown) {
    errorMessage.value = error instanceof ApiError ? error.message : '用户列表加载失败'
  } finally {
    loading.value = false
  }
}

async function handleCreate() {
  formError.value = ''
  submitting.value = true
  try {
    const response = await api.post<ApiResponse<User>>('/admin/users', {
      username: form.username,
      password: form.password,
    })
    const user = unwrap(response)
    users.value.push(user)
    form.username = ''
    form.password = ''
    showCreateForm.value = false
  } catch (error: unknown) {
    formError.value = error instanceof ApiError ? error.message : '创建用户失败'
  } finally {
    submitting.value = false
  }
}

async function handleDelete(user: User) {
  if (user.id === authStore.user?.id || !window.confirm(`确定删除用户“${user.username}”吗？`)) {
    return
  }

  deletingID.value = user.id
  errorMessage.value = ''
  try {
    const response = await api.delete<ApiResponse<null>>(`/admin/users/${user.id}`)
    unwrap(response)
    users.value = users.value.filter((item) => item.id !== user.id)
  } catch (error: unknown) {
    errorMessage.value = error instanceof ApiError ? error.message : '删除用户失败'
  } finally {
    deletingID.value = null
  }
}

async function loadAPIKeys() {
  apiKeysLoading.value = true
  apiKeyError.value = ''
  try {
    apiKeys.value = (await listAPIKeys()) ?? []
  } catch (error: unknown) {
    apiKeyError.value = error instanceof ApiError ? error.message : 'API 密钥列表加载失败'
  } finally {
    apiKeysLoading.value = false
  }
}

function validateAPIKeyName(name: string) {
  const length = Array.from(name).length
  if (length === 0) return '请填写备注名'
  if (length > API_KEY_NAME_MAX) return `备注名最多 ${API_KEY_NAME_MAX} 个字符`
  return ''
}

async function handleCreateAPIKey() {
  apiKeyFormError.value = ''
  const name = apiKeyName.value.trim()
  const invalid = validateAPIKeyName(name)
  if (invalid) {
    apiKeyFormError.value = invalid
    return
  }
  creatingKey.value = true
  try {
    const created = await createAPIKey(name)
    createdKey.value = created
    copyMessage.value = ''
    // 列表只保留掩码信息，不把明文 api_key 放进列表数据。
    apiKeys.value = [
      {
        id: created.id,
        name: created.name,
        key_prefix: created.key_prefix,
        created_by: created.created_by,
        last_used_at: created.last_used_at,
        created_at: created.created_at,
      },
      ...apiKeys.value,
    ]
    apiKeyName.value = ''
  } catch (error: unknown) {
    apiKeyFormError.value = error instanceof ApiError ? error.message : '生成密钥失败'
  } finally {
    creatingKey.value = false
  }
}

async function copyCreatedKey() {
  if (!createdKey.value) return
  try {
    await navigator.clipboard.writeText(createdKey.value.api_key)
    copyMessage.value = '已复制到剪贴板'
  } catch {
    copyMessage.value = '复制失败，请手动选中密钥复制'
  }
}

function dismissCreatedKey() {
  createdKey.value = null
  copyMessage.value = ''
}

async function handleRevokeAPIKey(item: APIKeyItem) {
  if (!window.confirm(`确定吊销密钥“${item.name}”吗？使用该密钥的外部工具将立即失效。`)) {
    return
  }

  revokingKeyID.value = item.id
  apiKeyError.value = ''
  try {
    await deleteAPIKey(item.id)
    apiKeys.value = apiKeys.value.filter((key) => key.id !== item.id)
    if (createdKey.value?.id === item.id) dismissCreatedKey()
  } catch (error: unknown) {
    apiKeyError.value = error instanceof ApiError ? error.message : '吊销密钥失败'
  } finally {
    revokingKeyID.value = null
  }
}

function formatDate(value: string) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) {
    return value
  }
  return new Intl.DateTimeFormat('zh-CN', {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}
</script>

<template>
  <section class="users-page" aria-labelledby="users-title">
    <header class="page-heading">
      <div>
        <p class="eyebrow">Admin console</p>
        <h1 id="users-title">用户管理</h1>
        <p class="page-description">管理可以访问私人番剧库的账号和权限。</p>
      </div>
      <button type="button" class="primary-btn" @click="showCreateForm = !showCreateForm">
        {{ showCreateForm ? '收起表单' : '新建用户' }}
      </button>
    </header>

    <div class="admin-summary" aria-label="用户统计">
      <div class="summary-item">
        <span>账号总数</span>
        <strong>{{ loading ? '...' : users.length }}</strong>
      </div>
      <div class="summary-item">
        <span>当前会话</span>
        <strong>{{ authStore.user?.username || '管理员' }}</strong>
      </div>
      <div class="summary-item">
        <span>当前权限</span>
        <strong>管理员</strong>
      </div>
    </div>

    <section v-if="showCreateForm" class="create-panel" aria-labelledby="create-user-title">
      <div class="panel-heading">
        <div>
          <p class="section-kicker">New account</p>
          <h2 id="create-user-title">新建用户</h2>
        </div>
        <span class="panel-note">提交后立即生效</span>
      </div>
      <form class="create-form" @submit.prevent="handleCreate">
        <div class="form-field">
          <label for="new-username">用户名</label>
          <input id="new-username" v-model="form.username" type="text" autocomplete="username" maxlength="64" required />
        </div>
        <div class="form-field">
          <label for="new-password">密码</label>
          <input id="new-password" v-model="form.password" type="password" autocomplete="new-password" required />
          <p class="field-hint" style="margin-top: 6px; color: var(--text-muted-color); font-size: 12px;">
            至少 8 个字符，最多 72 字节。
          </p>
        </div>
        <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
        <div class="form-actions">
          <button class="primary-btn" type="submit" :disabled="submitting">
            {{ submitting ? '创建中...' : '创建账号' }}
          </button>
        </div>
      </form>
    </section>

    <p v-if="errorMessage" class="error-msg" role="alert">{{ errorMessage }}</p>
    <section class="users-panel" aria-labelledby="users-table-title">
      <div class="panel-heading table-heading">
        <div>
          <p class="section-kicker">Accounts</p>
          <h2 id="users-table-title">账号列表</h2>
        </div>
        <span class="panel-note">删除操作不可撤销</span>
      </div>
      <div v-if="loading" class="empty-state" aria-live="polite">正在加载账号...</div>
      <div v-else-if="users.length === 0" class="empty-state">暂无用户</div>
      <div v-else class="table-wrapper">
        <table aria-label="用户账号列表">
          <caption class="visually-hidden">用户账号、角色、创建时间和操作</caption>
          <thead>
            <tr>
              <th scope="col">用户名</th>
              <th scope="col">角色</th>
              <th scope="col">创建时间</th>
              <th scope="col" class="action-column">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td class="username-cell" data-label="用户名">{{ user.username }}</td>
              <td data-label="角色">
                <span class="role-label">{{ user.is_admin ? '管理员' : '用户' }}</span>
              </td>
              <td data-label="创建时间">{{ formatDate(user.created_at) }}</td>
              <td class="action-column" data-label="操作">
                <button
                  v-if="user.id !== authStore.user?.id"
                  class="action-btn danger"
                  type="button"
                  :disabled="deletingID === user.id"
                  @click="handleDelete(user)"
                >
                  {{ deletingID === user.id ? '删除中...' : '删除' }}
                </button>
                <span v-else class="muted-label">当前账号</span>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>

    <section class="api-keys-panel" aria-labelledby="api-keys-title">
      <div class="panel-heading table-heading">
        <div>
          <p class="section-kicker">API keys</p>
          <h2 id="api-keys-title">API 密钥（外部工具接入）</h2>
        </div>
        <span class="panel-note">供 Hermes 等外部工具调用，吊销后立即失效</span>
      </div>
      <form class="api-key-form" @submit.prevent="handleCreateAPIKey">
        <div class="form-field">
          <label for="api-key-name">备注名</label>
          <input
            id="api-key-name"
            v-model="apiKeyName"
            type="text"
            autocomplete="off"
            placeholder="如 Hermes 下载机"
          />
          <p class="field-hint">1 到 64 个字符，用于区分不同的接入方。</p>
        </div>
        <div class="api-key-actions">
          <button class="primary-btn" type="submit" :disabled="creatingKey">
            {{ creatingKey ? '生成中...' : '生成密钥' }}
          </button>
        </div>
        <p v-if="apiKeyFormError" class="form-error" role="alert">{{ apiKeyFormError }}</p>
      </form>

      <div v-if="createdKey" class="api-key-banner" role="status">
        <div class="api-key-banner-text">
          <strong>请立即复制并妥善保存该密钥</strong>
          <p>完整密钥只显示这一次，关闭后无法再次查看。</p>
          <code class="api-key-secret">{{ createdKey.api_key }}</code>
          <p v-if="copyMessage" class="api-key-copy-message">{{ copyMessage }}</p>
        </div>
        <div class="api-key-banner-actions">
          <button type="button" class="action-btn api-key-copy-btn" @click="copyCreatedKey">复制</button>
          <button type="button" class="action-btn api-key-dismiss-btn" @click="dismissCreatedKey">关闭</button>
        </div>
      </div>

      <p v-if="apiKeyError" class="error-msg api-key-error" role="alert">{{ apiKeyError }}</p>
      <div v-if="apiKeysLoading" class="empty-state" aria-live="polite">正在加载密钥...</div>
      <div v-else-if="apiKeys.length === 0" class="empty-state">暂无 API 密钥</div>
      <div v-else class="table-wrapper">
        <table aria-label="API 密钥列表">
          <caption class="visually-hidden">API 密钥名称、掩码、创建时间、最近使用时间和操作</caption>
          <thead>
            <tr>
              <th scope="col">名称</th>
              <th scope="col">密钥</th>
              <th scope="col">创建时间</th>
              <th scope="col">最近使用</th>
              <th scope="col" class="action-column">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="item in apiKeys" :key="item.id" class="api-key-row">
              <td class="username-cell" data-label="名称">{{ item.name }}</td>
              <td data-label="密钥"><code class="key-prefix">{{ item.key_prefix }}</code></td>
              <td data-label="创建时间">{{ formatDate(item.created_at) }}</td>
              <td data-label="最近使用">{{ item.last_used_at ? formatDate(item.last_used_at) : '从未使用' }}</td>
              <td class="action-column" data-label="操作">
                <button
                  class="action-btn danger"
                  type="button"
                  :disabled="revokingKeyID === item.id"
                  @click="handleRevokeAPIKey(item)"
                >
                  {{ revokingKeyID === item.id ? '吊销中...' : '吊销' }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </section>
  </section>
</template>

<style scoped>
.users-page { max-width: 1120px; margin: 0 auto; padding-bottom: 32px; }
.page-heading { display: flex; align-items: end; justify-content: space-between; gap: 24px; padding: 12px 0 28px; border-bottom: 1px solid var(--border-color); }
h1 { color: var(--text-color); font-size: 36px; font-weight: 700; line-height: 1.15; }
.page-description { margin-top: 10px; color: var(--text-secondary); font-size: 15px; }
.admin-summary { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 16px; padding: 24px 0; border-bottom: 1px solid var(--border-color); }
.summary-item { min-width: 0; padding-left: 16px; border-left: 1px solid var(--border-color); }
.summary-item:first-child { padding-left: 0; border-left: 0; }
.summary-item span { display: block; color: var(--text-muted-color); font-size: 12px; }
.summary-item strong { display: block; margin-top: 6px; overflow: hidden; color: var(--text-color); font-size: 16px; text-overflow: ellipsis; white-space: nowrap; }
.create-panel, .users-panel, .api-keys-panel { margin-top: 24px; border: 1px solid var(--border-color); border-radius: var(--radius-md); background: var(--surface-color); box-shadow: var(--shadow-sm); }
.create-panel { padding: 22px; }
.panel-heading { display: flex; align-items: end; justify-content: space-between; gap: 16px; }
.section-kicker { margin-bottom: 5px; color: var(--text-muted-color); font-size: 11px; font-weight: 700; text-transform: uppercase; }
.panel-heading h2 { color: var(--text-color); font-size: 20px; }
.panel-note { color: var(--text-muted-color); font-size: 12px; }
.create-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; margin-top: 20px; }
.form-field { min-width: 0; }
.form-field label { display: block; margin-bottom: 7px; color: var(--text-secondary); font-size: 13px; font-weight: 600; }
.form-field input { width: 100%; min-height: 46px; padding: 0 12px; border: 1px solid var(--border-strong-color); border-radius: var(--radius-sm); background: var(--surface-muted-color); color: var(--text-color); outline: none; font-size: 16px; transition: border-color 180ms ease-out, box-shadow 180ms ease-out; }
.form-field input:focus { border-color: var(--accent-color); box-shadow: 0 0 0 4px var(--accent-glow); }
.form-error { grid-column: 1 / -1; margin: 0; color: var(--danger-color); font-size: 14px; }
.form-actions { grid-column: 1 / -1; display: flex; justify-content: flex-end; }
.table-heading { padding: 20px 22px 16px; border-bottom: 1px solid var(--border-color); }
.table-wrapper { overflow-x: auto; }
table { width: 100%; min-width: 640px; border-collapse: collapse; text-align: left; }
th, td { padding: 15px 22px; border-bottom: 1px solid var(--border-color); white-space: nowrap; }
th { color: var(--text-secondary); font-size: 12px; font-weight: 600; }
td { color: var(--text-color); font-size: 14px; }
tbody tr:last-child td { border-bottom: 0; }
.username-cell { font-weight: 600; }
.role-label { color: var(--primary-hover-color); }
.action-column { text-align: right; }
.muted-label { color: var(--text-muted-color); font-size: 13px; }
.empty-state { padding: 58px 24px; color: var(--text-secondary); text-align: center; }
.api-key-form { display: grid; grid-template-columns: minmax(0, 1fr) auto; align-items: start; gap: 16px; padding: 20px 22px; border-bottom: 1px solid var(--border-color); }
.api-key-actions { padding-top: 27px; }
.field-hint { margin-top: 6px; color: var(--text-muted-color); font-size: 12px; }
.api-key-banner { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin: 16px 22px; padding: 16px; border: 1px solid var(--warning-border); border-radius: var(--radius-md); background: var(--surface-muted-color); }
.api-key-banner-text { min-width: 0; }
.api-key-banner-text strong { display: block; color: var(--warning-color); font-size: 14px; }
.api-key-banner-text p { margin-top: 4px; color: var(--text-secondary); font-size: 13px; }
.api-key-secret { display: block; margin-top: 10px; padding: 10px 12px; overflow-wrap: anywhere; border: 1px solid var(--border-color); border-radius: var(--radius-sm); background: var(--surface-color); color: var(--text-color); font-size: 13px; user-select: all; }
.api-key-banner-actions { display: flex; flex: 0 0 auto; gap: 8px; }
.api-key-error { margin: 16px 22px 0; }
.key-prefix { color: var(--text-secondary); font-size: 13px; }
@media (max-width: 680px) {
  .page-heading { align-items: flex-start; flex-direction: column; gap: 18px; }
  .page-heading .primary-btn { width: 100%; }
  .create-form { grid-template-columns: 1fr; }
  .form-actions { grid-column: auto; justify-content: stretch; }
  .form-actions .primary-btn { width: 100%; }
  .api-key-form { grid-template-columns: 1fr; }
  .api-key-actions { padding-top: 0; }
  .api-key-actions .primary-btn { width: 100%; }
  .api-key-banner { flex-direction: column; }
  .table-wrapper { overflow-x: visible; }
  table { display: block; min-width: 0; }
  thead { display: none; }
  tbody, tr { display: block; }
  tbody tr { padding: 8px 18px; border-bottom: 1px solid var(--border-color); }
  tbody tr:last-child { border-bottom: 0; }
  td { display: flex; min-height: 44px; align-items: center; justify-content: space-between; gap: 16px; padding: 10px 0; border-bottom: 0; white-space: normal; text-align: right; }
  td::before { content: attr(data-label); flex: 0 0 auto; color: var(--text-muted-color); font-size: 12px; font-weight: 500; text-align: left; }
  td.action-column { text-align: right; }
}
@media (max-width: 480px) { h1 { font-size: 32px; } .admin-summary { gap: 8px; } .summary-item { padding-left: 8px; } .summary-item strong { font-size: 13px; } .panel-heading { align-items: flex-start; flex-direction: column; gap: 8px; } .table-heading { align-items: flex-start; flex-direction: row; } .table-heading .panel-note { margin-left: auto; text-align: right; } .create-panel { padding: 18px; } th, td { padding-right: 14px; padding-left: 14px; } }
</style>
