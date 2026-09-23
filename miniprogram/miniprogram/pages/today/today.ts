interface TodoItem {
  id: number
  content: string
  done: boolean
}

// T15 只搭地基：这里用静态样例驱动基础组件，T16 换成孩子当日聚合接口的真实数据。
Component({
  data: {
    demo: {
      subject: '语文',
      estimatedMinutes: 35,
      notes: '明天带《快乐读书吧》',
      checkedIn: false,
      todos: [
        { id: 1, content: '朗读课文第 3 课', done: true },
        { id: 2, content: '抄写生字并组词', done: false },
      ] as TodoItem[],
    },
  },
  methods: {
    onToggle(e: WechatMiniprogram.CustomEvent) {
      const id = e.detail.id as number
      const todos = this.data.demo.todos.map((t) => (t.id === id ? { ...t, done: !t.done } : t))
      this.setData({ 'demo.todos': todos })
    },
    onCheckin() {
      wx.showToast({ title: '打卡链路待 T16 接入', icon: 'none' })
    },
  },
})
