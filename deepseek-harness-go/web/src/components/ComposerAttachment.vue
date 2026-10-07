<script setup lang="ts">
// ComposerAttachment.vue — dsh 桌面端「composer 附件 rail」单元
//
// 对照 dsh 原生 ui-attachment/src/FileCard.tsx + AttachmentRail：
//   - 文件附件：240×64 DeepSeek Web card，16px radius，
//     蓝色渐变 document glyph + 文件名（省略） +
//     大写扩展名（截 8 字）+ 字节大小（如 "PNG 124 KB"）
//   - 状态：uploading (spinner + 进度条) / ready / error (retry)
//   - 移除按钮：默认 0 透明，hover/focus 出现 (touch 常显)
//   - 图片附件：64×64 缩略图 + 移除按钮 + lightbox
//
// 所有 props 由父组件传入；本组件是纯展示，状态由 SessionDetailView 维护。

import { computed, ref } from 'vue'
import { X, FileText, RefreshCw } from 'lucide-vue-next'
import { useI18n } from '@/i18n'

export type AttachmentKind = 'file' | 'image'
export type AttachmentState = 'uploading' | 'ready' | 'error'

export interface ComposerAttachmentItem {
  /** 唯一 id（用于 React-style diff 跟 key 列表） */
  id: string
  /** 原始文件名（含扩展名） */
  name: string
  /** 字节数 */
  bytes: number
  /** mime type */
  mime: string
  /** 'file' 或 'image' */
  kind: AttachmentKind
  /** 'uploading' | 'ready' | 'error' */
  state: AttachmentState
  /** 上传进度 0..1（uploading 状态时可选） */
  progress?: number
  /** 预览 URL（image 必填；file 可选 — 浏览器 blob URL） */
  previewUrl?: string
}

const props = defineProps<{
  item: ComposerAttachmentItem
  /** 当前 hover 状态由父组件 hover 计时控制（rail 动画） */
  showRemove?: boolean
}>()

const emit = defineEmits<{
  (e: 'remove', id: string): void
  (e: 'retry', id: string): void
  (e: 'preview', id: string): void
}>()

const { t } = useI18n()

const removeHover = ref(false)

// 文件扩展名（大写、限 8 字符，dsh 行为）
const ext = computed(() => {
  const dot = props.item.name.lastIndexOf('.')
  if (dot < 0 || dot === props.item.name.length - 1) return ''
  return props.item.name.slice(dot + 1).toUpperCase().slice(0, 8)
})

// 文件大小格式
const sizeText = computed(() => formatBytes(props.item.bytes))

// meta line
const metaText = computed(() => {
  if (props.item.state === 'uploading') {
    if (props.item.progress !== undefined && props.item.progress > 0) {
      return t('attachment.uploading').replace(
        '{percent}',
        String(Math.round(props.item.progress * 100))
      )
    }
    return t('attachment.uploading')
  }
  if (props.item.state === 'error') {
    return t('attachment.failed')
  }
  // ready
  if (!ext.value) return sizeText.value
  return `${ext.value} ${sizeText.value}`
})

// 进度条 0..1
const progressPct = computed(() => {
  if (props.item.state !== 'uploading') return 0
  if (props.item.progress === undefined) return 0
  return Math.max(0, Math.min(1, props.item.progress)) * 100
})

function onRemove() {
  emit('remove', props.item.id)
}
function onRetry() {
  emit('retry', props.item.id)
}
function onPreview() {
  emit('preview', props.item.id)
}

function formatBytes(b: number): string {
  if (b < 1024) return `${b} B`
  if (b < 1024 * 1024) return `${(b / 1024).toFixed(0)} KB`
  if (b < 1024 * 1024 * 1024) return `${(b / 1024 / 1024).toFixed(1)} MB`
  return `${(b / 1024 / 1024 / 1024).toFixed(2)} GB`
}
</script>

<template>
  <!-- 文件附件：240×64 dsh FileCard -->
  <div
    v-if="item.kind === 'file'"
    class="att-card file-card"
    :class="{ failed: item.state === 'error' }"
    :title="item.name"
  >
    <span class="att-icon" aria-hidden>
      <span v-if="item.state === 'uploading'" class="spinner" />
      <FileText v-else :size="18" :stroke-width="1.5" class="file-glyph" />
    </span>

    <span class="att-body">
      <span class="att-name">{{ item.name }}</span>
      <span class="att-meta" :class="{ 'meta-failed': item.state === 'error' }">
        {{ metaText }}
      </span>
    </span>

    <button
      v-if="item.state === 'error'"
      type="button"
      class="att-retry"
      :aria-label="t('attachment.retry')"
      @click="onRetry"
    >
      <RefreshCw :size="13" :stroke-width="1.5" />
    </button>

    <button
      type="button"
      class="att-remove"
      :class="{ 'remove-failed': item.state === 'error' }"
      :aria-label="t('attachment.remove').replace('{name}', item.name)"
      @click="onRemove"
      @mouseenter="removeHover = true"
      @mouseleave="removeHover = false"
    >
      <X :size="11" :stroke-width="2" />
    </button>

    <span
      v-if="item.state === 'uploading'"
      class="att-progress-track"
      aria-hidden
    >
      <span
        class="att-progress-bar"
        :style="{
          width:
            item.progress !== undefined && item.progress > 0
              ? `${progressPct}%`
              : undefined,
          animation:
            item.progress === undefined || item.progress === 0
              ? 'att-progress-indeterminate 1.2s ease-in-out infinite alternate'
              : 'none',
        }"
      />
    </span>
  </div>

  <!-- 图片附件：64×64 缩略图 + 移除 -->
  <div v-else class="att-card image-card">
    <button
      type="button"
      class="att-thumb"
      :title="t('attachment.openOriginal')"
      @click="onPreview"
    >
      <img v-if="item.previewUrl" :src="item.previewUrl" :alt="item.name" />
      <div v-else class="att-thumb-placeholder">
        <FileText :size="20" :stroke-width="1.2" />
      </div>
    </button>
    <button
      type="button"
      class="att-remove image-remove"
      :aria-label="t('attachment.remove').replace('{name}', item.name)"
      @click="onRemove"
    >
      <X :size="11" :stroke-width="2" />
    </button>
  </div>
