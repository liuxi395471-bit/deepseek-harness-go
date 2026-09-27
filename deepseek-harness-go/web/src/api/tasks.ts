// api/tasks.ts — Tasks REST 客户端

import { client } from './client'
import type { TaskItem } from './types'

export const tasksApi = {
  list: async (state = ''): Promise<TaskItem[]> => {
    const { data } = await client.get<{ items: TaskItem[] }>('/tasks', {
      params: state ? { state } : {},
    })
    return data.items ?? []
  },
  create: async (params: { title: string; input: string; profile?: string }): Promise<TaskItem> => {
    const { data } = await client.post<TaskItem>('/tasks', params)
    return data
  },
  cancel: async (id: string): Promise<void> => {
    await client.post(`/tasks/${encodeURIComponent(id)}/cancel`)
  },
  retry: async (id: string): Promise<TaskItem> => {
    const { data } = await client.post<TaskItem>(`/tasks/${encodeURIComponent(id)}/retry`)
    return data
  },
}
