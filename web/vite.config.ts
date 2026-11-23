import { writeFileSync } from 'node:fs'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vitest/config'

const placeholderContent =
  '# 占位文件：保证前端未构建时 go:embed 仍可编译。\n' +
  '# 执行 `pnpm --dir web build`（或 make build）后会生成真正的 dist 内容。\n'

// Vite 构建会清空 dist；构建结束后重新写入占位文件，确保 go:embed 始终有内容可嵌。
const keepDistPlaceholder = {
  name: 'keep-dist-placeholder',
  closeBundle() {
    writeFileSync('dist/.gitkeep', placeholderContent)
  },
}

export default defineConfig({
  plugins: [react(), keepDistPlaceholder],
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://127.0.0.1:8080',
    },
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
