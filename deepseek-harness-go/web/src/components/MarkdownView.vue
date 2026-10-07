<script setup lang="ts">
// MarkdownView.vue
//
// 用 marked 把 markdown → HTML，再用 DOMPurify 防 XSS，highlight.js 加亮。
// 代码块在 markdown 渲染时就被替换为带"语言标签 + 复制按钮"的格式，
// 通过占位 div + 运行时初始化小工具按钮完成。

import { computed, onMounted, onUpdated, ref } from 'vue'
import { Copy, Check } from 'lucide-vue-next'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import goLang from 'highlight.js/lib/languages/go'
import shell from 'highlight.js/lib/languages/shell'
import yaml from 'highlight.js/lib/languages/yaml'
import sql from 'highlight.js/lib/languages/sql'
import 'highlight.js/styles/atom-one-light.css'
import { copyToClipboard } from '@/utils/format'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', goLang)
hljs.registerLanguage('shell', shell)
hljs.registerLanguage('yaml', yaml)
hljs.registerLanguage('sql', sql)

const props = defineProps<{ source: string }>()

// 长表/可变 Vue ref 标记
const rootEl = ref<HTMLElement | null>(null)

const html = computed(() => {
  const raw = marked.parse(props.source ?? '', { async: false }) as string
  // 先做代码高亮（在 DOMPurify 之前，避免被过滤）
  const highlighted = highlightCodeBlocks(raw)
  // 然后净化
  return DOMPurify.sanitize(highlighted, {
    ADD_ATTR: ['target', 'rel', 'data-lang', 'data-code'],
  })
})

function highlightCodeBlocks(input: string): string {
  return input.replace(
    /<pre><code class="language-(\w+)">([\s\S]*?)<\/code><\/pre>/g,
    (_m, lang: string, body: string) => {
      const decoded = body
        .replace(/&lt;/g, '<')
        .replace(/&gt;/g, '>')
        .replace(/&amp;/g, '&')
        .replace(/&quot;/g, '"')
      let out = decoded
      try {
        out = hljs.highlight(decoded, { language: lang, ignoreIllegals: true }).value
      } catch {
        /* ignore */
      }
      // 容器 + 头部 + pre。复制按钮通过 runtime 事件绑定。
      const cb = `<div class="md-codeblock" data-lang="${lang}" data-code="${encodeURIComponent(
        decoded,
      )}"><div class="md-cb-head"><span class="md-cb-lang">${lang}</span><button class="md-cb-copy" type="button"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg><span>复制</span></button></div><pre><code class="hljs language-${lang}">${out}</code></pre></div>`
      return cb
    },
  )
}

function bindCopyButtons() {
  const root = rootEl.value
  if (!root) return
  const buttons = root.querySelectorAll<HTMLButtonElement>('.md-cb-copy')
  buttons.forEach((btn) => {
    if (btn.dataset.bound === '1') return
    btn.dataset.bound = '1'
    btn.addEventListener('click', async () => {
      const container = btn.closest('.md-codeblock') as HTMLElement | null
      if (!container) return
      const raw = decodeURIComponent(container.dataset.code || '')
      await copyToClipboard(raw)
      const span = btn.querySelector('span')
      const original = span?.textContent || '复制'
      if (span) span.textContent = '已复制'
      btn.classList.add('md-cb-copied')
      setTimeout(() => {
        if (span) span.textContent = original
        btn.classList.remove('md-cb-copied')
      }, 1500)
    })
  })
}

onMounted(bindCopyButtons)
onUpdated(bindCopyButtons)
</script>

<template>
  <div ref="rootEl" class="markdown" v-html="html" />
</template>

<style scoped>
.markdown {
  font-size: 14px;
  line-height: 1.7;
  word-wrap: break-word;
  color: var(--text);
}
.markdown :deep(p) {
  margin: 0 0 10px 0;
}
.markdown :deep(p:last-child) {
  margin-bottom: 0;
}
.markdown :deep(ul),
.markdown :deep(ol) {
  margin: 0 0 10px 0;
  padding-left: 24px;
}
.markdown :deep(li) {
  margin: 4px 0;
}
.markdown :deep(h1),
.markdown :deep(h2),
.markdown :deep(h3),
.markdown :deep(h4) {
  font-weight: 600;
  margin: 16px 0 8px;
  line-height: 1.3;
}
.markdown :deep(h1) { font-size: 18px; }
.markdown :deep(h2) { font-size: 16px; }
.markdown :deep(h3) { font-size: 15px; }
.markdown :deep(h4) { font-size: 14px; }
.markdown :deep(blockquote) {
  margin: 8px 0;
  padding: 4px 12px;
  border-left: 3px solid var(--border);
  color: var(--text-muted);
  background: var(--bg);
  border-radius: 4px;
}
.markdown :deep(table) {
  border-collapse: collapse;
  margin: 8px 0;
}
.markdown :deep(table th),
.markdown :deep(table td) {
  border: 1px solid var(--border);
  padding: 6px 10px;
  font-size: 13px;
}
.markdown :deep(table th) {
  background: var(--bg);
  font-weight: 600;
}
.markdown :deep(a) {
  color: var(--accent);
  text-decoration: none;
}
.markdown :deep(a:hover) {
  text-decoration: underline;
}
.markdown :deep(hr) {
  border: 0;
  border-top: 1px solid var(--border);
  margin: 12px 0;
}
.markdown :deep(.md-codeblock) {
  margin: 10px 0;
  border-radius: 8px;
  background: #1a202c;
  overflow: hidden;
  border: 1px solid #2d3748;
}
:global([data-theme="dark"]) .markdown :deep(.md-codeblock) {
  background: #0f172a;
  border-color: #1e293b;
}
.markdown :deep(.md-cb-head) {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 12px;
  background: rgba(255, 255, 255, 0.05);
  border-bottom: 1px solid #2d3748;
  font-size: 11px;
}
:global([data-theme="dark"]) .markdown :deep(.md-cb-head) {
  border-bottom-color: #1e293b;
}
.markdown :deep(.md-cb-lang) {
  color: #a0aec0;
  text-transform: lowercase;
  font-family: "SF Mono", Consolas, "Liberation Mono", monospace;
}
.markdown :deep(.md-cb-copy) {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.1);
  color: #a0aec0;
  border-radius: 4px;
  padding: 2px 8px;
  cursor: pointer;
  font-size: 11px;
  transition: background 0.15s, color 0.15s;
}
.markdown :deep(.md-cb-copy:hover) {
  background: rgba(255, 255, 255, 0.08);
  color: #edf2f7;
}
.markdown :deep(.md-cb-copied) {
  color: #4ade80 !important;
  border-color: #4ade80 !important;
}
.markdown :deep(.md-codeblock pre) {
  margin: 0;
  padding: 12px;
  overflow-x: auto;
  background: transparent !important;
  color: #edf2f7;
}
.markdown :deep(.md-codeblock code) {
  font-family: "SF Mono", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
  background: transparent !important;
  color: inherit !important;
  padding: 0 !important;
  white-space: pre;
}
.markdown :deep(code:not(pre code)) {
  background: var(--bg);
  border: 1px solid var(--border);
  padding: 1px 6px;
  border-radius: 4px;
  font-family: "SF Mono", Consolas, "Liberation Mono", monospace;
  font-size: 0.9em;
  color: var(--accent);
}
</style>