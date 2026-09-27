<script setup lang="ts">
// ApprovalsView.vue
//
// 待审批列表 + 单条 allow / always / deny + 批量 allow / deny。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref, computed } from 'vue'
import { Check, CheckCheck, X, ShieldCheck, RefreshCw, Square, CheckSquare } from 'lucide-vue-next'
import { approvalsApi } from '@/api/approvals'
import { formatRelative } from '@/utils/format'
import { useUIStore } from '@/stores/ui'

const qc = useQueryClient()
const ui = useUIStore()

const list = useQuery({
  queryKey: ['approvals'],
  queryFn: approvalsApi.list,
  refetchInterval: 3000,
})

const decideMut = useMutation({
  mutationFn: ({ id, decision }: { id: string; decision: 'allow' | 'deny' | 'always' }) =>
    approvalsApi.decide(id, decision),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['approvals'] }),
  onError: (e: Error) => ui.reportError(e, '审批失败'),
})

const batchMut = useMutation({
  mutationFn: ({ ids, decision }: { ids: string[]; decision: 'allow' | 'deny' | 'always' }) =>
    approvalsApi.decideBatch(ids, decision),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['approvals'] }),
  onError: (e: Error) => ui.reportError(e, '批量审批失败'),
})

const selected = ref<Set<string>>(new Set())

const allSelected = computed(() => {
  const items = list.data.value ?? []
  return items.length > 0 && items.every((a) => selected.value.has(a.id))
})

function toggleAll() {
  const items = list.data.value ?? []
  if (allSelected.value) {
    selected.value = new Set()
  } else {
    selected.value = new Set(items.map((a) => a.id))
  }
}

function toggleOne(id: string) {
  const next = new Set(selected.value)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  selected.value = next
}

function batch(decision: 'allow' | 'deny') {
  if (selected.value.size === 0) return
  batchMut.mutate({ ids: Array.from(selected.value), decision })
  selected.value = new Set()
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">待审批</h2>
      <div class="flex gap-2 items-center">
        <span v-if="selected.size" class="text-xs text-muted">已选 {{ selected.size }}</span>
        <button v-if="selected.size" class="btn" :disabled="batchMut.isPending.value" @click="batch('allow')">
          <Check :size="14" />
          批量允许
        </button>
        <button v-if="selected.size" class="btn btn-danger" :disabled="batchMut.isPending.value" @click="batch('deny')">
          <X :size="14" />
          批量拒绝
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
      <ShieldCheck :size="32" style="opacity: 0.3;" />
      <p>暂无待审批请求</p>
    </div>

    <div v-else class="list">
      <div v-for="a in list.data.value ?? []" :key="a.id" class="card approval-card">
        <div class="flex items-start gap-3">
          <input
            type="checkbox"
            :checked="selected.has(a.id)"
            style="margin-top: 6px;"
            @change="toggleOne(a.id)"
          />
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
          <div class="flex gap-2 flex-shrink-0">
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

    <div v-if="(list.data.value ?? []).length > 1" class="flex items-center gap-2 mt-4">
      <button class="btn" @click="toggleAll">
        <component :is="allSelected ? CheckSquare : Square" :size="14" />
        {{ allSelected ? '取消全选' : '全选' }}
      </button>
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
