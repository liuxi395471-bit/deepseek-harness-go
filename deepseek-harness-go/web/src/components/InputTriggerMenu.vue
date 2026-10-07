<script setup lang="ts">
// InputTriggerMenu.vue — dsh 桌面端「/ 与 @ 输入触发器候选菜单」
//
// 对照 dsh 原生 ui-input-trigger：
//   - 输入 / 时显示 slash command 列表（含描述）
//   - 输入 @ 时显示 file/session 引用列表
//   - 键盘 ↑↓ 切换、Enter/Tab 选中、Esc 关闭
//   - hover 行也高亮
//   - 分为分组（组标题）
//
// 父组件控制 query 与 open 状态，本组件只渲染。

import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { Hash, AtSign, FileText, MessageSquare, ChevronRight } from 'lucide-vue-next'

export type TriggerKind = 'slash' | 'mention'

export interface TriggerCandidate {
  /** 唯一 id */
  id: string
  /** 类型 */
  kind: TriggerKind
  /** 主标题 */
  label: string
  /** 别名（如果与 label 不同时显示在右侧） */
  name?: string
  /** 描述（右对齐） */
  description?: string
  /** 缩略 / 文字 */
  icon?: 'command' | 'file' | 'session' | 'mention'
  /** 是否有 drill-down 二级菜单 */
  drill?: boolean
  /** 分组标题（用于分组） */
  group?: string
}

const props = defineProps<{
  visible: boolean
  kind: TriggerKind
  query: string
  candidates: TriggerCandidate[]
  /** 菜单位置（与输入框对齐） */
  anchor?: { top: number; left: number; width: number }
  activeId?: string
}>()

const emit = defineEmits<{
  (e: 'pick', c: TriggerCandidate, action?: 'pick' | 'drill'): void
  (e: 'close'): void
  (e: 'hover', id: string): void
}>()

const rootRef = ref<HTMLElement | null>(null)
const listRef = ref<HTMLElement | null>(null)

// 过滤 + 分组
const groupedCandidates = computed(() => {
  const q = props.query.toLowerCase()
  const filtered = props.candidates.filter(c => {
    if (!q) return true
    return (
      c.label.toLowerCase().includes(q) ||
      (c.name ?? '').toLowerCase().includes(q)
    )
  })
  const groups: Record<string, TriggerCandidate[]> = {}
  for (const c of filtered) {
    const g = c.group || ''
    if (!groups[g]) groups[g] = []
    groups[g].push(c)
  }
  // 保持原顺序
  const order: string[] = []
  for (const c of filtered) {
    const g = c.group || ''
    if (!order.includes(g)) order.push(g)
  }
  return order.map(g => ({ title: g, items: groups[g] }))
})

// 扁平候选（用于键盘导航）
const flatCandidates = computed(() => groupedCandidates.value.flatMap(g => g.items))

// 监听 visible 打开时滚动到 active
watch(
  () => props.visible,
  v => {
    if (v) {
      nextTick(() => {
        scrollToActive()
      })
    }
  }
)

watch(
  () => props.activeId,
  () => {
    nextTick(() => scrollToActive())
  }
)

function scrollToActive() {
  if (!listRef.value) return
  const el = listRef.value.querySelector(
    `[data-candidate-id="${props.activeId}"]`
  ) as HTMLElement | null
  if (el) {
    el.scrollIntoView({ block: 'nearest' })
  }
}

function handleKeydown(e: KeyboardEvent) {
  if (!props.visible) return
  if (e.key === 'Escape') {
    e.preventDefault()
    e.stopPropagation()
    emit('close')
    return
  }
  if (flatCandidates.value.length === 0) return
  if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
    e.preventDefault()
    moveActive(1)
  } else if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
    e.preventDefault()
    moveActive(-1)
  } else if (e.key === 'Enter') {
    e.preventDefault()
    const cur = flatCandidates.value.find(c => c.id === props.activeId)
    if (cur) {
      emit('pick', cur, 'pick')
    }
  }
}

function moveActive(delta: number) {
  const list = flatCandidates.value
  if (list.length === 0) return
  const idx = list.findIndex(c => c.id === props.activeId)
  let next: number
  if (idx < 0) {
    next = delta > 0 ? 0 : list.length - 1
  } else {
    next = (idx + delta + list.length) % list.length
  }
  emit('hover', list[next].id)
}

onMounted(() => {
  window.addEventListener('keydown', handleKeydown, true)
})

onUnmounted(() => {
  window.removeEventListener('keydown', handleKeydown, true)
})

