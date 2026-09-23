import { getMaterial, listMaterials, MaterialItem } from '../../api/material'

interface Viewer {
  visible: boolean
  type: 'text' | 'video' | ''
  title: string
  body: string
  url: string
}

const emptyViewer: Viewer = { visible: false, type: '', title: '', body: '', url: '' }

// 单元资料列表：视频内联播放（预签名 URL，Range 原生支持）、图片全屏、文本弹层。
Component({
  data: {
    classId: 0,
    unitId: 0,
    unitName: '',
    items: [] as MaterialItem[],
    loading: false,
    viewer: emptyViewer,
  },
  methods: {
    onLoad(query: Record<string, string | undefined>) {
      const classId = Number(query.classId) || 0
      const unitId = Number(query.unitId) || 0
      const unitName = query.unitName ? decodeURIComponent(query.unitName) : ''
      this.setData({ classId, unitId, unitName })
      if (unitName) {
        wx.setNavigationBarTitle({ title: unitName })
      }
      this.load()
    },
    load() {
      if (!this.data.classId) {
        return
      }
      this.setData({ loading: true })
      listMaterials(this.data.classId, { unitId: this.data.unitId })
        .then((items) => this.setData({ items, loading: false }))
        .catch((err: unknown) => {
          this.setData({ loading: false })
          wx.showToast({ title: err instanceof Error ? err.message : '加载失败', icon: 'none' })
        })
    },
    open(e: WechatMiniprogram.BaseEvent) {
      const id = Number(e.currentTarget.dataset.id)
      const item = this.data.items.find((m) => m.id === id)
      if (!item) {
        return
      }
      if (item.type === 'text') {
        this.setData({
          viewer: { visible: true, type: 'text', title: item.title, body: item.body || '', url: '' },
        })
        return
      }
      getMaterial(id)
        .then((detail) => {
          const url = detail.playbackUrl || ''
          if (!url) {
            wx.showToast({ title: '资源地址不可用', icon: 'none' })
            return
          }
          if (detail.type === 'image') {
            // 图片全屏用系统预览器。
            wx.previewImage({ urls: [url], current: url })
          } else {
            this.setData({
              viewer: { visible: true, type: 'video', title: detail.title, body: '', url },
            })
          }
        })
        .catch((err: unknown) => {
          wx.showToast({ title: err instanceof Error ? err.message : '打开失败', icon: 'none' })
        })
    },
    closeViewer() {
      this.setData({ viewer: emptyViewer })
    },
    noop() {
      // 阻止点穿：点面板不关闭弹层。
    },
  },
})
