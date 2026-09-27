// api/auth.ts — 登录 / 注销 / 用户管理
//
// 使用 axios client（已配置 baseURL + Authorization 注入）；
// 登录成功时同时写入 user store。

import { client } from './client'
import { useUserStore, type CurrentUser } from '@/stores/user'

export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  expiresAt: string
  user: CurrentUser
}

export interface CreateUserRequest {
  username: string
  password: string
  role: 'admin' | 'user'
}

export async function login(username: string, password: string): Promise<LoginResponse> {
  const { data } = await client.post<LoginResponse>('/auth/login', {
    username,
    password,
  } as LoginRequest)
  useUserStore().setSession(data.token, data.user, data.expiresAt)
  return data
}

export async function logout(): Promise<void> {
  try {
    await client.post('/auth/logout')
  } catch {
    /* even if server fails, clear local */
  }
  useUserStore().clear()
}

export async function fetchMe(): Promise<CurrentUser> {
  const { data } = await client.get<CurrentUser>('/auth/me')
  useUserStore().refreshUser(data)
  return data
}

export async function listUsers(): Promise<CurrentUser[]> {
  const { data } = await client.get<CurrentUser[]>('/auth/users')
  return data
}

export async function createUser(req: CreateUserRequest): Promise<CurrentUser> {
  const { data } = await client.put<CurrentUser>('/auth/users', req)
  return data
}

export async function deleteUser(id: number): Promise<void> {
  await client.delete(`/auth/users?id=${id}`)
}
