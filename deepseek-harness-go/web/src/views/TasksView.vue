<script setup lang="ts">
// TasksView.vue

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { X, RefreshCw, ListTodo } from 'lucide-vue-next'
import { tasksApi } from '@/api/tasks'
import { formatRelative } from '@/utils/format'

const qc = useQueryClient()
const filter = ref<string>('')

const list = useQuery({
  queryKey: ['tasks', filter],
  queryFn: () => tasksApi.list(filter.value),
  refetchInterval: 5000,
})

const cancelMut = useMutation({
  mutationFn: (id: string) => tasksApi.cancel(id),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['tasks'] }),
})

function stateBadge(s: string) {
  const v = (s ?? '').toLowerCase()
  if (v === 'running' || v === 'active') return 'badge-running'
  if (v === 'pending' || v === 'queued' || v === 'waiting') return 'badge-pending'
  if (v === 'done' || v === 'completed' || v === 'succeeded') return 'badge-completed'
  if (v === 'failed' || v === 'error' || v === 'cancelled') return 'badge-failed'
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
</template>
