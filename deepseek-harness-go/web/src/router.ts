// router — Vue Router 4 配置
//
// v8.1 起：除 /login 外所有路由要求登录；未登录跳 /login?redirect=。
// v8.1 P4: 后端 NoAuth 模式下，router guard 等 bootApp 完成（自动 stub
// login）后再判断 isLoggedIn。

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { waitForBoot } from '@/stores/boot'

const routes: RouteRecordRaw[] = [
  {
    path: '/',
    redirect: '/sessions',
  },
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
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
    path: '/schedules',
    name: 'schedules',
    component: () => import('@/views/SchedulesView.vue'),
  },
  {
    path: '/webhooks',
    name: 'webhooks',
    component: () => import('@/views/WebhooksView.vue'),
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

router.beforeEach(async (to) => {
  // v8.1 P4: 等 App.onMounted 内的 bootApp() 完成（NoAuth 时自动 stub login）
  await waitForBoot()
  // /login 永远允许
  if (to.name === 'login') return true
  // /sessions/:sid 当 sid === 'demo' 时是 dsh 原生 demo 路径，
  // 允许未登录访问（前端走纯 mock 数据，不发任何 API 请求）。
  // 这是 dsh 桌面 demo 体验的入口。
  if (to.name === 'session-detail' && to.params.sid === 'demo') return true
  // 其他路由：要求已登录
  const userStore = useUserStore()
  if (!userStore.isLoggedIn) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})