</template>

<style scoped>
/* dsh 原生：240×64、16px radius、蓝色渐变 document glyph */
.att-card {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 10px;
  width: 240px;
  height: 64px;
  padding: 0 12px;
  border: 0.5px solid var(--dsw-alias-border-l2, rgba(255, 255, 255, 0.12));
  border-radius: var(--ds-radius-lg, 16px);
  background: var(--dsw-specific-input-major, rgba(255, 255, 255, 0.04));
  box-sizing: border-box;
  text-align: left;
  flex-shrink: 0;
}

.file-card.failed {
  border-color: var(--dsw-alias-state-error-primary, #d54941);
}

.att-icon {
  display: inline-flex;
  flex: none;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: 6px;
  background: linear-gradient(135deg, #3b82f6 0%, #1d4ed8 100%);
  color: #fff;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.2);
}

.att-icon .spinner {
  width: 16px;
  height: 16px;
  border: 2px solid currentColor;
  border-top-color: transparent;
  border-radius: 50%;
  animation: att-spin 0.8s linear infinite;
}

.file-glyph {
  color: #fff;
}

.att-body {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  padding: 8px 0;
}

.att-name {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--dsw-alias-label-primary, var(--text));
  font-size: 14px;
  font-weight: 500;
  line-height: 22px;
}

.att-meta {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  color: var(--dsw-alias-label-tertiary, var(--text-faint));
  font-size: 12px;
  line-height: 15px;
}

.meta-failed {
  color: var(--dsw-alias-state-error-primary, #d54941);
}

.att-remove {
  position: absolute;
  top: 6px;
  right: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: var(--dsw-alias-button-contrast-fill, rgba(0, 0, 0, 0.72));
  color: var(--dsw-alias-label-primary-inverted, #fff);
  opacity: 0;
  cursor: pointer;
  transition: opacity 0.2s ease-in-out;
}

.file-card:hover .att-remove,
.att-remove:focus-visible {
  opacity: 1;
}

.image-remove {
  opacity: 1; /* image 缩略图常显 */
}

.remove-failed {
  background: var(--dsw-alias-state-error-primary, #d54941);
  color: #fff;
  opacity: 1;
}

.att-retry {
  position: absolute;
  top: 6px;
  right: 28px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  padding: 0;
  border: none;
  border-radius: 50%;
  background: var(--dsw-alias-state-error-primary, #d54941);
  color: #fff;
  opacity: 0;
  cursor: pointer;
  transition: opacity 0.2s ease-in-out;
}

.file-card:hover .att-retry,
.att-retry:focus-visible {
  opacity: 1;
}

@media (pointer: coarse) {
  .att-remove,
  .att-retry {
    opacity: 1;
  }
}

.file-card:hover .att-name,
.file-card:focus-within .att-name {
  padding-right: 18px;
}

/* 上传进度条（dsh 原生 1.2s ease-in-out 动画） */
.att-progress-track {
  position: absolute;
  right: 12px;
  bottom: 5px;
  left: 12px;
  overflow: hidden;
  height: 2px;
  border-radius: 1px;
  background: var(--dsw-alias-fill-tertiary, rgba(255, 255, 255, 0.08));
}

.att-progress-bar {
  display: block;
  width: 35%;
  height: 100%;
  border-radius: inherit;
  background: var(--dsw-alias-brand-primary, #4d6bfe);
  transition: width 0.18s ease-out;
}

@keyframes att-spin {
  to {
    transform: rotate(360deg);
  }
}

@keyframes att-progress-indeterminate {
  from {
    transform: translateX(-70%);
  }
  to {
    transform: translateX(220%);
  }
}

/* 图片附件：64×64 缩略图 */
.image-card {
  width: 64px;
  height: 64px;
  padding: 0;
  border-radius: 12px;
  overflow: hidden;
}

.att-thumb {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 100%;
  height: 100%;
  padding: 0;
  border: none;
  background: transparent;
  cursor: pointer;
  overflow: hidden;
}

.att-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
}

.att-thumb-placeholder {
  width: 100%;
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  color: var(--text-faint);
  background: var(--bg-elevated);
}
</style>
