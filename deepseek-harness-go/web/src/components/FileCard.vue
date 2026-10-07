<script setup lang="ts">
// FileCard.vue — dsh 桌面端「文件编辑 / 创建」卡片
//
// 设计参考 dsh 原生 ui-tool 的 edit / write 渲染：
//   - 大字号文件路径（mono 字体）
//   - 行号列（line:col）
//   - 操作描述（"Go Hello World 程序…"）
//   - 文件预览（代码片段）
//   - 底部操作（diff / open / undo）

import { FileCode, FilePlus, FileEdit, ExternalLink, RotateCcw, ChevronDown, ChevronRight } from 'lucide-vue-next'
import { ref } from 'vue'

interface Props {
  kind: 'edit' | 'create'
  path: string
  lineCol?: string
  description?: string
  diff?: string
  language?: string
  preview?: string
}

const props = defineProps<Props>()

const showDiff = ref(false)

function kindIcon() {
  return props.kind === 'create' ? FilePlus : FileEdit
}
function kindLabel() {
  return props.kind === 'create' ? '已创建' : '已编辑'
}
</script>

<template>
  <div class="file-card">
    <div class="fc-header">
      <component :is="kindIcon()" :size="13" class="fc-icon" />
      <span class="fc-kind">{{ kindLabel() }}</span>
      <span class="fc-path truncate">{{ path }}</span>
      <span v-if="lineCol" class="fc-line">{{ lineCol }}</span>
    </div>
    <div v-if="description" class="fc-desc">{{ description }}</div>
    <div v-if="preview" class="fc-preview-wrap">
      <pre class="fc-preview"><code>{{ preview }}</code></pre>
    </div>
    <div v-if="diff" class="fc-diff-wrap">
      <button class="fc-toggle" @click="showDiff = !showDiff">
        <component :is="showDiff ? ChevronDown : ChevronRight" :size="11" />
        <span>View diff ({{ diff.split('\n').length }} 行)</span>
      </button>
      <pre v-if="showDiff" class="fc-diff">{{ diff }}</pre>
    </div>
    <div class="fc-footer">
      <button class="fc-action">
        <ExternalLink :size="11" />
        打开
      </button>
      <button class="fc-action">
        <RotateCcw :size="11" />
        撤销
      </button>
    </div>
  </div>
</template>

<style scoped>
.file-card {
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-md);
  padding: 8px 10px;
  font-size: 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.fc-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  min-width: 0;
}
.fc-icon {
  color: var(--accent);
  flex-shrink: 0;
}
.fc-kind {
  font-size: 10px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-faint);
  background: var(--bg-elevated);
  padding: 1px 5px;
  border-radius: 3px;
  flex-shrink: 0;
}
.fc-path {
  font-family: var(--ds-font-family-code);
  font-size: 11px;
  font-weight: 600;
  color: var(--text);
  flex: 1;
}
.fc-line {
  font-family: var(--ds-font-family-code);
  font-size: 10px;
  color: var(--text-faint);
  background: var(--bg-elevated);
  padding: 1px 4px;
  border-radius: 3px;
  flex-shrink: 0;
}

.fc-desc {
  font-size: 11px;
  color: var(--text-muted);
  line-height: 1.5;
  padding-left: 18px;
}

.fc-preview-wrap { padding-left: 18px; }
.fc-preview {
  margin: 0;
  padding: 6px 8px;
  font-family: var(--ds-font-family-code);
  font-size: 11px;
  background: var(--bg-code);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  overflow-x: auto;
  white-space: pre;
  max-height: 220px;
  overflow-y: auto;
}
.fc-preview code {
  font: inherit;
  color: inherit;
}

.fc-diff-wrap { padding-left: 18px; }
.fc-toggle {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  background: transparent;
  border: none;
  color: var(--text-faint);
  font-size: 10px;
  font-weight: 500;
  cursor: pointer;
  padding: 2px 4px;
  border-radius: var(--ds-radius-xs);
}
.fc-toggle:hover {
  background: var(--bg-hover);
  color: var(--text-muted);
}
.fc-diff {
  margin: 4px 0 0;
  padding: 6px 8px;
  font-family: var(--ds-font-family-code);
  font-size: 10px;
  background: var(--bg-code);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  max-height: 200px;
  overflow: auto;
  white-space: pre;
}

.fc-footer {
  display: flex;
  align-items: center;
  gap: 6px;
  padding-left: 18px;
  border-top: 1px solid var(--dsw-alias-border-l1, transparent);
  padding-top: 6px;
}
.fc-action {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 10px;
  font-weight: 500;
  color: var(--text-faint);
  background: transparent;
  border: none;
  cursor: pointer;
  padding: 2px 6px;
  border-radius: var(--ds-radius-xs);
}
.fc-action:hover {
  color: var(--text);
  background: var(--bg-hover);
}
</style>