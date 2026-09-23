import { createChild } from '../../api/family'

// 添加孩子：创建者自动成为监护人（api-contract §3.1）。
Component({
  data: { name: '', submitting: false },
  methods: {
    onName(e: WechatMiniprogram.Input) {
      this.setData({ name: e.detail.value })
    },
    submit() {
      const name = this.data.name.trim()
      if (!name) {
        wx.showToast({ title: '请填写孩子姓名', icon: 'none' })
        return
      }
      if (this.data.submitting) {
        return
      }
      this.setData({ submitting: true })
      createChild(name)
        .then(() => {
          wx.showToast({ title: '已添加', icon: 'success' })
          setTimeout(() => wx.navigateBack(), 600)
        })
        .catch((err: unknown) => {
          this.setData({ submitting: false })
          wx.showToast({ title: err instanceof Error ? err.message : '添加失败', icon: 'none' })
        })
    },
  },
})
