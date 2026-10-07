// stores/workspace.ts — 工作区分组（v8.1 P3）
//
// 工作区是用户级的 session 容器：左栏按工作区分组展示 session list。
// v8.1 P3 简化为纯前端 localStorage 持久化（不上后端），session 所属
// 工作区以 {sid → workspace} 的 map 形式持久化在 localStorage 中。
//
// 行为：
//   - 默认工作区 "默认"（id = "default"）始终存在且不可删除；
//   - 用户可新增 / 重命名 / 删除 自定义工作区；
//   - currentId 决定新会话落入哪个工作区；
//   - 切换 currentId 时，左栏按当前 id 过滤显示。
//   - getSessionWorkspace(sid) 返回该 session 的 workspace id（缺省
//     时返回 DEFAULT_ID —— 这样旧 session 自动归到默认工作区）。
//
// 持久化：localStorage 'dsh.console.workspaces' + '.currentWorkspace'
//       + '.sessionMeta'。

import { defineStore } from 'pinia'
import { ref, computed, watch } from 'vue'

export interface Workspace {
  id: string
  name: string
  createdAt: string
}

const STORAGE_KEY = 'dsh.console.workspaces'
const CURRENT_KEY = 'dsh.console.currentWorkspace'
const META_KEY = 'dsh.console.sessionMeta'
export const DEFAULT_ID = 'default'

function makeId(): string {
  return 'ws_' + Math.random().toString(36).slice(2, 9) + Date.now().toString(36).slice(-4)
}

function loadWorkspaces(): Workspace[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (raw) {
      const list = JSON.parse(raw) as Workspace[]
      // 始终保证默认工作区在第一位
      if (!list.find((w) => w.id === DEFAULT_ID)) {
        list.unshift({
          id: DEFAULT_ID,
          name: '默认',
          createdAt: new Date().toISOString(),
        })
      }
      return list
    }
  } catch {
    /* ignore */
  }
  return [
    {
      id: DEFAULT_ID,
      name: '默认',
      createdAt: new Date().toISOString(),
    },
  ]
}

function loadCurrent(workspaces: Workspace[]): string {
  try {
    const id = localStorage.getItem(CURRENT_KEY)
    if (id && workspaces.find((w) => w.id === id)) return id
  } catch {
    /* ignore */
  }
  return DEFAULT_ID
}

function loadMeta(): Record<string, { workspace?: string }> {
  try {
    const raw = localStorage.getItem(META_KEY)
    if (raw) return JSON.parse(raw)
  } catch {
    /* ignore */
  }
  return {}
}

export const useWorkspaceStore = defineStore('workspace', () => {
  const workspaces = ref<Workspace[]>(loadWorkspaces())
  const currentId = ref<string>(loadCurrent(workspaces.value))
  // sid → { workspace?: string }：纯前端元数据，与后端 session 表无关
  const sessionMeta = ref<Record<string, { workspace?: string }>>(loadMeta())

  const current = computed(() => {
    return workspaces.value.find((w) => w.id === currentId.value) || workspaces.value[0]
  })

  function persist() {
    try {
      localStorage.setItem(STORAGE_KEY, JSON.stringify(workspaces.value))
      localStorage.setItem(CURRENT_KEY, currentId.value)
      localStorage.setItem(META_KEY, JSON.stringify(sessionMeta.value))
    } catch {
      /* ignore */
    }
  }

  // 自动持久化
  watch(workspaces, persist, { deep: true })
  watch(currentId, persist)
  watch(sessionMeta, persist, { deep: true })

  function setCurrent(id: string) {
    if (workspaces.value.find((w) => w.id === id)) {
      currentId.value = id
    }
  }

  function add(name: string): Workspace {
    const ws: Workspace = {
      id: makeId(),
      name: name.trim() || '新工作区',
      createdAt: new Date().toISOString(),
    }
    workspaces.value.push(ws)
    currentId.value = ws.id
    return ws
  }

  function rename(id: string, name: string) {
    const w = workspaces.value.find((x) => x.id === id)
    if (w) w.name = name.trim() || w.name
  }

  function remove(id: string): string {
    if (id === DEFAULT_ID) return currentId.value
    const idx = workspaces.value.findIndex((x) => x.id === id)
    if (idx < 0) return currentId.value
    workspaces.value.splice(idx, 1)
    // 该工作区下所有 session meta 归到默认工作区
    for (const sid of Object.keys(sessionMeta.value)) {
      if (sessionMeta.value[sid]?.workspace === id) {
        sessionMeta.value[sid] = { workspace: DEFAULT_ID }
      }
    }
    if (currentId.value === id) {
      currentId.value = DEFAULT_ID
    }
    return currentId.value
  }

  // 把 sid 标记为当前工作区
  function bind(sid: string) {
    if (!sid) return
    sessionMeta.value[sid] = { ...(sessionMeta.value[sid] || {}), workspace: currentId.value }
  }

  // 读取 sid 所属工作区（缺省 = DEFAULT_ID）
  function getSessionWorkspace(sid: string): string {
    return sessionMeta.value[sid]?.workspace || DEFAULT_ID
  }

  // 删除 sid 的元数据
  function unbind(sid: string) {
    delete sessionMeta.value[sid]
  }

  return {
    workspaces,
    currentId,
    current,
    sessionMeta,
    setCurrent,
    add,
    rename,
    remove,
    bind,
    getSessionWorkspace,
    unbind,
  }
})
