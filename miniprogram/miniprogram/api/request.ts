import { API_BASE_URL } from '../config'
import { ensureAccessToken, refreshTokens } from './auth'
import { ApiError, ApiErrorBody, ErrorCode } from './error'

export interface RequestOptions {
  /** 相对 API_BASE_URL 的路径，如 `/me`。 */
  url: string
  method?: 'GET' | 'POST' | 'PATCH' | 'PUT' | 'DELETE'
  data?: unknown
  /** 是否带 Bearer，默认 true。 */
  auth?: boolean
  /** 传 true/字符串则展示 loading（字符串作提示文案）。 */
  loading?: boolean | string
}

/** 只有这两种 401 值得刷新后重放；其余 401 直接抛给调用方。 */
const RETRYABLE_401: string[] = [ErrorCode.invalidToken, ErrorCode.unauthorized]

function wxRequest(options: WechatMiniprogram.RequestOption): Promise<WechatMiniprogram.RequestSuccessCallbackResult> {
  return new Promise((resolve, reject) => {
    wx.request({
      ...options,
      success: resolve,
      fail: (err) => reject(new Error(err.errMsg || '网络错误')),
    })
  })
}

// ponytail: 全局单一 loading，多个并发请求会互相 hide；T16 起按页面合并加载态。
function startLoading(loading: boolean | string | undefined): boolean {
  if (!loading) {
    return false
  }
  wx.showLoading({ title: typeof loading === 'string' ? loading : '加载中', mask: true })
  return true
}

async function send<T>(opts: RequestOptions, useAuth: boolean, retried: boolean): Promise<T> {
  const header: Record<string, string> = { 'content-type': 'application/json' }
  if (useAuth) {
    header.Authorization = 'Bearer ' + (await ensureAccessToken())
  }

  const res = await wxRequest({
    url: API_BASE_URL + opts.url,
    // 旧版 miniprogram-api-typings 的 method 联合类型缺 PATCH；运行时基础库支持。
    method: (opts.method || 'GET') as WechatMiniprogram.RequestOption['method'],
    header,
    data: opts.data as string | WechatMiniprogram.IAnyObject | ArrayBuffer | undefined,
    timeout: 15000,
  })

  if (res.statusCode >= 200 && res.statusCode < 300) {
    return res.data as T
  }

  const body = (res.data || {}) as Partial<ApiErrorBody>
  const err = new ApiError(res.statusCode, {
    error: body.error || 'internal_error',
    message: body.message || '请求失败',
  })

  if (useAuth && !retried && res.statusCode === 401 && RETRYABLE_401.indexOf(err.code) >= 0) {
    await refreshTokens()
    return send<T>(opts, useAuth, true)
  }
  throw err
}

/**
 * 下载需要鉴权的二进制资源（如班级邀请小程序码），返回本地临时文件路径。
 * 不能走 request()：<image>/previewImage 只认本地路径或公网 URL。
 */
export async function downloadAuthed(path: string): Promise<string> {
  const header: Record<string, string> = { Authorization: 'Bearer ' + (await ensureAccessToken()) }
  return new Promise((resolve, reject) => {
    wx.downloadFile({
      url: API_BASE_URL + path,
      header,
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          resolve(res.tempFilePath)
        } else {
          reject(new ApiError(res.statusCode, { error: 'download_failed', message: '下载失败' }))
        }
      },
      fail: (err) => reject(new Error(err.errMsg || '下载失败')),
    })
  })
}

/** 所有业务请求入口：拼 base URL、带 token、401 刷新重放、统一错误形状与 loading 约定。 */
export async function request<T>(opts: RequestOptions): Promise<T> {
  const useAuth = opts.auth !== false
  const showLoading = startLoading(opts.loading)
  try {
    return await send<T>(opts, useAuth, false)
  } finally {
    if (showLoading) {
      wx.hideLoading()
    }
  }
}
