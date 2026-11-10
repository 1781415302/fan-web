import api, { ApiError, type ApiResponse, unwrap } from './index'
import type {
  ConfirmErrorData,
  ConfirmInboxRequest,
  ConfirmInboxResponse,
  PaginatedUnidentified,
  ScanJob,
} from '../types/library'

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

// 收件箱确认在服务端同步执行"建档 + 限定文件扫描"，耗时与单番扫描同量级；
// 走全局 10s 会在扫描途中超时，因此与 scanAnime 一样单独 600s。
export const CONFIRM_INBOX_TIMEOUT_MS = 600_000

const CONFIRM_ERROR_REASONS: readonly ConfirmErrorData['reason'][] = [
  'review_required',
  'upstream',
  'conflict',
  'dir_mismatch',
  'no_match',
  'invalid',
  'unsupported_engine',
]

const CODE_INVALID_PARAMS = 1001

// ConfirmInboxError 保留信封 data 中的 reason / file_names，供卡片内联渲染与分支处理。
// 继承 ApiError，既有 `e instanceof ApiError` 的处理与 code 判断仍然成立。
export class ConfirmInboxError extends ApiError {
  readonly reason: ConfirmErrorData['reason']
  readonly file_names: string[]

  constructor(code: number, reason: ConfirmErrorData['reason'], message: string, fileNames: string[] = []) {
    super(code, message)
    this.name = 'ConfirmInboxError'
    this.reason = reason
    this.file_names = fileNames
  }
}

function isConfirmReason(value: unknown): value is ConfirmErrorData['reason'] {
  return typeof value === 'string' && (CONFIRM_ERROR_REASONS as readonly string[]).includes(value)
}

function toConfirmInboxError(code: number, message: string, data: unknown): ConfirmInboxError {
  const detail = typeof data === 'object' && data !== null ? (data as Partial<ConfirmErrorData>) : {}
  // 参数校验类错误可能由框架层返回而不带 data，按 code 降级为 invalid；其它缺省按 upstream。
  const reason = isConfirmReason(detail.reason)
    ? detail.reason
    : code === CODE_INVALID_PARAMS
      ? 'invalid'
      : 'upstream'
  const detailMessage = typeof detail.message === 'string' && detail.message ? detail.message : ''
  const fileNames = Array.isArray(detail.file_names)
    ? detail.file_names.filter((name): name is string => typeof name === 'string')
    : []
  return new ConfirmInboxError(code, reason, detailMessage || message || '确认失败', fileNames)
}

// 不得使用 unwrap：unwrap 在 code!==0 时只抛 ApiError(code, message)，会丢掉 data.reason。
export async function confirmInbox(req: ConfirmInboxRequest): Promise<ConfirmInboxResponse> {
  const response = await api.post<ApiResponse<ConfirmInboxResponse | ConfirmErrorData | null>>(
    '/library/inbox/confirm',
    req,
    { timeout: CONFIRM_INBOX_TIMEOUT_MS },
  )
  const result = response.data as unknown
  if (typeof result !== 'object' || result === null || !('code' in result)) {
    throw new ApiError(-1, '响应格式错误：未返回有效的 ApiResponse')
  }
  const envelope = result as ApiResponse<unknown>
  if (envelope.code !== 0) {
    throw toConfirmInboxError(envelope.code, envelope.message, envelope.data)
  }
  return envelope.data as ConfirmInboxResponse
}
