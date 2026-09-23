import { listMyClasses, MyClass } from '../../api/class'
import { ClassTextbook, listClassTextbooks } from '../../api/textbook'

// 资料库 Tab：班级切换 → 按科目列教材卡 →（进 unit 列表）→ 单元资料。
// 资料先审后显，pending/blocked 由后端过滤，前端只需展示。
Component({
  data: {
    classes: [] as MyClass[],
    classId: 0,
    textbooks: [] as ClassTextbook[],
    loading: false,
  },
  pageLifetimes: {
    show() {
      this.load()
    },
  },
  methods: {
    load() {
      listMyClasses()
        .then((classes) => {
          const keep =
            classes.some((c) => c.id === this.data.classId) || classes.length === 0
              ? this.data.classId
              : classes[0].id
          this.setData({ classes, classId: keep })
          this.loadTextbooks(keep)
        })
        .catch((err: unknown) => {
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' })
        })
    },
    loadTextbooks(classId: number) {
      if (!classId) {
        this.setData({ textbooks: [], loading: false })
        return
      }
      this.setData({ loading: true })
      listClassTextbooks(classId)
        .then((textbooks) => this.setData({ textbooks, loading: false }))
        .catch((err: unknown) => {
          this.setData({ loading: false })
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' })
        })
    },
    switchClass(e: WechatMiniprogram.BaseEvent) {
      const classId = Number(e.currentTarget.dataset.id)
      if (!classId || classId === this.data.classId) {
        return
      }
      this.setData({ classId })
      this.loadTextbooks(classId)
    },
    openUnits(e: WechatMiniprogram.BaseEvent) {
      const subject = String(e.currentTarget.dataset.subject || '')
      wx.navigateTo({
        url: `/pages/library/units?classId=${this.data.classId}&subject=${encodeURIComponent(subject)}`,
      })
    },
    goSelect() {
      wx.navigateTo({ url: `/pages/class/detail?id=${this.data.classId}` })
    },
  },
})
