import { defineStore } from 'pinia'

export const useAppStore = defineStore('app', {
  state: () => ({
    siteName: '番剧库',
  }),
})
