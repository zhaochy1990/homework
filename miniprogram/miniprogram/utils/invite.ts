/** 班级邀请口令：`{classId}-{inviteCode}`（api-contract §3.2 邀请小程序码 scene 同构）。 */
export interface InviteToken {
  classId: number
  inviteCode: string
}

/** 解析管理员分享的邀请口令或扫码得到的 scene；非法返回 null。 */
export function parseInviteToken(raw: string): InviteToken | null {
  const text = (raw || '').trim()
  const m = /^(\d+)-(.+)$/.exec(text)
  if (!m) {
    return null
  }
  const classId = Number(m[1])
  if (!classId || !m[2].trim()) {
    return null
  }
  return { classId, inviteCode: m[2].trim() }
}

/** 从扫码结果的 path（如 `pages/class/join?scene=12-abcd1234`）里取 scene。 */
export function sceneFromPath(path: string): string {
  const m = /[?&]scene=([^&]+)/.exec(path || '')
  return m ? decodeURIComponent(m[1]) : ''
}
