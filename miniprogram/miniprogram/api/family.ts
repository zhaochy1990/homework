import { request } from './request'

export interface Child {
  id: number
  name: string
  avatarUrl: string
}

export interface GuardianInvite {
  inviteToken: string
  /** UTC ISO8601。 */
  expiresAt: string
}

/** GET /children：我的孩子（api-contract §3.1）。 */
export function listChildren(): Promise<Child[]> {
  return request<{ items: Child[] }>({ url: '/children' }).then((r) => r.items)
}

export function createChild(name: string): Promise<Child> {
  return request<Child>({ url: '/children', method: 'POST', data: { name }, loading: '创建中' })
}

export function patchChild(childId: number, name: string): Promise<Child> {
  return request<Child>({ url: `/children/${childId}`, method: 'PATCH', data: { name } })
}

/** 签发 72h 监护人邀请 token（分享卡片入 accept 页）。 */
export function createGuardianInvite(childId: number): Promise<GuardianInvite> {
  return request<GuardianInvite>({ url: `/children/${childId}/guardian-invites`, method: 'POST' })
}

export function acceptGuardianship(inviteToken: string): Promise<Child> {
  return request<Child>({
    url: '/guardianships/accept',
    method: 'POST',
    data: { inviteToken },
    loading: '处理中',
  })
}

export function quitGuardianship(childId: number): Promise<void> {
  return request<void>({ url: `/children/${childId}/guardians/me`, method: 'DELETE' })
}
