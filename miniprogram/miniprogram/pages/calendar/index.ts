import { listSchoolCalendar, SchoolCalendarItem } from '../../api/calendar'
import { addDays, todayString } from '../../utils/date'

interface MonthGroup {
  month: string
  items: SchoolCalendarItem[]
}

// 上学日历：法定节假日与调休补班；verified=false 的记录不参与业务判定，前端明确标注。
Component({
  data: {
    months: [] as MonthGroup[],
    loading: false,
    error: '',
  },
  methods: {
    onLoad() {
      this.load()
    },
    load() {
      this.setData({ loading: true, error: '' })
      const from = todayString()
      listSchoolCalendar(from, addDays(from, 365))
        .then((items) => this.setData({ months: this.group(items), loading: false }))
        .catch((err: unknown) =>
          this.setData({ loading: false, error: err instanceof Error ? err.message : '加载失败' }),
        )
    },
    group(items: SchoolCalendarItem[]): MonthGroup[] {
      const months: MonthGroup[] = []
      items.forEach((item) => {
        const month = item.date.slice(0, 7)
        const last = months[months.length - 1]
        if (last && last.month === month) {
          last.items.push(item)
        } else {
          months.push({ month, items: [item] })
        }
      })
      return months
    },
    retry() {
      this.load()
    },
  },
})
