<script setup lang="ts">
// ModelsView.vue — 模型渠道（dsh 桌面端风格）

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { Zap, RefreshCw, Plus, Trash2, Download, X, Cpu } from 'lucide-vue-next'
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
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">模型渠道</h1>
        <span class="page-count">{{ (list.data.value ?? []).length }}</span>
      </div>
      <div class="ph-right">
        <button class="btn btn-icon" @click="handleExportAudit" title="导出审计">
          <Download :size="13" />
        </button>
        <button class="btn btn-icon" @click="list.refetch()" title="刷新">
          <RefreshCw :size="13" />
        </button>
        <button class="btn btn-primary" @click="showNew = true">
          <Plus :size="13" />
          新增渠道
        </button>
      </div>
    </header>

    <div v-if="list.error.value" class="error-banner m-3">
      {{ (list.error.value as Error).message }}
    </div>

    <div class="content-body">
      <div v-if="list.isLoading.value" class="empty">加载中…</div>
      <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
        <Cpu :size="28" class="empty-icon" />
        <div class="empty-title">暂无模型渠道</div>
        <div class="empty-sub">点击右上角「新增渠道」开始</div>
      </div>

      <table v-else class="data-table">
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
            <td class="td-name">{{ m.channel }}</td>
            <td>{{ m.model }}</td>
            <td><span class="tag tag-mono">{{ m.protocol }}</span></td>
            <td>
              <span :class="['badge', m.active ? 'badge-success' : 'badge-disabled']">
                {{ m.active ? '启用' : '停用' }}
              </span>
            </td>
            <td>
              <span v-if="pinging[m.channel]" class="text-xs text-faint">测试中…</span>
              <span v-else-if="pingResults[m.channel]">
                <span v-if="pingResults[m.channel].ok" class="badge badge-success">
                  {{ pingResults[m.channel].latencyMs }}ms
                </span>
                <span v-else class="badge badge-failed" :title="pingResults[m.channel].error">
                  失败
                </span>
              </span>
              <span v-else class="text-xs text-faint">—</span>
            </td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm" :disabled="pinging[m.channel]" @click="ping(m.channel)">
                  <Zap :size="11" />
                  ping
                </button>
                <button
                  class="btn btn-sm"
                  :disabled="updateMut.isPending.value"
                  @click="updateMut.mutate({ channel: m.channel, item: { active: !m.active } })"
                >
                  {{ m.active ? '停用' : '启用' }}
                </button>
                <button
                  class="btn btn-sm btn-icon btn-danger-icon"
                  :disabled="removeMut.isPending.value"
                  @click="removeMut.mutate(m.channel)"
                  title="删除"
                >
                  <Trash2 :size="11" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <Modal :open="showNew" title="新增模型渠道" @close="showNew = false" @confirm="createMut.mutate({ channel: newChannel, model: newModel, protocol: newProtocol, baseUrl: newBaseUrl, active: true })">
      <label>
        <span>渠道 ID（必填）</span>
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

.data-table {
  width: 100%;
  border-collapse: separate;
  border-spacing: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-md);
  overflow: hidden;
  font-size: 12px;
}
.data-table th,
.data-table td {
  padding: 7px 10px;
  text-align: left;
  border-bottom: 1px solid var(--dsw-alias-border-l1, transparent);
}
.data-table th {
  background: var(--bg-elevated);
  font-weight: 600;
  font-size: 10px;
  color: var(--text-faint);
  text-transform: uppercase;
  letter-spacing: 0.04em;
}
.data-table tbody tr:last-child td { border-bottom: none; }
.data-table tbody tr:hover { background: var(--bg-elevated); }
.td-name { font-weight: 600; color: var(--text); font-family: var(--ds-font-family-code); font-size: 11px; }
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
.row-actions { display: flex; align-items: center; gap: 3px; }
.btn-danger-icon { color: var(--danger); }
.btn-danger-icon:hover:not(:disabled) {
  background: var(--danger-bg);
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 30%, transparent);
}
</style>
