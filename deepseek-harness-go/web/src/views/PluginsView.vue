<script setup lang="ts">
// PluginsView.vue

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { Power, Package, RefreshCw } from 'lucide-vue-next'
import { pluginsApi } from '@/api/plugins'

const qc = useQueryClient()

const list = useQuery({
  queryKey: ['plugins'],
  queryFn: pluginsApi.list,
  refetchInterval: 8000,
})

const enableMut = useMutation({
  mutationFn: (n: string) => pluginsApi.enable(n),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['plugins'] }),
})
const disableMut = useMutation({
  mutationFn: (n: string) => pluginsApi.disable(n),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['plugins'] }),
})

function stateBadge(s: string) {
  const v = (s ?? '').toLowerCase()
  if (v === 'enabled' || v === 'loaded' || v === 'active') return 'badge-loaded'
  if (v === 'disabled' || v === 'inactive') return 'badge-disabled'
  if (v === 'failed' || v === 'error') return 'badge-failed'
  return ''
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">插件管理</h2>
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
      <Package :size="32" style="opacity: 0.3;" />
      <p>暂无插件</p>
    </div>

    <div v-else class="list">
      <div v-for="p in list.data.value ?? []" :key="p.name" class="card plugin-card">
        <div class="flex items-center justify-between">
          <div style="flex: 1; min-width: 0;">
            <div class="flex items-center gap-3">
              <span class="font-bold">{{ p.name }}</span>
              <span class="text-xs text-muted">{{ p.kind }} · {{ p.source }}</span>
              <span v-if="p.version" class="text-xs text-muted">v{{ p.version }}</span>
              <span :class="['badge', stateBadge(p.state)]">{{ p.state }}</span>
              <span v-if="!p.healthy" class="badge badge-failed">不健康</span>
            </div>
            <div v-if="p.tools && p.tools.length" class="text-sm text-muted mt-2">
              工具：{{ p.tools.join(', ') }}
            </div>
            <div v-if="p.lastError" class="text-sm" style="color: var(--danger); margin-top: 4px;">
              {{ p.lastError }}
            </div>
          </div>
          <div class="flex gap-2">
            <button
              v-if="p.state !== 'enabled' && p.state !== 'loaded'"
              class="btn btn-primary"
              :disabled="enableMut.isPending.value"
              @click="enableMut.mutate(p.name)"
            >
              <Power :size="14" />
              启用
            </button>
            <button
              v-else
              class="btn"
              :disabled="disableMut.isPending.value"
              @click="disableMut.mutate(p.name)"
            >
              停用
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.plugin-card .btn {
  min-width: 80px;
}
</style>
