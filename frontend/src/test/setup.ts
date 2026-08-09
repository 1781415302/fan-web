import { afterEach } from 'vitest'

// Node 26 的全局 localStorage 在未提供 --localstorage-file 时为 undefined，
// 且会遮蔽 jsdom window 上的 localStorage。这里提供一个等价的内存实现，
// 使测试环境行为与浏览器一致。
if (!Object.prototype.hasOwnProperty.call(window, 'localStorage') || !window.localStorage) {
  const store = new Map<string, string>()
  const memoryStorage: Storage = {
    get length() {
      return store.size
    },
    clear() {
      store.clear()
    },
    getItem(key: string) {
      return store.has(key) ? store.get(key)! : null
    },
    key(index: number) {
      return Array.from(store.keys())[index] ?? null
    },
    removeItem(key: string) {
      store.delete(key)
    },
    setItem(key: string, value: string) {
      store.set(key, String(value))
    },
  }
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    enumerable: true,
    value: memoryStorage,
    writable: false,
  })
}

// 每个测试后清理 localStorage，恢复 mock 与 fake timers。
afterEach(() => {
  window.localStorage.clear()
})