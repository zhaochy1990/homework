import { request } from './request'

export interface Me {
  id: number
  nickname: string
  avatarUrl: string
}

/** GET /me：当前用户；后端首次请求时 upsert users（api-contract §3.1）。 */
export function getMe(): Promise<Me> {
  return request<Me>({ url: '/me' })
}

export function patchMe(nickname: string): Promise<Me> {
  return request<Me>({ url: '/me', method: 'PATCH', data: { nickname } })
}
