import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

export default defineConfig({
  plugins: [vue()],
  build: {
    rollupOptions: {
      output: {
        // v1.4.0 的 UpdateView-C7IPX_Ad.js 会被 Edge / uBlock 按 *_Ad.js 拦截
        //（ERR_BLOCKED_BY_CLIENT），系统更新点了没反应。hex 降低概率，
        // .chunk.js 让文件名对不上这类规则。
        hashCharacters: 'hex',
        chunkFileNames: 'assets/[name]-[hash].chunk.js',
        entryFileNames: 'assets/[name]-[hash].chunk.js',
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: 'jsdom',
    environmentOptions: {
      jsdom: {
        url: 'http://localhost/',
      },
    },
    setupFiles: ['./src/test/setup.ts'],
    clearMocks: true,
    restoreMocks: true,
  },
})
