import { writeFileSync } from 'node:fs'
import { fileURLToPath, URL } from 'node:url'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const placeholderContent =
  '# 占位文件：保证前端未构建时 go:embed 仍可编译。\n' +
  '# 执行 `pnpm --dir web-admin build`（或 make build）后会生成真正的 dist 内容。\n'

const keepDistPlaceholder = {
  name: 'keep-dist-placeholder',
  closeBundle() {
    writeFileSync('dist/.gitkeep', placeholderContent)
  },
}

export default defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  plugins: [react(), keepDistPlaceholder],
  server: {
    port: 5174,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/test/setup.ts',
    css: false,
    testTimeout: 20000,
    hookTimeout: 20000,
    fileParallelism: false,
  },
})
