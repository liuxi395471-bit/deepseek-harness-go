// api/schedules.ts — Schedule CRUD（v8.1）

import { client } from './client'

export interface ScheduleItem {
  id: string
  name: string
  cron: string
  action: string
  enabled: boolean
  createdAt: string
  lastRunAt?: string
  nextRunAt?: string
  lastStatus?: string
  lastError?: string
}

export async function listSchedules(): Promise<ScheduleItem[]> {
  const { data } = await client.get<ScheduleItem[]>('/schedules')
  return data
}

export async function createSchedule(item: Partial<ScheduleItem>): Promise<ScheduleItem> {
  const { data } = await client.post<ScheduleItem>('/schedules', item)
  return data
}

export async function updateSchedule(id: string, item: Partial<ScheduleItem>): Promise<ScheduleItem> {
  const { data } = await client.put<ScheduleItem>(`/schedules/${id}`, item)
  return data
}

export async function deleteSchedule(id: string): Promise<void> {
  await client.delete(`/schedules/${id}`)
}

export async function runSchedule(id: string): Promise<ScheduleItem> {
  const { data } = await client.post<ScheduleItem>(`/schedules/${id}/run`)
  return data
}
