import { request } from './request'

export interface SchoolCalendarItem {
  date: string
  /** holiday=法定节假日；其余为调休补班等（api-contract §3.9）。 */
  type: string
  name: string
  /** 未经人工核验的记录不参与业务判定，前端也标注出来。 */
  verified: boolean
}

export function listSchoolCalendar(from: string, to: string): Promise<SchoolCalendarItem[]> {
  return request<{ items: SchoolCalendarItem[] }>({
    url: `/school-calendar?from=${from}&to=${to}`,
  }).then((r) => r.items)
}
