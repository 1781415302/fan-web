import api, { type ApiResponse, unwrap } from './index'
import type { LibraryScanResult } from '../types/library'

export async function scanLibrary(): Promise<LibraryScanResult> {
  const response = await api.post<ApiResponse<LibraryScanResult>>('/library/scan')
  return unwrap(response)
}
