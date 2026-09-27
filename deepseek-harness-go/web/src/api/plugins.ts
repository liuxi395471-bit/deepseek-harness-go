// api/plugins.ts — Plugins REST 客户端

import { client } from './client'
import type { PluginItem } from './types'

export const pluginsApi = {
  list: async (): Promise<PluginItem[]> => {
    const { data } = await client.get<{ items: PluginItem[] }>('/plugins')
    return data.items ?? []
  },
  enable: async (name: string): Promise<void> => {
    await client.post(`/plugins/${encodeURIComponent(name)}/enable`)
  },
  disable: async (name: string): Promise<void> => {
    await client.post(`/plugins/${encodeURIComponent(name)}/disable`)
  },
}
