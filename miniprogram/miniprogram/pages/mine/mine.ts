import { getMe, Me } from '../../api/user'

// 我的页：T15 用 GET /me 验证登录与请求链路，T19 再补班级/孩子管理入口。
Component({
  data: {
    me: null as Me | null,
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
      getMe()
        .then((me) => this.setData({ me, loading: false }))
        .catch((err: unknown) => {
          this.setData({ loading: false, error: err instanceof Error ? err.message : '加载失败' })
        })
    },
    retry() {
      this.load()
    },
  },
})
