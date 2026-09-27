<script setup lang="ts">
// ModelsView.vue
//
// 渠道列表 + 启用的渠道切换 + ping 延迟测试 + 新增渠道。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { Zap, RefreshCw, Plus, Trash2 } from 'lucide-vue-next'
import { modelsApi } from '@/api/models'
import { auditApi } from '@/api/audit'
import { useUIStore } from '@/stores/ui'
import type { PingResult } from '@/api/types'
import Modal from '@/components/Modal.vue'

const qc = useQueryClient()
const ui = useUIStore()

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
  onError: (e: Error) => ui.reportError(e, '更新渠道失败'),
})
const removeMut = useMutation({
  mutationFn: (channel: string) => modelsApi.remove(channel),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['models'] })
    ui.pushToast('success', '渠道已删除')
  },
  onError: (e: Error) => ui.reportError(e, '删除渠道失败'),
})
const createMut = useMutation({
  mutationFn: (item: any) => modelsApi.create(item),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['models'] })
    ui.pushToast('success', '渠道已创建')
    showNew.value = false
    resetForm()
  },
  onError: (e: Error) => ui.reportError(e, '创建渠道失败'),
})

const showNew = ref(false)
const newChannel = ref('')
const newModel = ref('deepseek-chat')
const newProtocol = ref('openai-compatible')
const newBaseUrl = ref('')

function resetForm() {
  newChannel.value = ''
  newModel.value = 'deepseek-chat'
  newProtocol.value = 'openai-compatible'
  newBaseUrl.value = ''
}

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

async function handleExportAudit() {
  try {
    await auditApi.downloadJsonl(1000)
    ui.pushToast('success', '审计 JSONL 已下载')
  } catch (e) {
    ui.reportError(e as Error, '导出审计失败')
  }
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">模型渠道</h2>
      <div class="flex gap-2">
        <button class="btn" @click="handleExportAudit" title="导出审计">
          <RefreshCw :size="14" />
          导出审计
        </button>
        <button class="btn btn-primary" @click="showNew = true">
          <Plus :size="14" />
          新增渠道
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

    <table v-else-if="(list.data.value ?? []).length" class="model-table">
      <thead>
        <tr>
          <th>渠道</th>
          <th>模型</th>
          <th>协议</th>
          <th>状态</th>
          <th>连通性</th>
          <th style="width: 260px;">操作</th>
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
              <button class="btn btn-danger" :disabled="removeMut.isPending.value" @click="removeMut.mutate(m.channel)">
                <Trash2 :size="12" />
              </button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>

    <div v-else class="empty">暂无模型渠道</div>

    <Modal :open="showNew" title="新增模型渠道" @close="showNew = false" @confirm="createMut.mutate({ channel: newChannel, model: newModel, protocol: newProtocol, baseUrl: newBaseUrl, active: true })">
      <label>
        <span>渠道 ID（必填，唯一）</span>
        <input v-model="newChannel" placeholder="如 my-deepseek" />
      </label>
      <label>
        <span>模型名</span>
        <input v-model="newModel" placeholder="deepseek-chat / gpt-4o / …" />
      </label>
      <label>
        <span>协议</span>
        <select v-model="newProtocol">
          <option value="openai-compatible">openai-compatible</option>
          <option value="anthropic">anthropic</option>
          <option value="gemini">gemini</option>
          <option value="deepseek">deepseek</option>
        </select>
      </label>
      <label>
        <span>Base URL</span>
        <input v-model="newBaseUrl" placeholder="https://api.deepseek.com/v1" />
      </label>
      <template #footer>
        <button class="btn" @click="showNew = false">取消</button>
        <button
          class="btn btn-primary"
          :disabled="createMut.isPending.value || !newChannel.trim() || !newModel.trim()"
          @click="createMut.mutate({ channel: newChannel, model: newModel, protocol: newProtocol, baseUrl: newBaseUrl, active: true })"
        >
          {{ createMut.isPending.value ? '创建中…' : '创建' }}
        </button>
      </template>
    </Modal>
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
  background: var(--bg);
  font-weight: 600;
  font-size: 12px;
}
.model-table tbody tr:last-child td {
  border-bottom: none;
}
</style>
