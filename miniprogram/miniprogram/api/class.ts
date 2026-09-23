import { downloadAuthed, request } from './request'
import { Child } from './family'

export type ClassVisibility = 'public' | 'private'
export type ClassRole = 'admin' | 'member'

export interface ClassInfo {
  id: number
  name: string
  visibility: ClassVisibility
  joinApproval: boolean
  createdBy: number
}

export interface MyClass extends ClassInfo {
  role: ClassRole
}

export interface ClassDetail extends ClassInfo {
  myRole: ClassRole
  myChildren: Child[]
  /** 仅 admin 返回：邀请口令 = `${id}-${inviteCode}`。 */
  inviteCode?: string
}

export interface ClassMember {
  userId: number
  nickname: string
  avatarUrl: string
  role: ClassRole
}

export interface JoinRequest {
  id: number
  userId: number
  nickname: string
  avatarUrl: string
  status: string
  createdAt: string
}

export interface JoinResult {
  status: 'approved' | 'pending'
  requestId?: number
}

export function createClass(input: {
  name: string
  visibility: ClassVisibility
  joinApproval: boolean
}): Promise<ClassInfo> {
  return request<ClassInfo>({ url: '/classes', method: 'POST', data: input, loading: '创建中' })
}

export function listMyClasses(): Promise<MyClass[]> {
  return request<{ items: MyClass[] }>({ url: '/classes/my' }).then((r) => r.items)
}

export function searchPublicClasses(keyword: string): Promise<ClassInfo[]> {
  return request<{ items: ClassInfo[] }>({
    url: '/classes/public?keyword=' + encodeURIComponent(keyword),
  }).then((r) => r.items)
}

export function getClass(classId: number): Promise<ClassDetail> {
  return request<ClassDetail>({ url: `/classes/${classId}` })
}

export function patchClass(
  classId: number,
  input: { name?: string; visibility?: ClassVisibility; joinApproval?: boolean },
): Promise<ClassInfo> {
  return request<ClassInfo>({ url: `/classes/${classId}`, method: 'PATCH', data: input })
}

/** 三分支入班（公开直入 / 公开待审批 / 私密凭邀请码直入）。 */
export function joinClass(classId: number, inviteCode?: string): Promise<JoinResult> {
  return request<JoinResult>({
    url: '/join-requests',
    method: 'POST',
    data: { classId, inviteCode },
    loading: '加入中',
  })
}

export function listJoinRequests(classId: number): Promise<JoinRequest[]> {
  return request<{ items: JoinRequest[] }>({
    url: `/classes/${classId}/join-requests?status=pending`,
  }).then((r) => r.items)
}

export function decideJoinRequest(requestId: number, approve: boolean): Promise<void> {
  const action = approve ? 'approve' : 'reject'
  return request<void>({ url: `/join-requests/${requestId}/${action}`, method: 'POST' })
}

export function listMembers(classId: number): Promise<ClassMember[]> {
  return request<{ items: ClassMember[] }>({
    url: `/classes/${classId}/members?page_size=100`,
  }).then((r) => r.items)
}

export function removeMember(classId: number, userId: number): Promise<void> {
  return request<void>({ url: `/classes/${classId}/members/${userId}`, method: 'DELETE' })
}

export function setMemberRole(
  classId: number,
  userId: number,
  role: ClassRole,
): Promise<void> {
  const action = role === 'admin' ? 'promote' : 'demote'
  return request<void>({ url: `/classes/${classId}/members/${userId}/${action}`, method: 'POST' })
}

export function quitClass(classId: number): Promise<void> {
  return request<void>({ url: `/classes/${classId}/members/me`, method: 'DELETE' })
}

export function dissolveClass(classId: number): Promise<void> {
  return request<void>({ url: `/classes/${classId}`, method: 'DELETE', loading: '解散中' })
}

/** 邀请小程序码：page=pages/class/join、scene=`{classId}-{inviteCode}`。 */
export function getInviteQRCode(classId: number): Promise<string> {
  return downloadAuthed(`/classes/${classId}/invite-qrcode`)
}

export function listClassChildren(classId: number): Promise<Child[]> {
  return request<{ items: Child[] }>({
    url: `/classes/${classId}/children?page_size=100`,
  }).then((r) => r.items)
}

export function enrollChild(childId: number, classId: number): Promise<void> {
  return request<void>({
    url: `/children/${childId}/enrollments`,
    method: 'POST',
    data: { classId },
    loading: '报班中',
  })
}

export function unenrollChild(childId: number, classId: number): Promise<void> {
  return request<void>({ url: `/children/${childId}/enrollments/${classId}`, method: 'DELETE' })
}
