// api/models.ts — Models REST 客户端

import { client } from './client'
import type { ModelItem, PingResult } from './types'

export const modelsApi = {
  list: async (): Promise<ModelItem[]> => {
    const { data } = await client.get<{ items: ModelItem[] }>('/models')
    return data.items ?? []
  },
  create: async (item: Partial<ModelItem>): Promise<ModelItem> => {
    const { data } = await client.post<ModelItem>('/models', item)
    return data
  },
  update: async (channel: string, item: Partial<ModelItem>): Promise<void> => {
    await client.put(`/models/${encodeURIComponent(channel)}`, item)
  },
  remove: async (channel: string): Promise<void> => {
    await client.delete(`/models/${encodeURIComponent(channel)}`)
  },
  ping: async (channel: string): Promise<PingResult> => {
    const { data } = await client.post<PingResult>(
      `/models/${encodeURIComponent(channel)}/ping`,
    )
    return data
  },
}
