import { request } from './request'

/** 全局封闭科目枚举（ADR 0007），顺序与后端 subject.All 一致。 */
export const SUBJECTS = [
  '语文',
  '数学',
  '英语',
  '物理',
  '化学',
  '生物',
  '历史',
  '地理',
  '道法',
  '科学',
  '体育',
  '艺术',
  '其他',
]

export interface Unit {
  id: number
  name: string
  sortOrder: number
}

export interface Textbook {
  id: number
  subject: string
  name: string
  grade: string
  term: string
  createdBy: number
  createdAt: string
  units: Unit[]
}

export interface ClassTextbook {
  subject: string
  textbook: Textbook
}

export function searchTextbooks(subject: string, keyword: string): Promise<Textbook[]> {
  return request<{ items: Textbook[] }>({
    url: `/textbooks?subject=${encodeURIComponent(subject)}&keyword=${encodeURIComponent(keyword)}`,
  }).then((r) => r.items)
}

export function createTextbook(input: {
  subject: string
  name: string
  grade: string
  term: string
}): Promise<Textbook> {
  return request<Textbook>({ url: '/textbooks', method: 'POST', data: input, loading: '创建中' })
}

export function listClassTextbooks(classId: number): Promise<ClassTextbook[]> {
  return request<{ items: ClassTextbook[] }>({ url: `/classes/${classId}/textbooks` }).then(
    (r) => r.items,
  )
}

export function setClassTextbook(
  classId: number,
  subject: string,
  textbookId: number,
): Promise<void> {
  return request<unknown>({
    url: `/classes/${classId}/textbooks`,
    method: 'PUT',
    data: { subject, textbookId },
    loading: '保存中',
  }).then(() => undefined)
}
