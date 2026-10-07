<script setup lang="ts">
// PluginsView.vue — 插件管理（dsh 风格）

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { Power, Package, RefreshCw, Plus, Trash2 } from 'lucide-vue-next'
import { pluginsApi } from '@/api/plugins'
import { useUIStore } from '@/stores/ui'
import Modal from '@/components/Modal.vue'

const qc = useQueryClient()
const ui = useUIStore()

const list = useQuery({
  queryKey: ['plugins'],
  queryFn: pluginsApi.list,
  refetchInterval: 8000,
})

const enableMut = useMutation({
  mutationFn: (n: string) => pluginsApi.enable(n),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['plugins'] }),
  onError: (e: Error) => ui.reportError(e, '启用失败'),
})
const disableMut = useMutation({
  mutationFn: (n: string) => pluginsApi.disable(n),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['plugins'] }),
  onError: (e: Error) => ui.reportError(e, '停用失败'),
})
const installMut = useMutation({
  mutationFn: ({ name, source }: { name: string; source: string }) =>
    pluginsApi.install(name, source),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['plugins'] })
    ui.pushToast('success', '插件已安装')
    showInstall.value = false
    newName.value = ''
    newSource.value = ''
  },
  onError: (e: Error) => ui.reportError(e, '安装失败'),
})
const uninstallMut = useMutation({
  mutationFn: (n: string) => pluginsApi.uninstall(n),
  onSuccess: () => {
    qc.invalidateQueries({ queryKey: ['plugins'] })
    ui.pushToast('success', '插件已卸载')
  },
  onError: (e: Error) => ui.reportError(e, '卸载失败'),
})

const showInstall = ref(false)
const newName = ref('')
const newSource = ref('')

function stateBadge(s: string) {
  const v = (s ?? '').toLowerCase()
  if (v === 'enabled' || v === 'loaded' || v === 'active') return 'badge-success'
  if (v === 'disabled' || v === 'inactive' || v === 'uninstalled') return 'badge-disabled'
  if (v === 'failed' || v === 'error') return 'badge-failed'
  return ''
}

function handleUninstall(n: string) {
  if (!window.confirm(`确认卸载 ${n}？`)) return
  uninstallMut.mutate(n)
}
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">插件管理</h1>
        <span class="page-count">{{ (list.data.value ?? []).length }}</span>
      </div>
      <div class="ph-right">
        <button class="btn btn-icon" @click="list.refetch()" title="刷新">
          <RefreshCw :size="13" />
        </button>
        <button class="btn btn-primary" @click="showInstall = true">
          <Plus :size="13" />
          安装
        </button>
      </div>
    </header>

    <div v-if="list.error.value" class="error-banner m-3">
      {{ (list.error.value as Error).message }}
    </div>

    <div class="content-body">
      <div v-if="list.isLoading.value" class="empty">加载中…</div>
      <div v-else-if="(list.data.value ?? []).length === 0" class="empty">
        <Package :size="28" class="empty-icon" />
        <div class="empty-title">暂无插件</div>
      </div>

      <div v-else class="plugin-list">
        <div v-for="p in list.data.value ?? []" :key="p.name" class="plugin-item">
          <div class="plugin-head">
            <div class="plugin-info">
              <span class="plugin-name">{{ p.name }}</span>
              <div class="plugin-meta">
                <span class="tag tag-mono">{{ p.kind }}</span>
                <span v-if="p.version" class="tag tag-mono">v{{ p.version }}</span>
                <span class="tag tag-mono">{{ p.source }}</span>
                <span :class="['badge', stateBadge(p.state)]">{{ p.state }}</span>
                <span v-if="!p.healthy" class="badge badge-failed">不健康</span>
              </div>
            </div>
            <div class="plugin-actions">
              <button
                v-if="p.state !== 'enabled' && p.state !== 'loaded'"
                class="btn btn-sm"
                :disabled="enableMut.isPending.value"
                @click="enableMut.mutate(p.name)"
              >
                <Power :size="11" />
                启用
              </button>
              <button
                v-else
                class="btn btn-sm"
                :disabled="disableMut.isPending.value"
                @click="disableMut.mutate(p.name)"
              >
                停用
              </button>
              <button class="btn btn-sm btn-icon btn-danger-icon" @click="handleUninstall(p.name)" title="卸载">
                <Trash2 :size="11" />
              </button>
            </div>
          </div>
          <div v-if="p.tools && p.tools.length" class="plugin-tools">
            <span class="tools-label">tools:</span>
            <span v-for="t in p.tools" :key="t" class="tag tag-mono">{{ t }}</span>
          </div>
          <div v-if="p.lastError" class="plugin-error">{{ p.lastError }}</div>
        </div>
      </div>
    </div>

    <Modal :open="showInstall" title="安装插件" @close="showInstall = false" @confirm="installMut.mutate({ name: newName, source: newSource })">
      <label>
        <span>插件名称（必填）</span>
        <input v-model="newName" placeholder="如 echo / fs / my-tool" />
      </label>
      <label>
        <span>来源（可选）</span>
        <input v-model="newSource" placeholder="如 registry:echo-v1 / file:/path" />
      </label>
      <template #footer>
        <button class="btn" @click="showInstall = false">取消</button>
        <button
          class="btn btn-primary"
          :disabled="installMut.isPending.value || !newName.trim()"
          @click="installMut.mutate({ name: newName, source: newSource })"
        >
          {{ installMut.isPending.value ? '安装中…' : '安装' }}
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

.plugin-list { display: flex; flex-direction: column; gap: 4px; max-width: 960px; margin: 0 auto; }
.plugin-item {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-md);
  padding: 8px 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  transition: border-color var(--ds-duration-fast) var(--ds-ease-in-out);
}
.plugin-item:hover { border-color: var(--border-strong); }
.plugin-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}
.plugin-info { flex: 1; min-width: 0; }
.plugin-name { font-size: 12px; font-weight: 600; color: var(--text); margin-right: 8px; }
.plugin-meta { display: flex; align-items: center; gap: 4px; flex-wrap: wrap; margin-top: 2px; }
.plugin-actions { display: flex; align-items: center; gap: 3px; flex-shrink: 0; }
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
.plugin-tools {
  display: flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  color: var(--text-faint);
  flex-wrap: wrap;
}
.tools-label { color: var(--text-faint); }
.plugin-error {
  font-size: 11px;
  color: var(--danger);
  background: var(--danger-bg);
  padding: 4px 8px;
  border-radius: var(--ds-radius-xs);
  border: 1px solid color-mix(in srgb, var(--danger) 20%, transparent);
}
.btn-danger-icon { color: var(--danger); }
.btn-danger-icon:hover:not(:disabled) {
  background: var(--danger-bg);
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 30%, transparent);
}
</style>
