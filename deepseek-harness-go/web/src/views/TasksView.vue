<script setup lang="ts">
// TasksView.vue — 任务调度（dsh 风格）

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
  if (v === 'running' || v === 'active') return 'badge-info'
  if (v === 'pending' || v === 'queued' || v === 'waiting') return 'badge-warning'
  if (v === 'done' || v === 'completed' || v === 'succeeded') return 'badge-success'
  if (v === 'failed' || v === 'error' || v === 'cancelled' || v === 'canceled') return 'badge-failed'
  return ''
}
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">任务调度</h1>
        <span class="page-count">{{ (list.data.value ?? []).length }}</span>
      </div>
      <div class="ph-right">
        <select v-model="filter" class="filter-select">
          <option value="">全部状态</option>
          <option value="pending">pending</option>
          <option value="running">running</option>
          <option value="completed">completed</option>
          <option value="failed">failed</option>
          <option value="cancelled">cancelled</option>
        </select>
        <button class="btn btn-icon" @click="list.refetch()" title="刷新">
          <RefreshCw :size="13" />
        </button>
        <button class="btn btn-primary" @click="showNew = true">
          <Plus :size="13" />
          新建任务
        </button>
      </div>
    </header>

    <div v-if="list.error.value" class="error-banner m-3">
      {{ (list.error.value as Error).message }}
    </div>

    <div class="content-body">
      <div v-if="list.isLoading.value" class="empty">加载中…</div>
      <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
        <ListTodo :size="28" class="empty-icon" />
        <div class="empty-title">暂无任务</div>
      </div>

      <div v-else class="task-list">
        <div v-for="t in list.data.value ?? []" :key="t.id" class="task-item">
          <div class="task-head">
            <div class="task-info">
              <span class="task-name">{{ t.title || t.id }}</span>
              <div class="task-meta">
                <span class="tag tag-mono">{{ t.profile }}</span>
                <span :class="['badge', stateBadge(t.state)]">{{ t.state }}</span>
                <span v-if="t.code" class="tag tag-mono">#{{ t.code }}</span>
                <span class="text-xs text-faint">
                  创建 {{ formatRelative(t.createdAt) }} · 更新 {{ formatRelative(t.updatedAt) }}
                </span>
              </div>
            </div>
            <div class="task-actions">
              <button
                v-if="t.state === 'failed' || t.state === 'cancelled' || t.state === 'canceled'"
                class="btn btn-sm"
                :disabled="retryMut.isPending.value"
                @click="retryMut.mutate(t.id)"
              >
                <RotateCcw :size="11" />
                重试
              </button>
              <button
                v-if="t.state === 'running' || t.state === 'pending'"
                class="btn btn-sm btn-danger"
                :disabled="cancelMut.isPending.value"
                @click="cancelMut.mutate(t.id)"
              >
                <X :size="11" />
                取消
              </button>
            </div>
          </div>
          <div v-if="t.progress" class="text-xs text-faint">
            jobId={{ t.progress.jobId }} · {{ t.progress.lines }} 行 · {{ t.progress.status }}
          </div>
          <div v-if="t.error" class="task-error">{{ t.error }}</div>
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

<style scoped>
.page { display: flex; flex-direction: column; height: 100%; background: var(--bg); }
.page-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 10px 16px;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
  background: var(--bg-toolbar);
  flex-shrink: 0;
  gap: 12px;
}
.ph-left { display: flex; align-items: baseline; gap: 8px; }
.page-title { font-size: 13px; font-weight: 600; margin: 0; color: var(--text); }
.page-count {
  font-size: 10px;
  color: var(--text-faint);
  background: var(--bg-elevated);
  padding: 1px 6px;
  border-radius: 8px;
  font-weight: 600;
}
.ph-right { display: flex; align-items: center; gap: 4px; }
.filter-select { width: 130px; }
.content-body { flex: 1; overflow-y: auto; padding: 12px 16px; }
.m-3 { margin: 12px; }

.task-list { display: flex; flex-direction: column; gap: 4px; max-width: 960px; margin: 0 auto; }
.task-item {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-md);
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.task-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.task-info { flex: 1; min-width: 0; }
.task-name { font-size: 12px; font-weight: 600; color: var(--text); margin-right: 8px; }
.task-meta { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; margin-top: 2px; }
.task-actions { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
.tag {
  display: inline-block;
  padding: 1px 5px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  border-radius: 3px;
  font-size: 10px;
  color: var(--text-muted);
}
.tag-mono { font-family: var(--ds-font-family-code); }
.task-error {
  font-size: 11px;
  color: var(--danger);
  background: var(--danger-bg);
  padding: 4px 8px;
  border-radius: var(--ds-radius-xs);
  border: 1px solid color-mix(in srgb, var(--danger) 20%, transparent);
}
</style>
