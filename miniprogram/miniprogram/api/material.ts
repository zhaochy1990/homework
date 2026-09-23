import { request } from './request'

export type MaterialType = 'text' | 'image' | 'video'
export type MaterialSecStatus = 'pending' | 'pass' | 'blocked'

export interface MaterialItem {
  id: number
  classId: number
  unitId?: number
  type: MaterialType
  title: string
  body?: string
  sizeBytes?: number
  secStatus: MaterialSecStatus
  /** 仅详情返回：图片/视频的预签名 GET URL（1~2h，Range 原生支持）。 */
  playbackUrl?: string
  createdAt: string
}

/**
 * 班级资料列表（先审后显：后端只回 sec_status=pass，pending/blocked 不可见）。
 * unitId 省略时返回该班全部可见资料。
 */
export function listMaterials(
  classId: number,
  params?: { unitId?: number; type?: MaterialType },
): Promise<MaterialItem[]> {
  const query: string[] = ['page_size=100']
  if (params && params.unitId) {
    query.push('unitId=' + params.unitId)
  }
  if (params && params.type) {
    query.push('type=' + params.type)
  }
  return request<{ items: MaterialItem[] }>({
    url: `/classes/${classId}/materials?${query.join('&')}`,
  }).then((r) => r.items)
}

/** 资料详情：图片/视频带预签名播放 URL。 */
export function getMaterial(id: number): Promise<MaterialItem> {
  return request<MaterialItem>({ url: `/materials/${id}` })
}
