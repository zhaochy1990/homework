import {
  ClassDetail,
  ClassMember,
  ClassRole,
  decideJoinRequest,
  dissolveClass,
  enrollChild,
  getClass,
  getInviteQRCode,
  JoinRequest,
  listJoinRequests,
  listMembers,
  quitClass,
  removeMember,
  setMemberRole,
  unenrollChild,
} from '../../api/class'
import { Child, listChildren } from '../../api/family'
import { ClassTextbook, listClassTextbooks, SUBJECTS } from '../../api/textbook'
import { getMe } from '../../api/user'

type Tab = 'hw' | 'mat' | 'mem' | 'tb'

// 班级详情：作业 / 资料 / 成员 / 教材 四页签（T19）。
// 作业页签随 T20、资料页签随 T18 接入，这里保留结构与跳转。
Component({
  data: {
    id: 0,
    detail: null as ClassDetail | null,
    tab: 'mem' as Tab,
    myId: 0,
    isAdmin: false,
    isCreator: false,
    members: [] as ClassMember[],
    myChildren: [] as Child[],
    availableChildren: [] as Child[],
    classTextbooks: [] as ClassTextbook[],
    joinRequests: [] as JoinRequest[],
    subjects: SUBJECTS,
    inviteCode: '',
    qrPath: '',
    qrVisible: false,
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
      const id = this.data.id
      Promise.all([getClass(id), listMembers(id), listClassTextbooks(id), listChildren(), getMe()])
        .then(([detail, members, classTextbooks, children, me]) => {
          const inClass = detail.myChildren.map((c) => c.id)
          this.setData({
            detail,
            members,
            classTextbooks,
            myChildren: detail.myChildren,
            availableChildren: children.filter((c) => inClass.indexOf(c.id) < 0),
            myId: me.id,
            isAdmin: detail.myRole === 'admin',
            isCreator: detail.createdBy === me.id,
            inviteCode: detail.inviteCode ? detail.id + '-' + detail.inviteCode : '',
            loading: false,
          })
        })
        .catch((err: unknown) =>
          this.setData({ loading: false, error: err instanceof Error ? err.message : '加载失败' }),
        )
    },
    retry() {
      this.load()
    },
    switchTab(e: WechatMiniprogram.BaseEvent) {
      this.setData({ tab: e.currentTarget.dataset.tab as Tab })
    },
    goLibrary() {
      wx.switchTab({ url: '/pages/library/library' })
    },
    goHomework() {
      wx.showToast({ title: '发作业流随 T20 上线', icon: 'none' })
    },

    // ---- 孩子入班 ----
    enroll(e: WechatMiniprogram.BaseEvent) {
      const childId = Number(e.currentTarget.dataset.id)
      enrollChild(childId, this.data.id)
        .then(() => {
          wx.showToast({ title: '已报班', icon: 'success' })
          this.load()
        })
        .catch((err: unknown) => wx.showToast({ title: err instanceof Error ? err.message : '报班失败', icon: 'none' }))
    },
    unenroll(e: WechatMiniprogram.BaseEvent) {
      const childId = Number(e.currentTarget.dataset.id)
      wx.showModal({
        title: '退班',
        content: '孩子的勾选与打卡历史会保留，但不再展示。确定退班？',
        success: (res) => {
          if (!res.confirm) {
            return
          }
          unenrollChild(childId, this.data.id)
            .then(() => this.load())
            .catch((err: unknown) =>
              wx.showToast({ title: err instanceof Error ? err.message : '退班失败', icon: 'none' }),
            )
        },
      })
    },

    // ---- 成员管理 ----
    memberTap(e: WechatMiniprogram.BaseEvent) {
      const userId = Number(e.currentTarget.dataset.id)
      const role = e.currentTarget.dataset.role as ClassRole
      if (userId === this.data.myId) {
        return
      }
      const items: string[] = []
      const actions: Array<() => void> = []
      if (this.data.isAdmin && role !== 'admin') {
        items.push('移出班级')
        actions.push(() => this.mutate(() => removeMember(this.data.id, userId)))
      }
      if (this.data.isCreator) {
        if (role === 'admin') {
          items.push('取消管理员')
          actions.push(() => this.mutate(() => setMemberRole(this.data.id, userId, 'member')))
        } else {
          items.push('设为管理员')
          actions.push(() => this.mutate(() => setMemberRole(this.data.id, userId, 'admin')))
        }
      }
      if (!items.length) {
        return
      }
      wx.showActionSheet({ itemList: items, success: (res) => actions[res.tapIndex]() })
    },
    mutate(run: () => Promise<void>): Promise<void> {
      return run()
        .then(() => this.load())
        .catch((err: unknown) => {
          wx.showToast({ title: err instanceof Error ? err.message : '操作失败', icon: 'none' })
        })
    },
    loadRequests() {
      listJoinRequests(this.data.id)
        .then((joinRequests) => {
          this.setData({ joinRequests })
          if (!joinRequests.length) {
            wx.showToast({ title: '暂无待审批', icon: 'none' })
          }
        })
        .catch((err: unknown) =>
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' }),
        )
    },
    approveRequest(e: WechatMiniprogram.BaseEvent) {
      const requestId = Number(e.currentTarget.dataset.id)
      this.mutate(() => decideJoinRequest(requestId, true)).then(() => this.loadRequests())
    },
    rejectRequest(e: WechatMiniprogram.BaseEvent) {
      const requestId = Number(e.currentTarget.dataset.id)
      this.mutate(() => decideJoinRequest(requestId, false)).then(() => this.loadRequests())
    },

    // ---- 邀请码 / 二维码 ----
    showQR() {
      getInviteQRCode(this.data.id)
        .then((qrPath) => this.setData({ qrPath, qrVisible: true }))
        .catch((err: unknown) =>
          wx.showToast({ title: err instanceof Error ? err.message : '生成失败', icon: 'none' }),
        )
    },
    hideQR() {
      this.setData({ qrVisible: false })
    },
    noop() {
      // 阻止遮罩点击穿透。
      return undefined
    },
    copyCode() {
      wx.setClipboardData({ data: this.data.inviteCode })
    },
    previewQR() {
      if (this.data.qrPath) {
        wx.previewImage({ urls: [this.data.qrPath] })
      }
    },

    // ---- 教材 ----
    goSelectTextbook(e: WechatMiniprogram.BaseEvent) {
      const subject = e.currentTarget.dataset.subject as string
      wx.navigateTo({ url: `../textbook/select?classId=${this.data.id}&subject=${encodeURIComponent(subject)}` })
    },
    addTextbook(e: WechatMiniprogram.PickerChange) {
      if (!this.data.isAdmin) {
        wx.showToast({ title: '需要管理员权限', icon: 'none' })
        return
      }
      const subject = this.data.subjects[Number(e.detail.value)]
      wx.navigateTo({ url: `../textbook/select?classId=${this.data.id}&subject=${encodeURIComponent(subject)}` })
    },

    // ---- 退出 / 解散 ----
    quit() {
      wx.showModal({
        title: '退出班级',
        content: '退出后不再看到该班作业与资料。确定退出？',
        success: (res) => {
          if (res.confirm) {
            quitClass(this.data.id)
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
    dissolve() {
      wx.showModal({
        title: '解散班级',
        content: '解散后班级对所有人不可见。确定解散？',
        confirmColor: '#fa5151',
        success: (res) => {
          if (res.confirm) {
            dissolveClass(this.data.id)
              .then(() => {
                wx.showToast({ title: '已解散', icon: 'success' })
                setTimeout(() => wx.navigateBack(), 600)
              })
              .catch((err: unknown) =>
                wx.showToast({ title: err instanceof Error ? err.message : '解散失败', icon: 'none' }),
              )
          }
        },
      })
    },
  },
})
