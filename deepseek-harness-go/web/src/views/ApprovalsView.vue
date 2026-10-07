<script setup lang="ts">
// ApprovalsView.vue — 待审批（dsh 风格）

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
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">待审批</h1>
        <span class="page-count">{{ (list.data.value ?? []).length }}</span>
      </div>
      <div class="ph-right">
        <span v-if="selected.size" class="text-xs text-faint">已选 {{ selected.size }}</span>
        <button
          v-if="selected.size"
          class="btn btn-sm"
          :disabled="batchMut.isPending.value"
          @click="batch('allow')"
        >
          <Check :size="11" />
          批量允许
        </button>
        <button
          v-if="selected.size"
          class="btn btn-sm btn-danger"
          :disabled="batchMut.isPending.value"
          @click="batch('deny')"
        >
          <X :size="11" />
          批量拒绝
        </button>
        <button
          v-if="(list.data.value ?? []).length > 1"
          class="btn btn-sm"
          @click="toggleAll"
        >
          <component :is="allSelected ? CheckSquare : Square" :size="11" />
          {{ allSelected ? '取消全选' : '全选' }}
        </button>
        <button class="btn btn-icon" @click="list.refetch()" title="刷新">
          <RefreshCw :size="13" />
        </button>
      </div>
    </header>

    <div v-if="list.error.value" class="error-banner m-3">
      {{ (list.error.value as Error).message }}
    </div>

    <div class="content-body">
      <div v-if="list.isLoading.value" class="empty">加载中…</div>
      <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
        <ShieldCheck :size="28" class="empty-icon" />
        <div class="empty-title">暂无待审批请求</div>
      </div>

      <div v-else class="approval-list">
        <div v-for="a in list.data.value ?? []" :key="a.id" class="approval-item">
          <input
            type="checkbox"
            :checked="selected.has(a.id)"
            class="approval-check"
            @change="toggleOne(a.id)"
          />
          <div class="approval-body">
            <div class="approval-head">
              <span class="approval-tool">{{ a.tool }}</span>
              <span class="tag tag-mono">{{ a.profile }}</span>
              <span class="badge badge-warning">待审</span>
              <span class="text-xs text-faint">{{ formatRelative(a.createdAt) }}</span>
            </div>
            <pre class="approval-args">{{ JSON.stringify(a.args, null, 2) }}</pre>
            <div v-if="a.reason" class="text-sm text-muted mt-2">
              原因：{{ a.reason }}
            </div>
          </div>
          <div class="approval-actions">
            <button
              class="btn btn-sm"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'allow' })"
              title="仅本次允许"
            >
              <Check :size="11" />
              允许
            </button>
            <button
              class="btn btn-sm btn-primary"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'always' })"
              title="此后始终允许"
            >
              <CheckCheck :size="11" />
              始终
            </button>
            <button
              class="btn btn-sm btn-danger"
              :disabled="decideMut.isPending.value"
              @click="decideMut.mutate({ id: a.id, decision: 'deny' })"
            >
              <X :size="11" />
              拒绝
            </button>
          </div>
        </div>
      </div>
    </div>
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
.content-body { flex: 1; overflow-y: auto; padding: 12px 16px; }
.m-3 { margin: 12px; }
.mt-2 { margin-top: 8px; }

.approval-list { display: flex; flex-direction: column; gap: 6px; max-width: 1000px; margin: 0 auto; }
.approval-item {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-left: 3px solid var(--warning);
  border-radius: var(--ds-radius-md);
  padding: 8px 12px;
}
.approval-check {
  margin-top: 3px;
  width: 14px;
  height: 14px;
  cursor: pointer;
  accent-color: var(--accent);
}
.approval-body { flex: 1; min-width: 0; }
.approval-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
  margin-bottom: 4px;
}
.approval-tool { font-size: 12px; font-weight: 600; color: var(--text); }
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
.approval-args {
  margin: 6px 0 0 0;
  padding: 6px 8px;
  background: var(--bg-code);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-xs);
  font-size: 11px;
  font-family: var(--ds-font-family-code);
  max-height: 100px;
  overflow: auto;
}
.approval-actions { display: flex; flex-direction: column; gap: 3px; flex-shrink: 0; }
</style>
