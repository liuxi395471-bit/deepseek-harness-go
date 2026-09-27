// api/webhooks.ts — Webhook CRUD（v8.1）

import { client } from './client'

export interface WebhookItem {
  id: string
  name: string
  url: string
  secret?: string
  enabled: boolean
  createdAt: string
  lastStatus?: number
  lastError?: string
  lastDeliveredAt?: string
}

export interface WebhookDelivery {
  id: string
  webhookId: string
  payload: string
  statusCode: number
  attempt: number
  ok: boolean
  deliveredAt: string
  error?: string
}

export async function listWebhooks(): Promise<WebhookItem[]> {
  const { data } = await client.get<WebhookItem[]>('/webhooks')
  return data
}

export async function createWebhook(item: Partial<WebhookItem>): Promise<WebhookItem> {
  const { data } = await client.post<WebhookItem>('/webhooks', item)
  return data
}

export async function updateWebhook(id: string, item: Partial<WebhookItem>): Promise<WebhookItem> {
  const { data } = await client.put<WebhookItem>(`/webhooks/${id}`, item)
  return data
}

export async function deleteWebhook(id: string): Promise<void> {
  await client.delete(`/webhooks/${id}`)
}

export async function testWebhook(id: string, payload = ''): Promise<WebhookDelivery> {
  const { data } = await client.post<WebhookDelivery>(`/webhooks/${id}/test`, { payload })
  return data
}

export async function listDeliveries(webhookId: string, limit = 50): Promise<WebhookDelivery[]> {
  const { data } = await client.get<WebhookDelivery[]>(`/webhooks/${webhookId}/deliveries?limit=${limit}`)
  return data
}
