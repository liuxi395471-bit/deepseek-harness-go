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
  cancel: async (id: string): Promise<void> => {
    await client.post(`/tasks/${encodeURIComponent(id)}/cancel`)
  },
}
