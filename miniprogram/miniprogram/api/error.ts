/** 后端统一错误形状：`{"error": "<稳定机器码>", "message": "<中文>"}`（api-contract §1）。 */
export interface ApiErrorBody {
  error: string
  message: string
}

/** 与后端的稳定错误码一一对应；这里只列客户端需要分支处理的。 */
export const ErrorCode = {
  unauthorized: 'unauthorized',
  invalidToken: 'invalid_token',
  tokenRevoked: 'token_revoked',
  refreshTokenExpired: 'refresh_token_expired',
  wechatNeedsBinding: 'wechat_needs_binding',
} as const

export class ApiError extends Error {
  readonly status: number
  readonly code: string

  constructor(status: number, body: ApiErrorBody) {
    super(body.message || body.error)
    this.status = status
    this.code = body.error
  }
}
