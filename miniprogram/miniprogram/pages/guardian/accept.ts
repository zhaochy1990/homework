import { acceptGuardianship } from '../../api/family'

// 监护人邀请接受页：从分享卡片 path 带入 token（api-contract §3.1）。
Component({
  data: {
    token: '',
    accepting: false,
    done: false,
    error: '',
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      const token = query.token ? decodeURIComponent(query.token) : ''
      if (!token) {
        this.setData({ error: '邀请链接无效或已过期' })
        return
      }
      this.setData({ token })
    },
    accept() {
      if (!this.data.token || this.data.accepting) {
        return
      }
      this.setData({ accepting: true })
      acceptGuardianship(this.data.token)
        .then(() => this.setData({ accepting: false, done: true }))
        .catch((err: unknown) =>
          this.setData({ accepting: false, error: err instanceof Error ? err.message : '接受失败' }),
        )
    },
    goMine() {
      wx.switchTab({ url: '/pages/mine/mine' })
    },
  },
})
