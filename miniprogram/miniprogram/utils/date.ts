/** 本地日期（Asia/Shanghai 由设备时区决定，v1 假定设备在东八区）。 */
export function toDateString(d: Date): string {
  const p = (n: number) => (n < 10 ? '0' + n : '' + n)
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

export function todayString(): string {
  return toDateString(new Date())
}

/** 加 n 天（n 可为负）。 */
export function addDays(date: string, n: number): string {
  const [y, m, d] = date.split('-').map(Number)
  const dt = new Date(y, m - 1, d)
  dt.setDate(dt.getDate() + n)
  return toDateString(dt)
}
