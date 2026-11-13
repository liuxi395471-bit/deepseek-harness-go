<script setup lang="ts">
/**
 * 消息输入框 —— ChatGPT 风格的圆角输入区。
 *
 * 特性：
 * - Enter 发送，Shift+Enter 换行。
 * - 自动高度（随内容增长），有最大高度后转为滚动。
 * - 发送按钮在运行中变为"停止"按钮。
 */
import { computed, nextTick, ref, watch } from 'vue'
import { ArrowUp, Square } from 'lucide-vue-next'

const props = defineProps<{
  /** 是否正在运行（决定发送/停止按钮）。 */
  running: boolean
  /** 禁用的原因（如会话正在加载）。 */
  disabled?: boolean
}>()

const emit = defineEmits<{
  (e: 'send', text: string): void
  (e: 'stop'): void
}>()

const text = ref('')
const textarea = ref<HTMLTextAreaElement | null>(null)

/** 是否有可发送的内容。 */
const canSend = computed(() => text.value.trim().length > 0)

/** 调整 textarea 高度以贴合内容。 */
async function resize(): Promise<void> {
  const el = textarea.value
  if (!el) return
  el.style.height = 'auto'
  el.style.height = `${Math.min(el.scrollHeight, 320)}px`
}

watch(text, () => {
  void nextTick(resize)
})

function submit(): void {
  if (!canSend.value || props.disabled) return
  emit('send', text.value.trim())
  text.value = ''
  void nextTick(resize)
}

function onKeydown(e: KeyboardEvent): void {
  // Enter 发送；Shift+Enter 换行。
  // IME 组合输入期间（isComposing）不触发发送，避免中文输入法误触。
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    submit()
  }
}
</script>

<template>
  <div class="composer">
    <div class="composer__box" :class="{ 'composer__box--disabled': disabled }">
      <textarea
        ref="textarea"
        v-model="text"
        class="composer__input"
        rows="1"
        :placeholder="running ? '正在生成中…' : '给 DeepSeek Harness 发送消息…'"
        :disabled="disabled"
        aria-label="消息输入框"
        @keydown="onKeydown"
      ></textarea>

      <button
        v-if="running"
        class="composer__btn composer__btn--stop"
        title="停止生成"
        aria-label="停止生成"
        @click="emit('stop')"
      >
        <Square :size="15" fill="currentColor" />
      </button>
      <button
        v-else
        class="composer__btn"
        :class="{ 'composer__btn--ready': canSend }"
        title="发送"
        aria-label="发送"
        :disabled="!canSend"
        @click="submit"
      >
        <ArrowUp :size="17" />
      </button>
    </div>

    <p class="composer__hint">
      DeepSeek Harness 可能会出错，请核实重要信息。Enter 发送，Shift + Enter 换行。
    </p>
  </div>
</template>

<style scoped>
.composer {
  width: 100%;
  max-width: var(--content-max-width);
  margin: 0 auto;
  padding: 0 16px 12px;
}

.composer__box {
  display: flex;
  align-items: flex-end;
  gap: 8px;
  padding: 10px 10px 10px 16px;
  background: var(--bg-input);
  border: 1px solid var(--border-strong);
  border-radius: var(--radius-xl);
  box-shadow: var(--shadow-sm);
  transition: border-color var(--transition), box-shadow var(--transition);
}

.composer__box:focus-within {
  border-color: var(--text-tertiary);
  box-shadow: var(--shadow-md);
}

.composer__box--disabled {
  opacity: 0.6;
}

.composer__input {
  flex: 1;
  min-width: 0;
  resize: none;
  border: none;
  outline: none;
  background: transparent;
  font-size: 16px;
  line-height: 1.55;
  padding: 6px 0;
  max-height: 320px;
  overflow-y: auto;
  font-family: inherit;
}

.composer__input::placeholder {
  color: var(--text-tertiary);
}

.composer__btn {
  display: flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: var(--bg-active);
  color: var(--text-tertiary);
  flex-shrink: 0;
  transition: background var(--transition), color var(--transition), transform var(--transition);
}

.composer__btn--ready {
  background: var(--text-primary);
  color: var(--text-inverse);
}

[data-theme='dark'] .composer__btn--ready {
  background: var(--accent);
  color: #0d0d0d;
}

.composer__btn--ready:hover {
  background: var(--accent);
  color: #ffffff;
  transform: scale(1.05);
}

.composer__btn:disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.composer__btn--stop {
  background: var(--text-primary);
  color: var(--text-inverse);
}

[data-theme='dark'] .composer__btn--stop {
  background: var(--danger);
  color: #ffffff;
}

.composer__btn--stop:hover {
  background: var(--danger);
  color: #ffffff;
}

.composer__hint {
  margin: 9px 0 0;
  text-align: center;
  font-size: 11.5px;
  color: var(--text-tertiary);
  line-height: 1.4;
}
</style>
