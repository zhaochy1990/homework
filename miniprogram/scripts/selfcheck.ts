import assert from 'node:assert'
import { singleFlight } from '../miniprogram/utils/async.ts'

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
