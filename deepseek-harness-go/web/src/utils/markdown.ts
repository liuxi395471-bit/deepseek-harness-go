/**
 * Markdown 渲染工具。
 *
 * 流程：marked 解析 → highlight.js 高亮代码块 → DOMPurify 消毒。
 *
 * 安全：模型输出与工具输出都是不可信内容，必须经过 DOMPurify 消毒
 * 才能插入 DOM，否则会成为 XSS 入口。
 */

import DOMPurify from 'dompurify'
import hljs from 'highlight.js/lib/common'
import { Marked } from 'marked'

/**
 * 只注册常用语言，而不是 highlight.js 的全部 190+ 种。
 *
 * `highlight.js/lib/common` 已经包含最常见的语言（js/ts/python/go/
 * json/bash/sql/yaml/markdown 等），体积比完整包小一个数量级；
 * 未注册的语言会退化为纯文本，功能不受影响。
 */
void hljs

const marked = new Marked({
  gfm: true,
  breaks: true,
})

/** escapeHTML 把特殊字符转义，用于安全地嵌入 HTML。 */
function escapeHTML(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;')
}

/**
 * highlightCode 对一段代码做语法高亮。
 *
 * 未知的语言名会退化为纯文本（hljs.getLanguage 返回 undefined）。
 */
function highlightCode(code: string, lang: string | undefined): string {
  const language = lang?.trim().toLowerCase() ?? ''
  try {
    if (language && hljs.getLanguage(language)) {
      return hljs.highlight(code, { language, ignoreIllegals: true }).value
    }
    return escapeHTML(code)
  } catch {
    return escapeHTML(code)
  }
}

/**
 * 自定义 renderer：代码块走 highlight.js。
 *
 * marked v15 的 renderer 方法接收 token 对象而非位置参数。
 */
marked.use({
  renderer: {
    code({ text, lang }) {
      const highlighted = highlightCode(text, lang)
      const cls = lang ? ` class="language-${escapeHTML(lang)}"` : ''
      return `<pre><code${cls}>${highlighted}</code></pre>\n`
    },
  },
})

/**
 * renderMarkdown 把 Markdown 文本渲染成安全的 HTML 字符串。
 *
 * @param text 原始 Markdown 文本
 */
export function renderMarkdown(text: string): string {
  if (!text) return ''

  let html: string
  try {
    html = marked.parse(text, { async: false }) as string
  } catch {
    // 解析失败：退化为纯文本，避免整条消息消失。
    return escapeHTML(text)
  }

  // 消毒：白名单化标签与属性，移除 script / on* 事件等。
  return DOMPurify.sanitize(html, {
    ALLOWED_TAGS: [
      'p', 'br', 'hr', 'div', 'span',
      'strong', 'em', 'del', 'sub', 'sup', 'mark',
      'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'ul', 'ol', 'li',
      'blockquote', 'pre', 'code',
      'a', 'img',
      'table', 'thead', 'tbody', 'tr', 'th', 'td',
      'input',
    ],
    ALLOWED_ATTR: ['href', 'title', 'src', 'alt', 'class', 'type', 'checked', 'disabled'],
    ALLOW_DATA_ATTR: false,
  })
}

/**
 * formatArgs 把工具调用的参数字符串格式化为易读的 JSON。
 *
 * 后端传的是 JSON 编码的字符串（可能是空串或 "null"），
 * 这里尝试解析并缩进；解析失败则原样返回。
 */
export function formatArgs(args: string | undefined): string {
  if (!args || args === 'null') return '{}'
  try {
    return JSON.stringify(JSON.parse(args), null, 2)
  } catch {
    return args
  }
}