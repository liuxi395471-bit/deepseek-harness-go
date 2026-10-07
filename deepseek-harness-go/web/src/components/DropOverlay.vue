<script setup lang="ts">
// DropOverlay.vue — dsh 桌面端「拖文件全屏覆盖」
//
// 对照 dsh 原生 DropOverlay.tsx：
//   - 拖文件经过文档时显示全屏覆盖层
//   - 中央 illustration + 标题 + 限制行
//   - 不接受拖入时灰显
//
// 父组件通过 prop 控制显示与文案。

import { Upload } from 'lucide-vue-next'

defineProps<{
  visible: boolean
  disabled?: boolean
  title?: string
  limits?: string
}>()
</script>

<template>
  <Transition name="drop">
    <div v-if="visible" class="drop-overlay" :class="{ disabled }">
      <div class="drop-card">
        <div class="drop-icon">
          <Upload :size="48" :stroke-width="1.4" />
        </div>
        <div class="drop-title">{{ title || '拖到此处以附加文件' }}</div>
        <div v-if="limits" class="drop-limits">{{ limits }}</div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.drop-overlay {
  position: fixed;
  inset: 0;
  z-index: 9999;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.55);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  pointer-events: none; /* 透传拖动事件，父组件在 document 上拦截 */
}

.drop-overlay.disabled {
  background: rgba(0, 0, 0, 0.75);
}

.drop-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding: 32px 48px;
  border-radius: 20px;
  background: var(--bg-card, rgba(20, 14, 36, 0.7));
  border: 1.5px dashed var(--accent, #5b8bff);
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.5);
  color: var(--text);
  text-align: center;
  min-width: 320px;
}

.disabled .drop-card {
  border-color: var(--text-faint);
  opacity: 0.6;
}

.drop-icon {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 80px;
  height: 80px;
  border-radius: 50%;
  background: linear-gradient(135deg, rgba(91, 139, 255, 0.2), rgba(91, 139, 255, 0.05));
  color: var(--accent, #5b8bff);
}

.disabled .drop-icon {
  background: var(--bg-elevated);
  color: var(--text-faint);
}

.drop-title {
  font-size: 18px;
  font-weight: 600;
  letter-spacing: 0.01em;
}

.drop-limits {
  font-size: 12px;
  color: var(--text-faint);
  letter-spacing: 0.02em;
}

/* dsh 原生 0.2s 渐变 */
.drop-enter-active,
.drop-leave-active {
  transition: opacity 0.2s ease-in-out;
}

.drop-enter-from,
.drop-leave-to {
  opacity: 0;
}
</style>
