import { Child, createGuardianInvite, GuardianInvite, listChildren, patchChild, quitGuardianship } from '../../api/family'

// 孩子管理：改名 / 监护人邀请（分享卡片入 accept 页）/ 退出监护。
Component({
  data: {
    id: 0,
    child: null as Child | null,
    name: '',
    invite: null as GuardianInvite | null,
    loading: false,
    error: '',
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      this.setData({ id: Number(query.id) || 0 })
    },
    onShow() {
      this.load()
    },
    load() {
      if (!this.data.id || this.data.loading) {
        return
      }
      this.setData({ loading: true, error: '' })
      listChildren()
        .then((children) => {
          const child = children.filter((c) => c.id === this.data.id)[0]
          if (!child) {
            this.setData({ loading: false, error: '孩子不存在或你不是监护人' })
            return
          }
          this.setData({ child, name: child.name, loading: false })
          // 邀请 token 失败不阻塞孩子管理本身。
          createGuardianInvite(child.id)
            .then((invite) => this.setData({ invite }))
            .catch(() => undefined)
        })
        .catch((err: unknown) =>
          this.setData({ loading: false, error: err instanceof Error ? err.message : '加载失败' }),
        )
    },
    retry() {
      this.load()
    },
    onName(e: WechatMiniprogram.Input) {
      this.setData({ name: e.detail.value })
    },
    saveName() {
      const name = this.data.name.trim()
      if (!name || !this.data.child || name === this.data.child.name) {
        return
      }
      patchChild(this.data.child.id, name)
        .then((child) => {
          this.setData({ child })
          wx.showToast({ title: '已保存', icon: 'success' })
        })
        .catch((err: unknown) =>
          wx.showToast({ title: err instanceof Error ? err.message : '保存失败', icon: 'none' }),
        )
    },
    onShareAppMessage() {
      const child = this.data.child
      const token = this.data.invite ? this.data.invite.inviteToken : ''
      return {
        title: child ? `邀请你一起管理「${child.name}」的作业打卡` : '作业打卡 · 监护人邀请',
        path: token ? `pages/guardian/accept?token=${encodeURIComponent(token)}` : 'pages/mine/mine',
      }
    },
    quit() {
      if (!this.data.child) {
        return
      }
      wx.showModal({
        title: '退出监护',
        content: `退出后将不再能查看「${this.data.child.name}」的作业与打卡。确定退出？`,
        confirmColor: '#fa5151',
        success: (res) => {
          if (res.confirm && this.data.child) {
            quitGuardianship(this.data.child.id)
              .then(() => {
                wx.showToast({ title: '已退出', icon: 'success' })
                setTimeout(() => wx.navigateBack(), 600)
              })
              .catch((err: unknown) =>
                wx.showToast({ title: err instanceof Error ? err.message : '退出失败', icon: 'none' }),
              )
          }
        },
      })
    },
  },
})
