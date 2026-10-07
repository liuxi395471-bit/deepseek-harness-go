// api/attachments.ts — v8.1 P4 附件 API 客户端（DSH 原生 composer attachment rail）

import { client } from './client'
import type { AttachmentItem, FileEntry } from './types'

/** 列出 session 全部附件。 */
export async function listAttachments(sid: string): Promise<AttachmentItem[]> {
  const { data } = await client.get(`/sessions/${sid}/attachments`)
  return data?.items ?? []
}

/** 上传一个文件到 session，返回 attachment 元数据。
 *  progress 回调（0..1）可选。 */
export async function uploadAttachment(
  sid: string,
  file: File,
  onProgress?: (p: number) => void
): Promise<AttachmentItem> {
  const form = new FormData()
  form.append('file', file, file.name)
  const { data } = await client.post(`/sessions/${sid}/attachments`, form, {
    headers: { 'Content-Type': 'multipart/form-data' },
    onUploadProgress: (e) => {
      if (onProgress && e.total) onProgress(e.loaded / e.total)
    },
    timeout: 60_000,
  })
  return data
}

/** 删除附件。 */
export async function deleteAttachment(sid: string, aid: string): Promise<void> {
  await client.delete(`/sessions/${sid}/attachments/${aid}`)
}

/** 获取附件原始 URL（用于 <img src> / <a href>）。 */
export function attachmentUrl(sid: string, aid: string): string {
  return `/api/v1/console/sessions/${sid}/attachments/${aid}`
}

/** 列出工作区文件（@ 触发器数据源）。 */
export async function listFiles(
  path = '.',
  depth = 2
): Promise<{ path: string; entries: FileEntry[]; count: number }> {
  const { data } = await client.get('/files/', {
    params: { path, depth },
  })
  return data
}
