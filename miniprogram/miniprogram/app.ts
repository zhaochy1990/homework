import { ensureAccessToken } from './api/auth'

App<IAppOption>({
  globalData: {},
  onLaunch() {
    // 静默登录：失败不阻塞启动，各页面按需重试（「我的」页有失败态）。
    ensureAccessToken().catch((err) => {
      console.warn('[auth] 静默登录失败', err)
    })
  },
})
