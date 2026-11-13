<script setup lang="ts">
/**
 * 单条消息 —— 助手消息（Markdown 渲染 + 工具调用卡片）与
 * 用户消息（右对齐气泡）的统一渲染单元。
 */
import { computed, ref } from 'vue'
import { ChevronDown, CircleAlert, Copy, Check, Terminal, Wrench } from 'lucide-vue-next'
import type { UiMessage } from '@/composables/useChat'
import { formatArgs, renderMarkdown } from '@/utils/markdown'

const props = defineProps<{
  msg: UiMessage
}>()

/** 助手消息正文渲染为 HTML（已消毒）。 */
const html = computed(() =>
  props.msg.role === 'assistant' ? renderMarkdown(props.msg.content) : '',
)

/** 用户消息保留换行即可。 */
const isUser = computed(() => props.msg.role === 'user')

/** 复制按钮的"已复制"反馈。 */
const copied = ref(false)

async function copy(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    setTimeout(() => (copied.value = false), 1600)
  } catch {
    /* 剪贴板不可用（如非安全上下文）：静默忽略 */
  }
}
</script>

<template>
  <article class="msg" :class="`msg--${msg.role}`">
    <!-- 用户消息：右对齐气泡 -->
    <div v-if="isUser" class="bubble">
      <p class="bubble__text">{{ msg.content }}</p>
    </div>

    <!-- 助手消息：无气泡，宽度受限 -->
    <div v-else class="assistant">
      <div v-if="msg.content || msg.streaming" class="md-body" v-html="html"></div>

      <!-- 工具调用卡片 -->
      <div v-if="msg.tools.length" class="tools">
        <details
          v-for="tool in msg.tools"
          :key="tool.callId"
          class="tool"
          :class="{ 'tool--error': tool.isError, 'tool--running': tool.running }"
        >
          <summary class="tool__summary">
            <span class="tool__badge">
              <Terminal v-if="tool.running" :size="13" class="spin" />
              <CircleAlert v-else-if="tool.isError" :size="13" />
              <Wrench v-else :size="13" />
            </span>
            <span class="tool__name">{{ tool.name }}</span>
            <span v-if="tool.running" class="tool__status">执行中…</span>
            <span v-else-if="tool.isError" class="tool__status tool__status--error">
              失败
            </span>
            <span v-else-if="tool.tookMs !== undefined" class="tool__status">
              {{ tool.tookMs }} ms
            </span>
            <ChevronDown :size="15" class="tool__chevron" />
          </summary>

          <div class="tool__body">
            <div class="tool__section">
              <div class="tool__label">入参</div>
              <pre class="tool__code">{{ formatArgs(tool.args) }}</pre>
            </div>
            <div v-if="tool.result !== undefined" class="tool__section">
              <div class="tool__label">结果</div>
              <pre class="tool__code" :class="{ 'tool__code--error': tool.isError }">{{
                tool.result
              }}</pre>
            </div>
          </div>
        </details>
      </div>

      <!-- 空内容 + 流式中：显示呼吸点 -->
      <div v-if="msg.streaming && !msg.content && !msg.tools.length" class="thinking">
        <span></span><span></span><span></span>
      </div>

      <!-- 复制按钮：仅在内容完成后出现 -->
      <button
        v-if="msg.content && !msg.streaming"
        class="copy-btn"
        :title="copied ? '已复制' : '复制'"
        :aria-label="copied ? '已复制' : '复制'"
        @click="copy(msg.content)"
      >
        <Check v-if="copied" :size="14" />
        <Copy v-else :size="14" />
      </button>
    </div>
  </article>
</template>

<style scoped>
.msg {
  display: flex;
  margin-bottom: 26px;
  animation: fade-in 200ms ease-out;
}

/* 用户消息：右对齐 */
.msg--user {
  justify-content: flex-end;
}

.bubble {
  max-width: 85%;
  background: var(--bg-user-bubble);
  border-radius: var(--radius-lg);
  padding: 10px 16px;
}

.bubble__text {
  margin: 0;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  line-height: 1.6;
}

/* 助手消息：左对齐，无气泡 */
.msg--assistant {
  justify-content: flex-start;
}

.assistant {
  max-width: 100%;
  width: 100%;
  position: relative;
}

/* 工具调用卡片 */
.tools {
  margin-top: 12px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.tool {
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-md);
  background: var(--bg-subtle);
  overflow: hidden;
  transition: border-color var(--transition);
}

.tool:hover {
  border-color: var(--border-strong);
}

.tool--error {
  border-color: var(--danger);
  background: var(--danger-soft);
}

.tool--running {
  border-color: var(--accent);
}

.tool__summary {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 9px 12px;
  cursor: pointer;
  font-size: 13.5px;
  list-style: none;
  user-select: none;
}

.tool__summary::-webkit-details-marker {
  display: none;
}

.tool__badge {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  border-radius: 6px;
  background: var(--bg-active);
  color: var(--text-secondary);
  flex-shrink: 0;
}

.tool--error .tool__badge {
  background: transparent;
  color: var(--danger);
}

.tool--running .tool__badge {
  background: var(--accent-soft);
  color: var(--accent-text);
}

.tool__name {
  font-weight: 500;
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12.5px;
}

.tool__status {
  font-size: 12px;
  color: var(--text-tertiary);
}

.tool__status--error {
  color: var(--danger);
}

.tool__chevron {
  margin-left: auto;
  color: var(--text-tertiary);
  transition: transform var(--transition);
}

.tool[open] .tool__chevron {
  transform: rotate(180deg);
}

.tool__body {
  padding: 0 12px 12px;
  border-top: 1px solid var(--border-subtle);
}

.tool__section {
  margin-top: 10px;
}

.tool__label {
  font-size: 11.5px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.04em;
  color: var(--text-tertiary);
  margin-bottom: 5px;
}

.tool__code {
  margin: 0;
  padding: 10px 12px;
  background: var(--bg-code);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-sm);
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 12.5px;
  line-height: 1.55;
  overflow-x: auto;
  white-space: pre-wrap;
  overflow-wrap: anywhere;
  max-height: 340px;
  overflow-y: auto;
}

.tool__code--error {
  color: var(--danger);
}

/* 思考中的呼吸点 */
.thinking {
  display: flex;
  gap: 5px;
  padding: 6px 0;
}

.thinking span {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: var(--text-tertiary);
  animation: pulse-dot 1.3s ease-in-out infinite;
}

.thinking span:nth-child(2) {
  animation-delay: 0.16s;
}
.thinking span:nth-child(3) {
  animation-delay: 0.32s;
}

/* 复制按钮 */
.copy-btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  border-radius: var(--radius-sm);
  color: var(--text-tertiary);
  opacity: 0;
  transition: opacity var(--transition), background var(--transition);
  margin-top: 6px;
}

.assistant:hover .copy-btn {
  opacity: 1;
}

.copy-btn:hover {
  background: var(--bg-hover);
  color: var(--text-primary);
}

/* 旋转动画（工具执行中） */
.spin {
  animation: spin 900ms linear infinite;
}
</style>
