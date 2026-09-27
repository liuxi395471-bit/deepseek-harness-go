<script setup lang="ts">
// PluginsView.vue
//
// 插件列表 + 启停 + 安装 / 卸载。

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
  if (v === 'enabled' || v === 'loaded' || v === 'active') return 'badge-loaded'
  if (v === 'disabled' || v === 'inactive') return 'badge-disabled'
  if (v === 'uninstalled') return 'badge-disabled'
  if (v === 'failed' || v === 'error') return 'badge-failed'
  return ''
}

function handleUninstall(n: string) {
  if (!window.confirm(`确认卸载 ${n}？`)) return
  uninstallMut.mutate(n)
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">插件管理</h2>
      <div class="flex gap-2">
        <button class="btn btn-primary" @click="showInstall = true">
          <Plus :size="14" />
          安装
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
            <button class="btn btn-danger" @click="handleUninstall(p.name)">
              <Trash2 :size="14" />
              卸载
            </button>
          </div>
        </div>
      </div>
    </div>

    <Modal :open="showInstall" title="安装插件" @close="showInstall = false" @confirm="installMut.mutate({ name: newName, source: newSource })">
      <label>
        <span>插件名称（必填）</span>
        <input v-model="newName" placeholder="如 echo / fs / my-tool" />
      </label>
      <label>
        <span>来源（可选，仅作标签记录）</span>
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
.plugin-card .btn {
  min-width: 80px;
}
</style>
