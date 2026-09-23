import { createClass } from '../../api/class'

// 建班：创建者自动成为 admin（api-contract §3.2）。
Component({
  data: {
    name: '',
    visibility: 'public' as 'public' | 'private',
    joinApproval: false,
    submitting: false,
  },
  methods: {
    onName(e: WechatMiniprogram.Input) {
      this.setData({ name: e.detail.value })
    },
    onVisibility(e: WechatMiniprogram.RadioGroupChange) {
      const visibility = e.detail.value === 'private' ? 'private' : 'public'
      this.setData({ visibility, joinApproval: visibility === 'private' ? false : this.data.joinApproval })
    },
    onApproval(e: WechatMiniprogram.SwitchChange) {
      this.setData({ joinApproval: e.detail.value })
    },
    submit() {
      const name = this.data.name.trim()
      if (!name) {
        wx.showToast({ title: '请填写班级名称', icon: 'none' })
        return
      }
      if (this.data.submitting) {
        return
      }
      this.setData({ submitting: true })
      createClass({ name, visibility: this.data.visibility, joinApproval: this.data.joinApproval })
        .then(() => {
          wx.showToast({ title: '已创建', icon: 'success' })
          setTimeout(() => wx.navigateBack(), 600)
        })
        .catch((err: unknown) => {
          this.setData({ submitting: false })
          wx.showToast({ title: err instanceof Error ? err.message : '创建失败', icon: 'none' })
        })
    },
  },
})
