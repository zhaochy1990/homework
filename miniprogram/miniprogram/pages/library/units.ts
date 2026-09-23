import { listClassTextbooks, Textbook } from '../../api/textbook'

// 单元列表：资料库 → 选教材 → 该教材的单元。
Component({
  data: {
    classId: 0,
    subject: '',
    textbook: null as Textbook | null,
    loading: false,
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      const classId = Number(query.classId) || 0
      const subject = query.subject ? decodeURIComponent(query.subject) : ''
      this.setData({ classId, subject })
      this.load()
    },
    load() {
      if (!this.data.classId || !this.data.subject) {
        return
      }
      this.setData({ loading: true })
      listClassTextbooks(this.data.classId)
        .then((list) => {
          const chosen = list.find((t) => t.subject === this.data.subject)
          const textbook = chosen ? chosen.textbook : null
          if (textbook) {
            wx.setNavigationBarTitle({ title: `${textbook.subject} · ${textbook.name}` })
          }
          this.setData({ textbook, loading: false })
        })
        .catch((err: unknown) => {
          this.setData({ loading: false })
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' })
        })
    },
    openUnit(e: WechatMiniprogram.BaseEvent) {
      const unitId = Number(e.currentTarget.dataset.id)
      const name = String(e.currentTarget.dataset.name || '')
      wx.navigateTo({
        url: `/pages/library/materials?classId=${this.data.classId}&unitId=${unitId}&unitName=${encodeURIComponent(name)}`,
      })
    },
  },
})
