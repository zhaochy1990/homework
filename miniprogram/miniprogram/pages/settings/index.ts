import { clearTokens } from '../../api/auth'
import { getMe, Me } from '../../api/user'

// 设置：账号信息与本地登录态管理（v1 无更多项）。
Component({
  data: {
    me: null as Me | null,
  },
  methods: {
    onLoad() {
      getMe()
        .then((me) => this.setData({ me }))
        .catch(() => undefined)
    },
    logout() {
      wx.showModal({
        title: '退出登录',
        content: '将清除本地登录态，下次进入自动重新登录。',
        success: (res) => {
          if (res.confirm) {
            clearTokens()
            wx.reLaunch({ url: '/pages/mine/mine' })
          }
        },
      })
    },
  },
})
