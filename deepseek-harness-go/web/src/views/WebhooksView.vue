<script setup lang="ts">
// WebhooksView — Webhook 通知管理（dsh 风格）

import { onMounted, ref } from 'vue'
import { Plus, Trash2, Send, History, RefreshCw, Bell } from 'lucide-vue-next'
import { useUIStore } from '@/stores/ui'
import { useI18n } from '@/i18n'
import Modal from '@/components/Modal.vue'
import {
  listWebhooks,
  createWebhook,
  deleteWebhook,
  testWebhook,
  listDeliveries,
  type WebhookItem,
  type WebhookDelivery,
} from '@/api/webhooks'

const ui = useUIStore()
const { t } = useI18n()

const items = ref<WebhookItem[]>([])
const loading = ref(false)
const showCreate = ref(false)
const draft = ref({ name: '', url: '', secret: '', enabled: true })

const showDeliveries = ref(false)
const currentDeliveries = ref<WebhookDelivery[]>([])
const currentWebhook = ref<WebhookItem | null>(null)

async function refresh() {
  loading.value = true
  try {
    items.value = await listWebhooks()
  } catch (e) {
    ui.reportError(e)
  } finally {
    loading.value = false
  }
}

async function onCreate() {
  if (!draft.value.name || !draft.value.url) return
  try {
    const item = await createWebhook({
      name: draft.value.name,
      url: draft.value.url,
      secret: draft.value.secret,
      enabled: draft.value.enabled,
    })
    items.value.push(item)
    showCreate.value = false
    draft.value = { name: '', url: '', secret: '', enabled: true }
    ui.pushToast('success', t('common.confirm'), item.name)
  } catch (e) {
    ui.reportError(e)
  }
}

async function onTest(item: WebhookItem) {
  try {
    const del = await testWebhook(item.id)
    ui.pushToast(
      del.ok ? 'success' : 'error',
      del.ok ? '✓' : '✗',
      `HTTP ${del.statusCode} (${del.attempt} attempt)`,
    )
    if (currentWebhook.value?.id === item.id) {
      currentDeliveries.value.unshift(del)
    }
  } catch (e) {
    ui.reportError(e)
  }
}

async function onDelete(item: WebhookItem) {
  if (!confirm(`Delete "${item.name}"?`)) return
  try {
    await deleteWebhook(item.id)
    items.value = items.value.filter((i) => i.id !== item.id)
  } catch (e) {
    ui.reportError(e)
  }
}

async function onShowDeliveries(item: WebhookItem) {
  currentWebhook.value = item
  showDeliveries.value = true
  try {
    currentDeliveries.value = await listDeliveries(item.id, 50)
  } catch (e) {
    ui.reportError(e)
  }
}

onMounted(refresh)
</script>

<template>
  <div class="page">
    <header class="page-header">
      <div class="ph-left">
        <h1 class="page-title">Webhook 通知</h1>
        <span class="page-count">{{ items.length }}</span>
      </div>
      <div class="ph-right">
        <button class="btn btn-icon" @click="refresh" title="刷新">
          <RefreshCw :size="13" />
        </button>
        <button class="btn btn-primary" @click="showCreate = true">
          <Plus :size="13" />
          新建 Webhook
        </button>
      </div>
    </header>

    <div class="content-body">
      <div v-if="loading" class="empty">loading…</div>
      <div v-else-if="items.length === 0" class="empty">
        <Bell :size="28" class="empty-icon" />
        <div class="empty-title">暂无 Webhook</div>
      </div>

      <table v-else class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>URL</th>
            <th>状态</th>
            <th>最近</th>
            <th style="width: 140px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="td-name">{{ item.name }}</td>
            <td><code class="url-code">{{ item.url }}</code></td>
            <td>
              <span :class="['badge', item.enabled ? 'badge-success' : 'badge-disabled']">
                {{ item.enabled ? 'enabled' : 'disabled' }}
              </span>
            </td>
            <td>
              <span v-if="item.lastStatus != null" :class="['badge', String(item.lastStatus) === 'ok' || item.lastStatus === 200 ? 'badge-success' : 'badge-failed']">
                {{ item.lastStatus }}
              </span>
              <span v-else class="text-xs text-faint">—</span>
            </td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm btn-icon" @click="onShowDeliveries(item)" title="投递历史">
                  <History :size="11" />
                </button>
                <button class="btn btn-sm btn-icon" @click="onTest(item)" title="测试">
                  <Send :size="11" />
                </button>
                <button class="btn btn-sm btn-icon btn-danger-icon" @click="onDelete(item)" title="删除">
                  <Trash2 :size="11" />
                </button>
              </div>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <Modal :open="showCreate" :title="t('webhooks.dialogTitle')" @close="showCreate = false">
      <label>
        <span>名称</span>
        <input v-model="draft.name" />
      </label>
      <label>
        <span>目标 URL</span>
        <input v-model="draft.url" placeholder="https://..." />
      </label>
      <label>
        <span>签名密钥（HMAC-SHA256）</span>
        <input v-model="draft.secret" placeholder="（留空则不签名）" />
      </label>
      <template #footer>
        <button class="btn" @click="showCreate = false">取消</button>
        <button class="btn btn-primary" @click="onCreate">创建</button>
      </template>
    </Modal>

    <Modal
      :open="showDeliveries"
      :title="`${t('webhooks.deliveries')} — ${currentWebhook?.name ?? ''}`"
      @close="showDeliveries = false"
    >
      <div v-if="currentDeliveries.length === 0" class="empty">无投递记录</div>
      <table v-else class="data-table">
        <thead>
          <tr>
            <th>时间</th>
            <th>状态</th>
            <th>attempt</th>
            <th>error</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="d in currentDeliveries" :key="d.id">
            <td class="td-mono">{{ new Date(d.deliveredAt).toLocaleString() }}</td>
            <td>
              <span :class="['badge', d.ok ? 'badge-success' : 'badge-failed']">
                {{ d.statusCode }}
              </span>
            </td>
            <td>{{ d.attempt }}</td>
            <td>{{ d.error || '' }}</td>
          </tr>
        </tbody>
      </table>
      <template #footer>
        <button class="btn" @click="showDeliveries = false">关闭</button>
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
.td-name { font-weight: 600; color: var(--text); }
.td-mono { font-family: var(--ds-font-family-code); font-size: 11px; }
.url-code {
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  padding: 1px 5px;
  border-radius: 3px;
  font-family: var(--ds-font-family-code);
  font-size: 11px;
  color: var(--accent);
}
.row-actions { display: flex; align-items: center; gap: 3px; }
.btn-danger-icon { color: var(--danger); }
.btn-danger-icon:hover:not(:disabled) {
  background: var(--danger-bg);
  color: var(--danger);
  border-color: color-mix(in srgb, var(--danger) 30%, transparent);
}
</style>
