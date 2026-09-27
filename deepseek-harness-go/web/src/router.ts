// router — Vue Router 4 配置
//
// v8.1 起：除 /login 外所有路由要求登录；未登录跳 /login?redirect=。

import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useUserStore } from '@/stores/user'

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

router.beforeEach((to) => {
  // /login 永远允许
  if (to.name === 'login') return true
  // 其他路由：要求已登录
  const userStore = useUserStore()
  if (!userStore.isLoggedIn) {
    return { path: '/login', query: { redirect: to.fullPath } }
  }
  return true
})
