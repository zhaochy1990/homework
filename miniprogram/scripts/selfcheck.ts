import assert from 'node:assert'
import { singleFlight } from '../miniprogram/utils/async.ts'
import { parseInviteToken, sceneFromPath } from '../miniprogram/utils/invite.ts'
import { addDays, toDateString } from '../miniprogram/utils/date.ts'

// 一次性 refresh_token 的轮换：多个请求同时 401 时必须复用同一次刷新，否则并发刷新会互相吊销。
let calls = 0
const resolvers: Array<() => void> = []
const run = singleFlight(async () => {
  calls += 1
  await new Promise<void>((resolve) => resolvers.push(resolve))
  return calls
})

const a = run()
const b = run()
assert.strictEqual(a, b, '并发调用应复用同一 in-flight Promise')
resolvers.forEach((resolve) => resolve())
assert.strictEqual(await a, 1)
assert.strictEqual(await b, 1)
assert.strictEqual(calls, 1, '并发只执行一次')

const c = run()
resolvers.forEach((resolve) => resolve())
assert.strictEqual(await c, 2, '结束后可再次执行')

// 失败不应被缓存，下次调用重新执行。
let attempts = 0
const boom = singleFlight(async () => {
  attempts += 1
  throw new Error('网络错误')
})
await assert.rejects(boom())
await assert.rejects(boom())
assert.strictEqual(attempts, 2, '失败不应被缓存')

console.log('utils/async selfcheck ok')

// 班级邀请口令与扫码 scene 解析（T19）。
assert.deepStrictEqual(parseInviteToken(' 12-abcd1234 '), { classId: 12, inviteCode: 'abcd1234' })
assert.strictEqual(parseInviteToken('abc'), null, '无分隔符应拒绝')
assert.strictEqual(parseInviteToken('0-abcd'), null, 'classId 为 0 应拒绝')
assert.strictEqual(parseInviteToken('12-'), null, '空邀请码应拒绝')
assert.strictEqual(sceneFromPath('pages/class/join?scene=12-abcd1234'), '12-abcd1234')
assert.strictEqual(sceneFromPath('pages/class/join?a=1&scene=7-xy%3Dz'), '7-xy=z')
assert.strictEqual(sceneFromPath('https://example.com/x'), '')

// 日期工具。
assert.strictEqual(addDays('2026-01-01', -1), '2025-12-31')
assert.strictEqual(addDays('2026-02-28', 1), '2026-03-01')
assert.strictEqual(toDateString(new Date(2026, 8, 23)), '2026-09-23')

console.log('utils/invite + date selfcheck ok')
