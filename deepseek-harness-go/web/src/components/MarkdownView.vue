<script setup lang="ts">
// MarkdownView.vue
//
// 用 marked 把 markdown → HTML，再用 DOMPurify 防 XSS，highlight.js 加亮。
// 用于 assistant 消息体渲染。

import { computed } from 'vue'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import python from 'highlight.js/lib/languages/python'
import 'highlight.js/styles/atom-one-light.css'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('python', python)

const props = defineProps<{ source: string }>()

marked.setOptions({
  gfm: true,
  breaks: true,
})

const html = computed(() => {
  const raw = marked.parse(props.source ?? '', { async: false }) as string
  // 在 DOMPurify 后做代码高亮（避免被过滤）
  const cleaned = DOMPurify.sanitize(raw, { ADD_ATTR: ['target', 'rel'] })
  return highlightInPlace(cleaned)
})

function highlightInPlace(cleaned: string): string {
  return cleaned.replace(
    /<code class="language-(\w+)">([\s\S]*?)<\/code>/g,
    (_m, lang: string, body: string) => {
      const decoded = body
        .replace(/&lt;/g, '<')
        .replace(/&gt;/g, '>')
        .replace(/&amp;/g, '&')
        .replace(/&quot;/g, '"')
      let out: string
      try {
        out = hljs.highlight(decoded, { language: lang, ignoreIllegals: true }).value
      } catch {
        out = decoded
      }
      return `<code class="language-${lang} hljs">${out}</code>`
    },
  )
}
</script>

<template>
  <div class="markdown" v-html="html" />
</template>

<style scoped>
.markdown {
  font-size: 13px;
  line-height: 1.6;
  word-wrap: break-word;
}
.markdown :deep(pre) {
  background: #1a202c;
  color: #edf2f7;
  padding: 12px;
  border-radius: 6px;
  overflow-x: auto;
}
.markdown :deep(code) {
  font-family: "SF Mono", Consolas, "Liberation Mono", monospace;
  font-size: 12px;
}
.markdown :deep(p) {
  margin: 0 0 8px 0;
}
.markdown :deep(ul),
.markdown :deep(ol) {
  margin: 0 0 8px 0;
  padding-left: 24px;
}
.markdown :deep(a) {
  color: var(--accent);
}
</style>
