import { AUTH_BASE_URL, CLIENT_ID } from '../config'
import { singleFlight } from '../utils/async'
import { ApiError, ApiErrorBody, ErrorCode } from './error'

const TOKEN_KEY = 'auth_tokens'
// access_token 有效期 1h；剩余不足 60s 视为过期，提前刷新。
const EXPIRY_SKEW_MS = 60 * 1000

export interface Tokens {
  accessToken: string
  refreshToken: string
  /** epoch 毫秒 */
  expiresAt: number
}

interface TokenResponse {
  access_token: string
  refresh_token: string
  expires_in: number
}

/** 读本地会话；没有或结构不完整返回 null。 */
export function getTokens(): Tokens | null {
  const raw = wx.getStorageSync(TOKEN_KEY)
  if (!raw || typeof raw !== 'object') {
    return null
  }
  const t = raw as Partial<Tokens>
  if (typeof t.accessToken !== 'string' || typeof t.refreshToken !== 'string' || typeof t.expiresAt !== 'number') {
    return null
  }
  return { accessToken: t.accessToken, refreshToken: t.refreshToken, expiresAt: t.expiresAt }
}

export function clearTokens(): void {
  wx.removeStorageSync(TOKEN_KEY)
}

function saveTokens(res: TokenResponse): Tokens {
  const tokens: Tokens = {
    accessToken: res.access_token,
    refreshToken: res.refresh_token,
    expiresAt: Date.now() + res.expires_in * 1000,
  }
  wx.setStorageSync(TOKEN_KEY, tokens)
  return tokens
}

function loginCode(): Promise<string> {
  return new Promise((resolve, reject) => {
    wx.login({
      success: (res) => {
        if (res.code) {
          resolve(res.code)
        } else {
          reject(new Error('wx.login 未返回 code'))
        }
      },
      fail: (err) => reject(new Error(err.errMsg || 'wx.login 失败')),
    })
  })
}

function encodeForm(fields: Record<string, string>): string {
  return Object.keys(fields)
    .map((k) => encodeURIComponent(k) + '=' + encodeURIComponent(fields[k]))
    .join('&')
}

function authRequest<T>(path: string, data: string, header: Record<string, string>): Promise<T> {
  return new Promise((resolve, reject) => {
    wx.request({
      url: AUTH_BASE_URL + path,
      method: 'POST',
      data,
      header,
      timeout: 15000,
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.data as T)
        } else {
          const body = (res.data || {}) as Partial<ApiErrorBody>
          reject(
            new ApiError(res.statusCode, {
              error: body.error || 'internal_error',
              message: body.message || '认证服务错误',
            }),
          )
        }
      },
      fail: (err) => reject(new Error(err.errMsg || '网络错误')),
    })
  })
}

/** 微信免密登录：wx.login code → token_exchange（RFC 8693）。 */
export async function login(): Promise<Tokens> {
  const code = await loginCode()
  const res = await authRequest<TokenResponse>(
    '/oauth/token',
    encodeForm({
      grant_type: 'token_exchange',
      client_id: CLIENT_ID,
      subject_token: code,
      subject_token_type: 'wechat_mini_program',
    }),
    { 'content-type': 'application/x-www-form-urlencoded' },
  )
  return saveTokens(res)
}

async function doRefresh(): Promise<Tokens> {
  const tokens = getTokens()
  if (!tokens) {
    throw new ApiError(401, { error: ErrorCode.unauthorized, message: '登录已失效' })
  }
  try {
    const res = await authRequest<TokenResponse>(
      '/api/auth/refresh',
      JSON.stringify({ refresh_token: tokens.refreshToken }),
      { 'content-type': 'application/json', 'X-Client-Id': CLIENT_ID },
    )
    return saveTokens(res)
  } catch (err) {
    // refresh_token 一次性轮换：已撤销/过期即会话终结，清空本地让上层重新登录。
    if (err instanceof ApiError && err.status === 401) {
      clearTokens()
    }
    throw err
  }
}

/** 并发去重的刷新：多个请求同时 401 时只轮换一次。 */
export const refreshTokens = singleFlight(doRefresh)

/** 本地有效（未临近过期）则直接返回 accessToken，否则 null。 */
export function cachedAccessToken(): string | null {
  const tokens = getTokens()
  if (!tokens || tokens.expiresAt - Date.now() <= EXPIRY_SKEW_MS) {
    return null
  }
  return tokens.accessToken
}

/** 保证有可用的 accessToken：优先本地 → refresh → 重新登录。 */
export async function ensureAccessToken(): Promise<string> {
  const cached = cachedAccessToken()
  if (cached) {
    return cached
  }
  if (getTokens()) {
    try {
      return (await refreshTokens()).accessToken
    } catch (err) {
      console.warn('[auth] refresh 失败，回退重新登录', err)
    }
  }
  return (await login()).accessToken
}
