/** 并发去重：同一时刻只执行一次 fn，进行中的调用复用同一个 Promise，结束后可再次执行。 */
export function singleFlight<T>(fn: () => Promise<T>): () => Promise<T> {
  let inflight: Promise<T> | null = null
  return () => {
    if (inflight === null) {
      inflight = fn().finally(() => {
        inflight = null
      })
    }
    return inflight
  }
}
