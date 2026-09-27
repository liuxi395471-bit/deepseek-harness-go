// api/approvals.ts — Approvals REST 客户端

import { client } from './client'
import type { ApprovalItem, Decision } from './types'

export const approvalsApi = {
  list: async (): Promise<ApprovalItem[]> => {
    const { data } = await client.get<{ items: ApprovalItem[] }>('/approvals')
    return data.items ?? []
  },
  decide: async (id: string, decision: Decision): Promise<void> => {
    await client.post(`/approvals/${encodeURIComponent(id)}/decide`, { decision })
  },
  decideBatch: async (ids: string[], decision: Decision): Promise<void> => {
    await client.post('/approvals/decide-batch', { ids, decision })
  },
}
