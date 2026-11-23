import '@testing-library/jest-dom/vitest'
import { configure } from '@testing-library/dom'

// AntD 页面渲染较重，放宽异步等待时间，避免并行测试时误报。
configure({ asyncUtilTimeout: 5000 })

// Ant Design 的响应式组件依赖 matchMedia，jsdom 未实现。
Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

// 部分组件会在挂载时使用 ResizeObserver。
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

if (!('ResizeObserver' in window)) {
  Object.defineProperty(window, 'ResizeObserver', {
    writable: true,
    value: ResizeObserverStub,
  })
}
