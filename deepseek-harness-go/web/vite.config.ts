import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// Vite 配置：Vue 3 SPA + 路径别名 + gzip 预算检查
//
// 构建产物输出到 dist/，被 internal/console/embed.go 通过 //go:embed
// 注入；预算 gzip ≤ 300KB（见 docs/v8/DESIGN-v8.md §6）。
//
// base='/console/'：让所有构建产物（js/css/static/asset）的 url 带 /console/
// 前缀，匹配 dsh 的 /console/* SPA 路由前缀。开发期仍可在根路径
// 通过 vite proxy 转发到 /api/v1/console。
export default defineConfig({
  base: '/console/',
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      // dev 时把 /api/v1/console/* 代理到 dsh -serve
      '/api/v1/console': {
        target: 'http://127.0.0.1:7777',
        changeOrigin: true,
      },
    },
  },
  build: {
    target: 'es2022',
    outDir: 'dist',
    emptyOutDir: true,
    sourcemap: false,
    rollupOptions: {
      output: {
        // 拆包让 vendor 与 app 分开缓存
        manualChunks: {
          vendor: ['vue', 'vue-router', 'pinia', 'axios'],
          query: ['@tanstack/vue-query'],
          render: ['marked', 'dompurify', 'highlight.js'],
        },
      },
    },
    chunkSizeWarningLimit: 320, // KB（≈ gzip 300KB + 余量）
  },
})
