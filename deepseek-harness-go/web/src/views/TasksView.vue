<script setup lang="ts">
// TasksView.vue
//
// 任务列表 + 创建任务弹窗 + 重试失败任务。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { Plus, X, RefreshCw, ListTodo, RotateCcw } from 'lucide-vue-next'
import { tasksApi } from '@/api/tasks'
import { formatRelative } from '@/utils/format'
import { useUIStore } from '@/stores/ui'
import Modal from '@/components/Modal.vue'

const qc = useQueryClient()
const ui = useUIStore()
const filter = ref<string>('')

const list = useQuery({
  queryKey: ['tasks', filter],
  queryFn: () => tasksApi.list(filter.value),
  refetchInterval: 5000,
})

const cancelMut = useMutation({
  mutationFn: (id: string) => tasksApi.cancel(id),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
  onError: (e: Error) => ui.reportError(e, '取消任务失败'),
})

const retryMut = useMutation({
  mutationFn: (id: string) => tasksApi.retry(id),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['tasks'] })
    ui.pushToast('success', '已重新提交任务')
  },
  onError: (e: Error) => ui.reportError(e, '重试失败'),
})

const createMut = useMutation({
  mutationFn: (params: { title: string; input: string; profile: string }) =>
    tasksApi.create(params),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['tasks'] })
    ui.pushToast('success', '任务已创建')
    showNew.value = false
    newTitle.value = ''
    newInput.value = ''
  },
  onError: (e: Error) => ui.reportError(e, '创建失败'),
})

const showNew = ref(false)
const newTitle = ref('')
const newInput = ref('')
const newProfile = ref('headless')

function stateBadge(s: string) {
  const v = (s ?? '').toLowerCase()
  if (v === 'running' || v === 'active') return 'badge-running'
  if (v === 'pending' || v === 'queued' || v === 'waiting') return 'badge-pending'
  if (v === 'done' || v === 'completed' || v === 'succeeded') return 'badge-completed'
  if (v === 'failed' || v === 'error' || v === 'cancelled' || v === 'canceled') return 'badge-failed'
  return ''
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">任务调度</h2>
      <div class="flex gap-3 items-center">
        <select v-model="filter" style="width: auto; min-width: 140px;">
          <option value="">全部状态</option>
          <option value="pending">pending</option>
          <option value="running">running</option>
          <option value="completed">completed</option>
          <option value="failed">failed</option>
          <option value="cancelled">cancelled</option>
        </select>
        <button class="btn btn-primary" @click="showNew = true">
          <Plus :size="14" />
          新建任务
        </button>
        <button class="btn" @click="list.refetch()">
          <RefreshCw :size="14" />
          刷新
        </button>
      </div>
    </div>

    <div v-if="list.error.value" class="error-banner">
      {{ (list.error.value as Error).message }}
    </div>

    <div v-if="list.isLoading.value" class="empty">加载中…</div>

    <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
      <ListTodo :size="32" style="opacity: 0.3;" />
      <p>暂无任务</p>
    </div>

    <div v-else class="list">
      <div v-for="t in list.data.value ?? []" :key="t.id" class="card">
        <div class="flex items-center justify-between">
          <div style="flex: 1; min-width: 0;">
            <div class="flex items-center gap-3">
              <span class="font-bold">{{ t.title || t.id }}</span>
              <span class="text-xs text-muted">{{ t.profile }}</span>
              <span :class="['badge', stateBadge(t.state)]">{{ t.state }}</span>
              <span v-if="t.code" class="text-xs text-muted">#{{ t.code }}</span>
            </div>
            <div class="text-sm text-muted mt-2">
              创建于 {{ formatRelative(t.createdAt) }} · 更新于 {{ formatRelative(t.updatedAt) }}
            </div>
            <div v-if="t.error" class="text-sm" style="color: var(--danger); margin-top: 4px;">
              {{ t.error }}
            </div>
            <div v-if="t.progress" class="text-xs text-muted mt-2">
              进度：jobId={{ t.progress.jobId }} · {{ t.progress.lines }} 行 · {{ t.progress.status }}
            </div>
          </div>
          <div class="flex gap-2">
            <button
              v-if="t.state === 'failed' || t.state === 'cancelled' || t.state === 'canceled'"
              class="btn btn-primary"
              :disabled="retryMut.isPending.value"
              @click="retryMut.mutate(t.id)"
            >
              <RotateCcw :size="14" />
              重试
            </button>
            <button
              v-if="t.state === 'running' || t.state === 'pending'"
              class="btn btn-danger"
              :disabled="cancelMut.isPending.value"
              @click="cancelMut.mutate(t.id)"
            >
              <X :size="14" />
              取消
            </button>
          </div>
        </div>
      </div>
    </div>

    <Modal :open="showNew" title="新建任务" @close="showNew = false" @confirm="createMut.mutate({ title: newTitle, input: newInput, profile: newProfile })">
      <label>
        <span>任务标题</span>
        <input v-model="newTitle" placeholder="可选；留空用输入前 40 字" />
      </label>
      <label>
        <span>任务输入</span>
        <textarea v-model="newInput" rows="4" placeholder="输入 prompt（必填）" />
      </label>
      <label>
        <span>Profile</span>
        <select v-model="newProfile">
          <option value="headless">headless</option>
          <option value="default">default</option>
          <option value="restricted">restricted</option>
        </select>
      </label>
      <template #footer>
        <button class="btn" @click="showNew = false">取消</button>
        <button
          class="btn btn-primary"
          :disabled="createMut.isPending.value || !newInput.trim()"
          @click="createMut.mutate({ title: newTitle, input: newInput, profile: newProfile })"
        >
          {{ createMut.isPending.value ? '创建中…' : '创建' }}
        </button>
      </template>
    </Modal>
  </div>
</template>
