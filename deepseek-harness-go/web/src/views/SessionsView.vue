<script setup lang="ts">
// SessionsView.vue
//
// 会话列表 + 新建会话（带 title & model 字段）。

import { useQuery, useMutation, useQueryClient } from '@tanstack/vue-query'
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, Trash2, MessageSquare } from 'lucide-vue-next'
import { sessionsApi } from '@/api/sessions'
import { formatRelative } from '@/utils/format'

const router = useRouter()
const qc = useQueryClient()

const list = useQuery({
  queryKey: ['sessions'],
  queryFn: () => sessionsApi.list(50),
  refetchInterval: 5000,
})

const showNew = ref(false)
const newTitle = ref('')
const newModel = ref('deepseek-chat')

const createMut = useMutation({
  mutationFn: () => sessionsApi.create(newTitle.value || '未命名会话', newModel.value),
  onSuccess: (session) => {
    qc.invalidateQueries({ queryKey: ['sessions'] })
    showNew.value = false
    newTitle.value = ''
    router.push(`/sessions/${encodeURIComponent(session.sid)}`)
  },
})

const removeMut = useMutation({
  mutationFn: (sid: string) => sessionsApi.remove(sid),
  onSuccess: () => qc.invalidateQueries({ queryKey: ['sessions'] }),
})

function open(sid: string) {
  router.push(`/sessions/${encodeURIComponent(sid)}`)
}

function handleDelete(sid: string) {
  if (window.confirm('删除会话？')) removeMut.mutate(sid)
}
</script>

<template>
  <div>
    <div class="flex items-center justify-between mb-2">
      <h2 class="text-lg font-bold">会话列表</h2>
      <button class="btn btn-primary" @click="showNew = !showNew">
        <Plus :size="14" />
        新建会话
      </button>
    </div>

    <div v-if="showNew" class="card mb-2">
      <div class="flex gap-3 items-center">
        <input v-model="newTitle" placeholder="会话标题（可选）" />
        <select v-model="newModel" style="width: auto; min-width: 180px;">
          <option value="deepseek-chat">deepseek-chat</option>
          <option value="deepseek-reasoner">deepseek-reasoner</option>
          <option value="claude-3-5-sonnet">claude-3-5-sonnet</option>
        </select>
        <button
          class="btn btn-primary"
          :disabled="createMut.isPending.value"
          @click="createMut.mutate()"
        >
          {{ createMut.isPending.value ? '创建中…' : '创建' }}
        </button>
      </div>
      <div v-if="createMut.error.value" class="error-banner mt-2">
        {{ (createMut.error.value as Error).message }}
      </div>
    </div>

    <div v-if="list.error.value" class="error-banner">
      {{ (list.error.value as Error).message }}
    </div>

    <div v-if="list.isLoading.value" class="empty">加载中…</div>

    <div v-else-if="(list.data.value?.items ?? []).length === 0" class="empty">
      <MessageSquare :size="32" style="opacity: 0.3;" />
      <p>还没有会话。点击右上角"新建会话"开始。</p>
    </div>

    <div v-else class="list">
      <div
        v-for="s in list.data.value?.items ?? []"
        :key="s.sid"
        class="card session-card"
        @click="open(s.sid)"
      >
        <div class="flex items-center justify-between">
          <div style="flex: 1; min-width: 0;">
            <div class="font-bold">{{ s.title || '未命名' }}</div>
            <div class="text-sm text-muted mt-2 session-preview">
              {{ s.preview || '（暂无内容）' }}
            </div>
            <div class="text-xs text-muted mt-2">
              {{ s.model }} · {{ s.rounds }} 轮 · {{ formatRelative(s.updatedAt) }}
            </div>
          </div>
          <button
            class="btn btn-danger"
            @click.stop="handleDelete(s.sid)"
          >
            <Trash2 :size="14" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.session-card {
  cursor: pointer;
  transition: border-color 0.15s, transform 0.15s;
}
.session-card:hover {
  border-color: var(--accent);
}
.session-preview {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  max-width: 700px;
}
</style>
