<script setup lang="ts">
// StepChain.vue — dsh 桌面端 step 步骤链
//
// 设计参考 dsh 原生 ChatGroupSeat 的 step 列表：
//   - 每个 step：图标 + 标题（步骤数 / 文件名）+ 可选预览
//   - tool 步骤有独立 row：图标 + 命令 + 输出预览

import { FileCode, Terminal, Pencil, Save, Search, Globe, ChevronRight, FileEdit, Wrench } from 'lucide-vue-next'

interface Step {
  id: string
  icon: 'edit' | 'terminal' | 'web' | 'search' | 'tool' | 'file'
  title: string
  detail?: string
  preview?: string
  previewKind?: 'code' | 'text'
  done?: boolean
}

defineProps<{
  steps: Step[]
}>()

function iconFor(kind: Step['icon']) {
  if (kind === 'edit') return Pencil
  if (kind === 'terminal') return Terminal
  if (kind === 'web') return Globe
  if (kind === 'search') return Search
  if (kind === 'file') return FileCode
  return Wrench
}
</script>

<template>
  <div class="step-chain">
    <div v-for="(s, i) in steps" :key="s.id" class="step-row">
      <div class="step-rail">
        <div class="step-dot" :class="{ done: s.done }">
          <component :is="iconFor(s.icon)" :size="11" />
        </div>
        <div v-if="i < steps.length - 1" class="step-line" />
      </div>
      <div class="step-body">
        <div class="step-title">
          <span class="step-num">{{ steps.length - i }}.</span>
          <span class="step-name">{{ s.title }}</span>
          <span v-if="s.done" class="step-done">✓</span>
        </div>
        <div v-if="s.detail" class="step-detail">{{ s.detail }}</div>
        <div v-if="s.preview" class="step-preview-wrap">
          <pre class="step-preview" :class="`step-preview-${s.previewKind ?? 'code'}`">{{ s.preview }}</pre>
        </div>
      </div>
    </div>
  </div>
</template>

<style scoped>
.step-chain {
  display: flex;
  flex-direction: column;
  gap: 0;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-md);
  padding: 8px 10px;
  font-size: 12px;
}
.step-row {
  display: flex;
  gap: 8px;
  align-items: flex-start;
  padding-bottom: 6px;
}
.step-row:last-child { padding-bottom: 0; }

.step-dot {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 18px;
  height: 18px;
  border-radius: 4px;
  background: var(--bg-elevated);
  border: 1px solid var(--border);
  color: var(--text-faint);
  flex-shrink: 0;
}
.step-dot.done {
  background: var(--success-bg);
  border-color: color-mix(in srgb, var(--success) 40%, transparent);
  color: var(--success);
}

.step-line {
  width: 1px;
  flex: 1;
  margin-top: 18px;
  background: var(--border);
  min-height: 8px;
}

.step-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.step-title {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 12px;
  color: var(--text);
  line-height: 1.5;
}
.step-num {
  font-size: 10px;
  font-weight: 700;
  color: var(--text-faint);
  font-variant-numeric: tabular-nums;
  min-width: 14px;
}
.step-name {
  font-weight: 500;
  color: var(--text);
  font-family: var(--ds-font-family-code);
  font-size: 11px;
  word-break: break-all;
}
.step-done {
  color: var(--success);
  margin-left: auto;
  font-size: 10px;
}

.step-detail {
  font-size: 11px;
  color: var(--text-faint);
  font-family: var(--ds-font-family-code);
  word-break: break-all;
}

.step-preview-wrap { margin-top: 2px; }
.step-preview {
  margin: 0;
  padding: 6px 8px;
  font-family: var(--ds-font-family-code);
  font-size: 11px;
  background: var(--bg-code);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  max-height: 140px;
  overflow: auto;
  white-space: pre;
}
.step-preview-text {
  font-family: var(--ds-font-family);
  white-space: pre-wrap;
  word-break: break-word;
}
</style>