import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath, URL } from 'node:url'

// Vite 配置。
//
// 开发模式：前端跑在 5173，通过 proxy 把 /api 与 /healthz 转发到
// Go 后端（默认 127.0.0.1:8080），这样浏览器侧同源，无需处理 CORS。
//
// 生产模式：`npm run build` 产出 dist/，由 Go 的 internal/webui 通过
// go:embed 直接内嵌进二进制，单进程提供前后端。
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
      '/healthz': {
        target: 'http://127.0.0.1:8080',
        changeOrigin: true,
      },
    },
  },
  build: {
    // 直接产出到 Go 的 embed 目录，使 `go build` 能把前端内嵌进二进制。
    // 这样 `dsh -serve` 单进程即可提供完整前后端，无需额外部署。
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
    // 产物体积不大，但保留 sourcemap 便于排查线上问题。
    sourcemap: false,
    chunkSizeWarningLimit: 900,
  },
})
