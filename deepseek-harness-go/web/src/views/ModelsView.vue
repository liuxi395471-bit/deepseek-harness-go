<script setup lang="ts">
// ModelsView.vue
//
// 渠道列表 + 启用的渠道切换 + ping 延迟测试。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { Zap, RefreshCw } from 'lucide-vue-next'
import { modelsApi } from '@/api/models'
import type { PingResult } from '@/api/types'

const qc = useQueryClient()

const list = useQuery({
  queryKey: ['models'],
  queryFn: modelsApi.list,
  refetchInterval: 10000,
})

const pingResults = ref<Record<string, PingResult>>({})
const pinging = ref<Record<string, boolean>>({})

const updateMut = useMutation({
  mutationFn: ({ channel, item }: { channel: string; item: any }) =>
    modelsApi.update(channel, item),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['models'] }),
})

async function ping(channel: string) {
  pinging.value = { ...pinging.value, [channel]: true }
  try {
    const r = await modelsApi.ping(channel)
    pingResults.value = { ...pingResults.value, [channel]: r }
  } catch (e) {
    pingResults.value = {
      ...pingResults.value,
      [channel]: { ok: false, latencyMs: 0, error: (e as Error).message },
    }
  } finally {
    pinging.value = { ...pinging.value, [channel]: false }
  }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">模型渠道</h2>
      <button class="btn" @click="list.refetch()">
        <RefreshCw :size="14" />
        刷新
      </button>
    </div>

    <div v-if="list.error.value" class="error-banner">
      {{ (list.error.value as Error).message }}
    </div>

    <div v-if="list.isLoading.value" class="empty">加载中…</div>

    <table v-else-if="(list.data.value ?? []).length" class="model-table">
      <thead>
        <tr>
          <th>渠道</th>
          <th>模型</th>
          <th>协议</th>
          <th>状态</th>
          <th>连通性</th>
          <th style="width: 200px;">操作</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="m in list.data.value ?? []" :key="m.channel">
          <td class="font-bold">{{ m.channel }}</td>
          <td>{{ m.model }}</td>
          <td>{{ m.protocol }}</td>
          <td>
            <span v-if="m.active" class="badge badge-loaded">启用</span>
            <span v-else class="badge badge-disabled">未启用</span>
          </td>
          <td>
            <span v-if="pinging[m.channel]" class="text-sm text-muted">测试中…</span>
            <span v-else-if="pingResults[m.channel]">
              <span v-if="pingResults[m.channel].ok" class="badge badge-loaded">
                {{ pingResults[m.channel].latencyMs }} ms
              </span>
              <span v-else class="badge badge-failed" :title="pingResults[m.channel].error">
                失败
              </span>
            </span>
            <span v-else class="text-xs text-muted">—</span>
          </td>
          <td>
            <div class="flex gap-2">
              <button class="btn" :disabled="pinging[m.channel]" @click="ping(m.channel)">
                <Zap :size="12" />
                ping
              </button>
              <button
                class="btn"
                :disabled="updateMut.isPending.value"
                @click="updateMut.mutate({ channel: m.channel, item: { active: !m.active } })"
              >
                {{ m.active ? '停用' : '启用' }}
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-else class="empty">暂无模型渠道</div>
  </div>
</template>

<style scoped>
.model-table {
  width: 100%;
  border-collapse: collapse;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 8px;
  overflow: hidden;
}
.model-table th,
.model-table td {
  padding: 10px 12px;
  text-align: left;
  border-bottom: 1px solid var(--border);
  font-size: 13px;
}
.model-table th {
  background: #edf2f7;
  font-weight: 600;
  font-size: 12px;
}
.model-table tbody tr:last-child td {
  border-bottom: none;
}
</style>
