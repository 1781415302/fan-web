import api, { type ApiResponse, unwrap } from './index'
import type { LibraryScanResult } from '../types/library'

// 库扫描是同步执行的长时间后端任务（遍历目录 + 串行调用外部 Bangumi 接口），
// 首次全库扫描极易超过全局 axios 10s 超时；超时后客户端中止请求但后端仍会继续执行，
// 前端会误报"扫描失败"。这里为扫描请求单独放宽超时，避免大库场景被提前中止。
const SCAN_TIMEOUT_MS = 600_000

export async function scanLibrary(): Promise<LibraryScanResult> {
  const response = await api.post<ApiResponse<LibraryScanResult>>('/library/scan', undefined, {
    timeout: SCAN_TIMEOUT_MS,
  })
  return unwrap(response)
}
