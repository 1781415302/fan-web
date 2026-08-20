import api, { type ApiResponse, unwrap } from './index'
import type { PaginatedUnidentified, ScanJob } from '../types/library'

// 全库扫描改为作业：POST 立即返回 ScanJob，GET 读状态。
// 轮询走全局 10s 超时，禁止再按同步 600s 处理。
// Web 不接 GET /library/dirs。

export async function startLibraryScan(): Promise<ScanJob> {
  const response = await api.post<ApiResponse<ScanJob>>('/library/scan')
  return unwrap(response)
}

export async function getLibraryScan(): Promise<ScanJob> {
  const response = await api.get<ApiResponse<ScanJob>>('/library/scan')
  return unwrap(response)
}

export async function listUnidentified(page = 1, pageSize = 50): Promise<PaginatedUnidentified> {
  const response = await api.get<ApiResponse<PaginatedUnidentified>>('/library/unidentified', {
    params: { page, page_size: pageSize },
  })
  return unwrap(response)
}