function iconFor(c: TriggerCandidate) {
  if (c.icon === 'file') return FileText
  if (c.icon === 'session') return MessageSquare
  if (c.icon === 'mention') return AtSign
  if (c.kind === 'slash') return Hash
  return AtSign
}

function onMousedownRow(c: TriggerCandidate, e: MouseEvent) {
  // mousedown 而非 click：避免编辑器失焦
  e.preventDefault()
  emit('pick', c, c.drill ? 'drill' : 'pick')
}

function onHoverRow(c: TriggerCandidate) {
  emit('hover', c.id)
}
</script>

<template>
  <Transition name="trigger">
    <div
      v-if="visible"
      ref="rootRef"
      class="trigger-menu"
      :class="{ 'trigger-menu-empty': flatCandidates.length === 0 }"
      role="listbox"
    >
      <div v-if="flatCandidates.length === 0" class="trigger-empty">
        没有匹配项
      </div>
      <div v-else ref="listRef" class="trigger-list">
        <div
          v-for="group in groupedCandidates"
          :key="group.title || '_root'"
          class="trigger-group"
        >
          <div v-if="group.title" class="trigger-group-title">
            {{ group.title }}
          </div>
          <button
            v-for="c in group.items"
            :key="c.id"
            type="button"
            :data-candidate-id="c.id"
            class="trigger-row"
            :class="{ active: c.id === activeId }"
            @mousedown="onMousedownRow(c, $event)"
            @mouseenter="onHoverRow(c)"
          >
            <span class="trigger-row-icon">
              <component :is="iconFor(c)" :size="14" :stroke-width="1.6" />
            </span>
            <span class="trigger-row-main">
              <span class="trigger-row-label">{{ c.label }}</span>
              <span
                v-if="c.name && c.name.toLowerCase() !== c.label.toLowerCase()"
                class="trigger-row-alias"
                >{{ c.name }}</span
              >
            </span>
            <span v-if="c.description" class="trigger-row-desc">{{
              c.description
            }}</span>
            <ChevronRight v-if="c.drill" :size="12" class="trigger-row-drill" />
          </button>
        </div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.trigger-menu {
  position: absolute;
  z-index: 500;
  top: 0;
  left: 0;
  right: 0;
  transform: translateY(calc(-100% - 8px));
  max-height: 320px;
  overflow: hidden;
  border-radius: 12px;
  background: var(--bg-card, rgba(20, 14, 36, 0.95));
  border: 1px solid var(--border, rgba(255, 255, 255, 0.08));
  box-shadow: 0 16px 48px rgba(0, 0, 0, 0.5);
  backdrop-filter: blur(20px);
  -webkit-backdrop-filter: blur(20px);
  display: flex;
  flex-direction: column;
  color: var(--text);
}

.trigger-list {
  overflow-y: auto;
  padding: 4px;
  scrollbar-width: thin;
}

.trigger-group {
  display: flex;
  flex-direction: column;
}

.trigger-group-title {
  padding: 8px 10px 4px;
  font-size: 11px;
  font-weight: 600;
  text-transform: uppercase;
  letter-spacing: 0.06em;
  color: var(--text-faint);
}

.trigger-row {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 7px 10px;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: left;
  cursor: pointer;
  border-radius: 8px;
  transition: background 0.1s ease;
}

.trigger-row:hover,
.trigger-row.active {
  background: var(--bg-hover, rgba(255, 255, 255, 0.06));
}

.trigger-row-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  flex-shrink: 0;
  color: var(--text-faint);
}

.trigger-row.active .trigger-row-icon {
  color: var(--accent, #5b8bff);
}

.trigger-row-main {
  display: flex;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
  flex: 1;
}

.trigger-row-label {
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.trigger-row-alias {
  font-size: 11px;
  color: var(--text-faint);
  white-space: nowrap;
}

.trigger-row-desc {
  font-size: 11px;
  color: var(--text-faint);
  white-space: nowrap;
  text-align: right;
  flex-shrink: 0;
  max-width: 200px;
  overflow: hidden;
  text-overflow: ellipsis;
}

.trigger-row-drill {
  color: var(--text-faint);
  flex-shrink: 0;
}

.trigger-empty {
  padding: 18px;
  text-align: center;
  font-size: 12px;
  color: var(--text-faint);
}

.trigger-enter-active,
.trigger-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.trigger-enter-from,
.trigger-leave-to {
  opacity: 0;
  transform: translateY(4px);
}
</style>
