import { Child, listChildren } from '../../api/family'
import { listMyClasses, MyClass } from '../../api/class'
import { getMe, Me } from '../../api/user'

// 我的页：个人资料 + 我的班级 / 我的孩子（T19）。班级详情四页签在 class/detail。
Component({
  data: {
    me: null as Me | null,
    classes: [] as MyClass[],
    children: [] as Child[],
    loading: false,
    error: '',
  },
  pageLifetimes: {
    show() {
      this.load()
    },
  },
  methods: {
    load() {
      if (this.data.loading) {
        return
      }
      this.setData({ loading: true, error: '' })
      Promise.all([getMe(), listMyClasses(), listChildren()])
        .then(([me, classes, children]) => this.setData({ me, classes, children, loading: false }))
        .catch((err: unknown) =>
          this.setData({ loading: false, error: err instanceof Error ? err.message : '加载失败' }),
        )
    },
    retry() {
      this.load()
    },
    goClass(e: WechatMiniprogram.BaseEvent) {
      wx.navigateTo({ url: '../class/detail?id=' + e.currentTarget.dataset.id })
    },
    goCreateClass() {
      wx.navigateTo({ url: '../class/create' })
    },
    goJoinClass() {
      wx.navigateTo({ url: '../class/join' })
    },
    goChild(e: WechatMiniprogram.BaseEvent) {
      wx.navigateTo({ url: '../child/detail?id=' + e.currentTarget.dataset.id })
    },
    goCreateChild() {
      wx.navigateTo({ url: '../child/create' })
    },
    goCalendar() {
      wx.navigateTo({ url: '../calendar/index' })
    },
    goSettings() {
      wx.navigateTo({ url: '../settings/index' })
    },
    goLibrary() {
      wx.switchTab({ url: '/pages/library/library' })
    },
    inviteGuardian() {
      const children = this.data.children
      if (children.length === 0) {
        wx.showToast({ title: '先添加孩子', icon: 'none' })
        return
      }
      if (children.length === 1) {
        wx.navigateTo({ url: '../child/detail?id=' + children[0].id + '&invite=1' })
        return
      }
      wx.showActionSheet({
        itemList: children.map((c) => c.name),
        success: (res) => {
          wx.navigateTo({ url: '../child/detail?id=' + children[res.tapIndex].id + '&invite=1' })
        },
      })
    },
  },
})
