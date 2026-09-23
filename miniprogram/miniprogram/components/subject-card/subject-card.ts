interface TodoItem {
  id: number
  content: string
  done: boolean
}

Component({
  options: { addGlobalClass: true },
  properties: {
    subject: { type: String, value: '' },
    /** 0 表示未估算（后端 estimatedMinutes 为 null）。 */
    estimatedMinutes: { type: Number, value: 0 },
    todos: { type: Array, value: [] },
    notes: { type: String, value: '' },
    checkedIn: { type: Boolean, value: false },
  },
  data: {
    checkedCount: 0,
    total: 0,
    allTicked: false,
  },
  observers: {
    todos(list: TodoItem[]) {
      const arr = list || []
      const checked = arr.filter((t) => t.done).length
      this.setData({
        checkedCount: checked,
        total: arr.length,
        allTicked: arr.length > 0 && checked === arr.length,
      })
    },
  },
  methods: {
    onToggle(e: WechatMiniprogram.CustomEvent) {
      this.triggerEvent('toggle', { id: e.currentTarget.dataset.id })
    },
    onCheckin() {
      this.triggerEvent('checkin')
    },
  },
})
