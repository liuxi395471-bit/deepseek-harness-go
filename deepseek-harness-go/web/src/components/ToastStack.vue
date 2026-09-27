<script setup lang="ts">
// ToastStack.vue — 把 uiStore.toasts 渲染为右上角通知列表。
import { useUIStore } from '@/stores/ui'
import { X, CheckCircle2, AlertCircle, Info } from 'lucide-vue-next'

const ui = useUIStore()
</script>

<template>
  <div class="toast-container" data-test="toast-container">
    <div
      v-for="t in ui.toasts"
      :key="t.id"
      :class="['toast', t.level === 'error' && 'toast-error', t.level === 'success' && 'toast-success']"
    >
      <component
        :is="t.level === 'error' ? AlertCircle : t.level === 'success' ? CheckCircle2 : Info"
        :size="16"
        :style="{ marginTop: '2px' }"
      />
      <div style="flex: 1; min-width: 0;">
        <div class="toast-title">{{ t.title }}</div>
        <div v-if="t.message" class="toast-msg">{{ t.message }}</div>
      </div>
      <button class="toast-close" @click="ui.dismissToast(t.id)" aria-label="dismiss">×</button>
    </div>
  </div>
</template>
