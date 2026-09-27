<script setup lang="ts">
// SchedulesView — 定时任务管理（v8.1）
//
// 列表 + 创建 + 启用/停用 + 立即运行。
// Action 字段是 JSON 字符串，例如：
//   {"type":"agent_run","prompt":"hello"}
//   {"type":"webhook","webhookId":"abc123"}

import { onMounted, ref } from 'vue'
import { Plus, Play, Trash2, Power, PowerOff } from 'lucide-vue-next'
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
  <div class="schedules-page">
    <div class="page-header">
      <h1>{{ t('schedules.title') }}</h1>
      <button class="btn btn-primary" @click="showCreate = true">
        <Plus :size="14" />
        {{ t('schedules.create') }}
      </button>
    </div>

    <div v-if="loading" class="empty">loading…</div>
    <div v-else-if="items.length === 0" class="empty">{{ t('schedules.empty') }}</div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>{{ t('schedules.name') }}</th>
          <th>{{ t('schedules.cron') }}</th>
          <th>状态</th>
          <th>lastRun</th>
          <th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="item in items" :key="item.id">
          <td>{{ item.name }}</td>
          <td><code>{{ item.cron }}</code></td>
          <td>
            <span :class="['badge', item.enabled ? 'badge-loaded' : 'badge-failed']">
              {{ item.enabled ? 'enabled' : 'disabled' }}
            </span>
          </td>
          <td>
            <span v-if="item.lastRunAt" :title="item.lastError || ''">
              {{ item.lastStatus || '—' }}
            </span>
            <span v-else>—</span>
          </td>
          <td class="actions">
            <button class="btn btn-icon" @click="onRun(item)" :title="t('schedules.runNow')">
              <Play :size="14" />
            </button>
            <button class="btn btn-icon" @click="onToggle(item)" :title="item.enabled ? t('schedules.disable') : t('schedules.enable')">
              <component :is="item.enabled ? PowerOff : Power" :size="14" />
            </button>
            <button class="btn btn-icon btn-danger" @click="onDelete(item)" :title="t('common.delete')">
              <Trash2 :size="14" />
            </button>
          </td>
        </tr>
      </tbody>
    </table>

    <Modal :open="showCreate" :title="t('schedules.dialogTitle')" @close="showCreate = false">
      <template #default>
        <div class="form-row">
          <label>{{ t('schedules.name') }}</label>
          <input v-model="draft.name" />
        </div>
        <div class="form-row">
          <label>{{ t('schedules.cron') }}</label>
          <input v-model="draft.cron" placeholder="*/5 * * * *" />
        </div>
        <div class="form-row">
          <label>{{ t('schedules.action') }}</label>
          <textarea v-model="draft.action" rows="4"></textarea>
        </div>
      </template>
      <template #footer>
        <button class="btn" @click="showCreate = false">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" @click="onCreate">{{ t('common.confirm') }}</button>
      </template>
    </Modal>
  </div>
</template>

<style scoped>
.schedules-page { display: flex; flex-direction: column; gap: 16px; }
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
.form-row input, .form-row textarea { width: 100%; padding: 6px 8px; background: var(--bg); color: var(--text); border: 1px solid var(--border); border-radius: 4px; font-size: 13px; font-family: inherit; }
code { background: var(--bg); padding: 1px 4px; border-radius: 3px; font-size: 12px; }
.badge { display: inline-block; padding: 2px 6px; border-radius: 3px; font-size: 11px; background: var(--border); color: var(--text-muted); }
.badge-loaded { background: rgba(34, 197, 94, 0.15); color: rgb(34, 197, 94); }
.badge-failed { background: rgba(239, 68, 68, 0.15); color: rgb(239, 68, 68); }
</style>
