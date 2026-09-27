<script setup lang="ts">
// WebhooksView — Webhook 通知管理（v8.1）
//
// 列表 + 创建 + 测试 + 删除 + 投递历史查看。
// HMAC-SHA256 签名密钥仅在创建/更新时返回一次。

import { onMounted, ref } from 'vue'
import { Plus, Trash2, Send, History } from 'lucide-vue-next'
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

// 投递历史 modal
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
  <div class="webhooks-page">
    <div class="page-header">
      <h1>{{ t('webhooks.title') }}</h1>
      <button class="btn btn-primary" @click="showCreate = true">
        <Plus :size="14" />
        {{ t('webhooks.create') }}
      </button>
    </div>

    <div v-if="loading" class="empty">loading…</div>
    <div v-else-if="items.length === 0" class="empty">{{ t('webhooks.empty') }}</div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>{{ t('webhooks.name') }}</th>
          <th>{{ t('webhooks.url') }}</th>
          <th>状态</th>
          <th>lastStatus</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id">
          <td>{{ item.name }}</td>
          <td><code>{{ item.url }}</code></td>
          <td>
            <span :class="['badge', item.enabled ? 'badge-loaded' : 'badge-failed']">
              {{ item.enabled ? 'enabled' : 'disabled' }}
            </span>
          </td>
          <td>{{ item.lastStatus || '—' }}</td>
          <td class="actions">
            <button class="btn btn-icon" @click="onShowDeliveries(item)" :title="t('webhooks.deliveries')">
              <History :size="14" />
            </button>
            <button class="btn btn-icon" @click="onTest(item)" :title="t('webhooks.test')">
              <Send :size="14" />
            </button>
            <button class="btn btn-icon btn-danger" @click="onDelete(item)" :title="t('common.delete')">
              <Trash2 :size="14" />
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <Modal
      :open="showCreate"
      :title="t('webhooks.dialogTitle')"
      @close="showCreate = false"
    >
      <template #default>
        <div class="form-row">
          <label>{{ t('webhooks.name') }}</label>
          <input v-model="draft.name" />
        </div>
        <div class="form-row">
          <label>{{ t('webhooks.url') }}</label>
          <input v-model="draft.url" placeholder="https://..." />
        </div>
        <div class="form-row">
          <label>{{ t('webhooks.secret') }}</label>
          <input v-model="draft.secret" placeholder="（留空则不签名）" />
        </div>
      </template>
      <template #footer>
        <button class="btn" @click="showCreate = false">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="onCreate">{{ t('common.confirm') }}</button>
      </template>
    </Modal>

    <Modal
      :open="showDeliveries"
      :title="`${t('webhooks.deliveries')} — ${currentWebhook?.name ?? ''}`"
      @close="showDeliveries = false"
    >
      <template #default>
        <div v-if="currentDeliveries.length === 0" class="empty">无投递记录</div>
        <table v-else class="table">
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
              <td class="mono">{{ new Date(d.deliveredAt).toLocaleString() }}</td>
              <td>
                <span :class="['badge', d.ok ? 'badge-loaded' : 'badge-failed']">
                  {{ d.statusCode }}
                </span>
              </td>
              <td>{{ d.attempt }}</td>
              <td>{{ d.error || '' }}</td>
            </tr>
          </tbody>
        </table>
      </template>
      <template #footer>
        <button class="btn" @click="showDeliveries = false">{{ t('common.close') }}</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.webhooks-page { display: flex; flex-direction: column; gap: 16px; }
.page-header { display: flex; justify-content: space-between; align-items: center; }
.page-header h1 { font-size: 18px; margin: 0; }
.empty { color: var(--text-muted); padding: 20px; text-align: center; }
.table { width: 100%; border-collapse: collapse; }
.table th, .table td { padding: 8px 12px; text-align: left; border-bottom: 1px solid var(--border); font-size: 13px; }
.table th { font-weight: 600; color: var(--text-muted); font-size: 12px; }
.actions { display: flex; gap: 4px; justify-content: flex-end; }
.btn { display: inline-flex; align-items: center; gap: 4px; padding: 4px 10px; border: 1px solid var(--border); border-radius: 4px; background: var(--bg-card); color: var(--text); cursor: pointer; font-size: 12px; }
.btn-primary { background: var(--accent); color: #fff; border-color: var(--accent); }
.btn-icon { padding: 4px; }
.btn-danger { color: var(--danger); }
.btn:hover:not(:disabled) { opacity: 0.85; }
.form-row { margin-bottom: 12px; }
.form-row label { display: block; font-size: 12px; color: var(--text-muted); margin-bottom: 4px; }
.form-row input { width: 100%; padding: 6px 8px; background: var(--bg); color: var(--text); border: 1px solid var(--border); border-radius: 4px; font-size: 13px; }
code { background: var(--bg); padding: 1px 4px; border-radius: 3px; font-size: 12px; }
.mono { font-family: monospace; font-size: 12px; }
.badge { display: inline-block; padding: 2px 6px; border-radius: 3px; font-size: 11px; background: var(--border); color: var(--text-muted); }
.badge-loaded { background: rgba(34, 197, 94, 0.15); color: rgb(34, 197, 94); }
.badge-failed { background: rgba(239, 68, 68, 0.15); color: rgb(239, 68, 68); }
</style>
