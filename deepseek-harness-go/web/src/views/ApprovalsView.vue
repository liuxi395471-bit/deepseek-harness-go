<script setup lang="ts">
// ApprovalsView.vue
//
// 待审批列表 + allow / always / deny 三选一。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Check, CheckCheck, X, ShieldCheck, RefreshCw } from 'lucide-vue-next'
import { approvalsApi } from '@/api/approvals'
import { formatRelative } from '@/utils/format'

const qc = useQueryClient()

const list = useQuery({
  queryKey: ['approvals'],
  queryFn: approvalsApi.list,
  refetchInterval: 3000,
})

const decideMut = useMutation({
  mutationFn: ({ id, decision }: { id: string; decision: 'allow' | 'deny' | 'always' }) =>
    approvalsApi.decide(id, decision),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['approvals'] }),
})
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">待审批</h2>
      <button class="btn" @click="list.refetch()">
        <RefreshCw :size="14" />
        刷新
      </button>
    </div>

    <div v-if="list.error.value" class="error-banner">
      {{ (list.error.value as Error).message }}
    </div>

    <div v-if="list.isLoading.value" class="empty">加载中…</div>

    <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
      <ShieldCheck :size="32" style="opacity: 0.3;" />
      <p>暂无待审批请求</p>
    </div>

    <div v-else class="list">
      <div v-for="a in list.data.value ?? []" :key="a.id" class="card approval-card">
        <div class="flex items-center justify-between">
          <div style="flex: 1; min-width: 0;">
            <div class="flex items-center gap-3">
              <span class="font-bold">{{ a.tool }}</span>
              <span class="text-xs text-muted">{{ a.profile }}</span>
              <span class="badge badge-pending">待审</span>
              <span class="text-xs text-muted">{{ formatRelative(a.createdAt) }}</span>
            </div>
            <pre class="approval-args">{{ JSON.stringify(a.args, null, 2) }}</pre>
            <div v-if="a.reason" class="text-sm text-muted mt-2">
              原因：{{ a.reason }}
            </div>
          </div>
          <div class="flex gap-2">
            <button
              class="btn"
              title="仅本次允许"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'allow' })"
            >
              <Check :size="14" />
              允许
            </button>
            <button
              class="btn btn-primary"
              title="此后始终允许"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'always' })"
            >
              <CheckCheck :size="14" />
              始终
            </button>
            <button
              class="btn btn-danger"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'deny' })"
            >
              <X :size="14" />
              拒绝
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.approval-args {
  margin: 8px 0 0 0;
  padding: 8px;
  background: #1a202c;
  color: #edf2f7;
  border-radius: 4px;
  font-size: 11px;
  max-height: 120px;
  overflow: auto;
}
</style>
