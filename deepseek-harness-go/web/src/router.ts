// router — Vue Router 4 配置
//
// 5 页 + 1 详情（嵌套在 sessions 下）；每个页面用 <script setup>。
// History 模式：dsh -serve 由 SPA fallback 接管 deep-link。

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/sessions',
  },
  {
    path: '/sessions',
    name: 'sessions',
    component: () => import('@/views/SessionsView.vue'),
  },
  {
    path: '/sessions/:sid',
    name: 'session-detail',
    component: () => import('@/views/SessionDetailView.vue'),
    props: true,
  },
  {
    path: '/plugins',
    name: 'plugins',
    component: () => import('@/views/PluginsView.vue'),
  },
  {
    path: '/models',
    name: 'models',
    component: () => import('@/views/ModelsView.vue'),
  },
  {
    path: '/tasks',
    name: 'tasks',
    component: () => import('@/views/TasksView.vue'),
  },
  {
    path: '/approvals',
    name: 'approvals',
    component: () => import('@/views/ApprovalsView.vue'),
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/sessions',
  },
]

export const router = createRouter({
  history: createWebHistory('/console/'),
  routes,
})
