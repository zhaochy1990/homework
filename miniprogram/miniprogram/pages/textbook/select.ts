import { createTextbook, searchTextbooks, setClassTextbook, Textbook } from '../../api/textbook'

// 班级按科目选用 / 更换教材，并可向全局库补充新教材（v1 无审核）。
Component({
  data: {
    classId: 0,
    subject: '',
    keyword: '',
    items: [] as Textbook[],
    loading: false,
    creating: false,
    name: '',
    grade: '',
    term: '',
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      const subject = query.subject ? decodeURIComponent(query.subject) : ''
      this.setData({ classId: Number(query.classId) || 0, subject })
      this.search()
    },
    onKeyword(e: WechatMiniprogram.Input) {
      this.setData({ keyword: e.detail.value })
    },
    search() {
      if (!this.data.subject || this.data.loading) {
        return
      }
      this.setData({ loading: true })
      searchTextbooks(this.data.subject, this.data.keyword.trim())
        .then((items) => this.setData({ items, loading: false }))
        .catch((err: unknown) => {
          this.setData({ loading: false })
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' })
        })
    },
    choose(e: WechatMiniprogram.BaseEvent) {
      const textbookId = Number(e.currentTarget.dataset.id)
      setClassTextbook(this.data.classId, this.data.subject, textbookId)
        .then(() => {
          wx.showToast({ title: '已选用', icon: 'success' })
          setTimeout(() => wx.navigateBack(), 600)
        })
        .catch((err: unknown) =>
          wx.showToast({ title: err instanceof Error ? err.message : '保存失败', icon: 'none' }),
        )
    },
    toggleCreate() {
      this.setData({ creating: !this.data.creating })
    },
    onName(e: WechatMiniprogram.Input) {
      this.setData({ name: e.detail.value })
    },
    onGrade(e: WechatMiniprogram.Input) {
      this.setData({ grade: e.detail.value })
    },
    onTerm(e: WechatMiniprogram.Input) {
      this.setData({ term: e.detail.value })
    },
    create() {
      const name = this.data.name.trim()
      if (!name) {
        wx.showToast({ title: '请填写教材名', icon: 'none' })
        return
      }
      createTextbook({ subject: this.data.subject, name, grade: this.data.grade.trim(), term: this.data.term.trim() })
        .then((t) => setClassTextbook(this.data.classId, this.data.subject, t.id))
        .then(() => {
          wx.showToast({ title: '已创建并选用', icon: 'success' })
          setTimeout(() => wx.navigateBack(), 700)
        })
        .catch((err: unknown) =>
          wx.showToast({ title: err instanceof Error ? err.message : '创建失败', icon: 'none' }),
        )
    },
  },
})
