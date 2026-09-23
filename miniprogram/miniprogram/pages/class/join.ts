import { ClassInfo, joinClass, searchPublicClasses } from '../../api/class'
import { parseInviteToken, sceneFromPath } from '../../utils/invite'

// 加入班级：搜索公开班 / 输入邀请口令 / 扫码（scene=`{classId}-{inviteCode}`）。
Component({
  data: {
    keyword: '',
    results: [] as ClassInfo[],
    searched: false,
    searching: false,
    code: '',
    joining: false,
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      if (!query.scene) {
        return
      }
      let raw = query.scene
      try {
        raw = decodeURIComponent(query.scene)
      } catch {
        // 保留原始 scene。
      }
      const token = parseInviteToken(raw)
      if (token) {
        this.doJoin(token.classId, token.inviteCode)
      } else {
        wx.showToast({ title: '邀请码无效', icon: 'none' })
      }
    },
    onKeyword(e: WechatMiniprogram.Input) {
      this.setData({ keyword: e.detail.value })
    },
    onCode(e: WechatMiniprogram.Input) {
      this.setData({ code: e.detail.value })
    },
    search() {
      const keyword = this.data.keyword.trim()
      if (this.data.searching) {
        return
      }
      this.setData({ searching: true })
      searchPublicClasses(keyword)
        .then((results) => this.setData({ results, searched: true, searching: false }))
        .catch((err: unknown) => {
          this.setData({ searching: false })
          wx.showToast({ title: err instanceof Error ? err.message : '搜索失败', icon: 'none' })
        })
    },
    joinPublic(e: WechatMiniprogram.BaseEvent) {
      this.doJoin(Number(e.currentTarget.dataset.id), undefined)
    },
    joinByCode() {
      const token = parseInviteToken(this.data.code)
      if (!token) {
        wx.showToast({ title: '口令形如 12-abcd1234', icon: 'none' })
        return
      }
      this.doJoin(token.classId, token.inviteCode)
    },
    scan() {
      wx.scanCode({
        success: (res) => {
          const token = parseInviteToken(sceneFromPath(res.path) || res.result)
          if (!token) {
            wx.showToast({ title: '不是班级邀请码', icon: 'none' })
            return
          }
          this.doJoin(token.classId, token.inviteCode)
        },
        fail: () => undefined,
      })
    },
    doJoin(classId: number, inviteCode?: string) {
      if (this.data.joining) {
        return
      }
      this.setData({ joining: true })
      joinClass(classId, inviteCode)
        .then((res) => {
          this.setData({ joining: false })
          if (res.status === 'approved') {
            wx.showToast({ title: '已加入班级', icon: 'success' })
            setTimeout(() => wx.navigateBack(), 700)
          } else {
            wx.showModal({
              title: '已提交申请',
              content: '管理员同意后即可加入该班级。',
              showCancel: false,
              success: () => wx.navigateBack(),
            })
          }
        })
        .catch((err: unknown) => {
          this.setData({ joining: false })
          wx.showToast({ title: err instanceof Error ? err.message : '加入失败', icon: 'none' })
        })
    },
  },
})
