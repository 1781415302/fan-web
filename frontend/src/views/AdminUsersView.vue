<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'

import api, { ApiError, type ApiResponse, unwrap } from '../api'
import { useAuthStore } from '../stores/auth'
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

onMounted(loadUsers)

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
    <div class="page-heading">
      <div>
        <p class="page-kicker">管理</p>
        <h1 id="users-title">用户管理</h1>
      </div>
      <button class="primary-button" type="button" @click="showCreateForm = !showCreateForm">
        {{ showCreateForm ? '取消' : '新建用户' }}
      </button>
    </div>

    <form v-if="showCreateForm" class="create-form" @submit.prevent="handleCreate">
      <div class="form-field">
        <label for="new-username">用户名</label>
        <input id="new-username" v-model="form.username" type="text" required />
      </div>
      <div class="form-field">
        <label for="new-password">密码</label>
        <input id="new-password" v-model="form.password" type="password" required />
      </div>
      <p v-if="formError" class="form-error" role="alert">{{ formError }}</p>
      <button class="primary-button" type="submit" :disabled="submitting">
        {{ submitting ? '创建中...' : '创建' }}
      </button>
    </form>

    <p v-if="errorMessage" class="form-error" role="alert">{{ errorMessage }}</p>
    <div class="users-panel">
      <div v-if="loading" class="empty-state">加载中...</div>
      <div v-else-if="users.length === 0" class="empty-state">暂无用户</div>
      <div v-else class="table-wrapper">
        <table>
          <thead>
            <tr>
              <th>用户名</th>
              <th>角色</th>
              <th>创建时间</th>
              <th class="action-column">操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="user in users" :key="user.id">
              <td>{{ user.username }}</td>
              <td>
                <span class="role-label">{{ user.is_admin ? '管理员' : '用户' }}</span>
              </td>
              <td>{{ formatDate(user.created_at) }}</td>
              <td class="action-column">
                <button
                  v-if="user.id !== authStore.user?.id"
                  class="danger-button"
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
    </div>
  </section>
</template>

<style scoped>
.users-page {
  padding: 8px 0 40px;
}

.page-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 24px;
  margin-bottom: 24px;
}

.page-kicker {
  margin-bottom: 4px;
  color: var(--primary-color);
  font-size: 14px;
  font-weight: 600;
}

h1 {
  color: var(--text-color);
  font-size: 28px;
}

.primary-button,
.danger-button {
  min-height: 36px;
  padding: 7px 14px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 14px;
  font-weight: 600;
}

.primary-button {
  border: 1px solid var(--primary-color);
  background: var(--primary-color);
  color: #fff;
}

.primary-button:hover {
  background: var(--primary-hover-color);
  border-color: var(--primary-hover-color);
}

.primary-button:disabled,
.danger-button:disabled {
  cursor: wait;
  opacity: 0.65;
}

.create-form {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr) auto;
  align-items: end;
  gap: 16px;
  margin-bottom: 20px;
  padding: 20px;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--surface-color);
}

.form-field {
  display: grid;
  gap: 6px;
}

label {
  color: var(--text-secondary);
  font-size: 14px;
}

input {
  width: 100%;
  min-height: 38px;
  padding: 7px 10px;
  border: 1px solid var(--border-color);
  border-radius: 4px;
  background: var(--bg-color);
  color: var(--text-color);
  outline: none;
}

input:focus {
  border-color: var(--primary-color);
}

.form-error {
  margin: 12px 0;
  color: #f87171;
  font-size: 14px;
}

.create-form .form-error {
  grid-column: 1 / -1;
  margin: 0;
}

.users-panel {
  overflow: hidden;
  border: 1px solid var(--border-color);
  border-radius: 8px;
  background: var(--surface-color);
}

.table-wrapper {
  overflow-x: auto;
}

table {
  width: 100%;
  min-width: 620px;
  border-collapse: collapse;
  text-align: left;
}

th,
td {
  padding: 14px 18px;
  border-bottom: 1px solid var(--border-color);
  white-space: nowrap;
}

th {
  color: var(--text-secondary);
  font-size: 13px;
  font-weight: 600;
}

td {
  color: var(--text-color);
  font-size: 14px;
}

tbody tr:last-child td {
  border-bottom: 0;
}

.role-label {
  color: var(--primary-hover-color);
}

.action-column {
  text-align: right;
}

.danger-button {
  border: 1px solid #7f1d1d;
  background: transparent;
  color: #f87171;
}

.danger-button:hover {
  border-color: #ef4444;
  background: #450a0a;
}

.muted-label,
.empty-state {
  color: var(--text-secondary);
}

.empty-state {
  padding: 56px 24px;
  text-align: center;
}

@media (max-width: 720px) {
  .page-heading {
    align-items: start;
  }

  .create-form {
    grid-template-columns: 1fr;
  }

  .create-form .primary-button {
    width: 100%;
  }
}

@media (max-width: 480px) {
  .page-heading {
    gap: 12px;
  }

  h1 {
    font-size: 24px;
  }

  .primary-button {
    padding-inline: 10px;
  }
}
</style>
