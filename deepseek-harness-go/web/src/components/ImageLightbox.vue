<script setup lang="ts">
// ImageLightbox.vue — dsh 桌面端「图片全屏预览」
//
// 对照 dsh 原生 ImageLightbox（ui-primitives）：
//   - 黑色 80% 背景覆盖
//   - 居中显示原图 fit-to-viewport（不缩放）
//   - Escape 关闭、点击 mask 关闭、右上角 close 按钮
//   - 关闭后焦点还原到 opener（父组件负责）

import { onMounted, onUnmounted } from 'vue'
import { X } from 'lucide-vue-next'

const props = defineProps<{
  src: string
  alt?: string
  visible: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

function handleKeydown(e: KeyboardEvent) {
  if (!props.visible) return
  if (e.key === 'Escape') {
    e.stopPropagation()
    emit('close')
  }
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown, true)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown, true)
})

function onMask() {
  emit('close')
}
</script>

<template>
  <Transition name="lightbox">
    <div
      v-if="visible"
      class="lightbox-mask"
      role="dialog"
      aria-modal="true"
      @click="onMask"
    >
      <button
        type="button"
        class="lightbox-close"
        aria-label="关闭预览"
        @click.stop="onMask"
      >
        <X :size="20" :stroke-width="1.5" />
      </button>
      <img
        v-if="src"
        :src="src"
        :alt="alt"
        class="lightbox-img"
        @click.stop
      />
    </div>
  </Transition>
</template>

<style scoped>
.lightbox-mask {
  position: fixed;
  inset: 0;
  z-index: 10000;
  display: flex;
  align-items: center;
  justify-content: center;
  background: rgba(0, 0, 0, 0.82);
  backdrop-filter: blur(12px);
  -webkit-backdrop-filter: blur(12px);
  cursor: zoom-out;
}

.lightbox-img {
  max-width: 92vw;
  max-height: 92vh;
  object-fit: contain;
  cursor: auto;
  user-select: none;
  -webkit-user-drag: none;
  border-radius: 6px;
  box-shadow: 0 24px 72px rgba(0, 0, 0, 0.6);
}

.lightbox-close {
  position: absolute;
  top: 18px;
  right: 18px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.1);
  color: #fff;
  cursor: pointer;
  transition: background 0.15s ease;
}

.lightbox-close:hover {
  background: rgba(255, 255, 255, 0.2);
}

.lightbox-enter-active,
.lightbox-leave-active {
  transition: opacity 0.2s ease-in-out;
}

.lightbox-enter-from,
.lightbox-leave-to {
  opacity: 0;
}
</style>
