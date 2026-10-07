<script setup lang="ts">
// SchedulesView — 定时任务管理（dsh 风格）

import { onMounted, ref } from 'vue'
import { Plus, Play, Trash2, Power, PowerOff, RefreshCw, Clock } from 'lucide-vue-next'
import { useUIStore } from '@/stores/ui'
import { useI18n } from '@/i18n'
import Modal from '@/components/Modal.vue'
import {
  listSchedules,
  createSchedule,
  updateSchedule,
  deleteSchedule,
  runSchedule,
  type ScheduleItem,
} from '@/api/schedules'

const ui = useUIStore()
const { t } = useI18n()

const items = ref<ScheduleItem[]>([])
const loading = ref(false)
const showCreate = ref(false)
const draft = ref({ name: '', cron: '*/5 * * * *', action: '{"type":"agent_run","prompt":"hello"}' })

async function refresh() {
  loading.value = true
  try {
    items.value = await listSchedules()
  } catch (e) {
    ui.reportError(e)
  } finally {
    loading.value = false
  }
}

async function onCreate() {
  if (!draft.value.name || !draft.value.cron) return
  try {
    const item = await createSchedule({
      name: draft.value.name,
      cron: draft.value.cron,
      action: draft.value.action,
      enabled: true,
    })
    items.value.push(item)
    showCreate.value = false
    draft.value = { name: '', cron: '*/5 * * * *', action: '{"type":"agent_run","prompt":"hello"}' }
    ui.pushToast('success', t('common.confirm'), item.name)
  } catch (e) {
    ui.reportError(e)
  }
}

async function onToggle(item: ScheduleItem) {
  try {
    const next = await updateSchedule(item.id, { ...item, enabled: !item.enabled })
    Object.assign(item, next)
  } catch (e) {
    ui.reportError(e)
  }
}

async function onRun(item: ScheduleItem) {
  try {
    const next = await runSchedule(item.id)
    Object.assign(item, next)
    ui.pushToast('success', t('common.confirm'), `→ ${item.name}`)
  } catch (e) {
    ui.reportError(e)
  }
}

async function onDelete(item: ScheduleItem) {
  if (!confirm(`Delete "${item.name}"?`)) return
  try {
    await deleteSchedule(item.id)
    items.value = items.value.filter((i) => i.id !== item.id)
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
        <h1 class="page-title">定时任务</h1>
        <span class="page-count">{{ items.length }}</span>
      </div>
      <div class="ph-right">
        <button class="btn btn-icon" @click="refresh" title="刷新">
          <RefreshCw :size="13" />
        </button>
        <button class="btn btn-primary" @click="showCreate = true">
          <Plus :size="13" />
          新建调度
        </button>
      </div>
    </header>

    <div class="content-body">
      <div v-if="loading" class="empty">loading…</div>
      <div v-else-if="items.length === 0" class="empty">
        <Clock :size="28" class="empty-icon" />
        <div class="empty-title">暂无定时任务</div>
      </div>

      <table v-else class="data-table">
        <thead>
          <tr>
            <th>名称</th>
            <th>Cron</th>
            <th>状态</th>
            <th>最近</th>
            <th style="width: 140px;">操作</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="item in items" :key="item.id">
            <td class="td-name">{{ item.name }}</td>
            <td><code class="cron-code">{{ item.cron }}</code></td>
            <td>
              <span :class="['badge', item.enabled ? 'badge-success' : 'badge-disabled']">
                {{ item.enabled ? 'enabled' : 'disabled' }}
              </span>
            </td>
            <td>
              <span v-if="item.lastRunAt" :title="item.lastError || ''">
                <span :class="['badge', item.lastStatus === 'ok' ? 'badge-success' : 'badge-failed']">
                  {{ item.lastStatus || '—' }}
                </span>
              </span>
              <span v-else class="text-xs text-faint">—</span>
            </td>
            <td>
              <div class="row-actions">
                <button class="btn btn-sm btn-icon" @click="onRun(item)" title="立即运行">
                  <Play :size="11" />
                </button>
                <button class="btn btn-sm btn-icon" @click="onToggle(item)" :title="item.enabled ? '停用' : '启用'">
                  <component :is="item.enabled ? PowerOff : Power" :size="11" />
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

    <Modal :open="showCreate" :title="t('schedules.dialogTitle')" @close="showCreate = false">
      <label>
        <span>名称</span>
        <input v-model="draft.name" />
      </label>
      <label>
        <span>Cron 表达式</span>
        <input v-model="draft.cron" placeholder="*/5 * * * *" />
      </label>
      <label>
        <span>动作 (JSON)</span>
        <textarea v-model="draft.action" rows="4"></textarea>
      </label>
      <template #footer>
        <button class="btn" @click="showCreate = false">取消</button>
        <button class="btn btn-primary" @click="onCreate">创建</button>
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
.cron-code {
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
