// i18n — 极简多语言（zh-CN / en-US）。
//
// 设计：用简单字典 + useI18n composable。无 vue-i18n 依赖。
// key 用点号连接（如 "sessions.empty"）。

import { computed, ref } from 'vue'

export type Locale = 'zh-CN' | 'en-US'

const STORAGE_KEY = 'dsh.console.locale'
const initial: Locale = (() => {
  try {
    const v = localStorage.getItem(STORAGE_KEY)
    if (v === 'zh-CN' || v === 'en-US') return v
  } catch {
    /* ignore */
  }
  return 'zh-CN'
})()

export const locale = ref<Locale>(initial)

const dict: Record<Locale, Record<string, string>> = {
  'zh-CN': {
    'app.brand': 'DeepSeek Harness · 控制台',
    'common.refresh': '刷新',
    'common.cancel': '取消',
    'common.delete': '删除',
    'common.confirm': '确定',
    'common.close': '关闭',
    'common.search': '搜索',
    'common.theme.light': '浅色',
    'common.theme.dark': '深色',
    'common.language': '语言',
    'common.connect': '连接',
    'common.disconnect': '断开',
    'common.connected': '已连接',
    'common.authFailed': '认证失败',
    'common.disconnected': '未连接',
    'common.copy': '复制',
    'common.export': '导出',
    'common.retry': '重试',
    'common.required': '必填',

    'nav.sessions': '会话',
    'nav.plugins': '插件',
    'nav.models': '模型',
    'nav.tasks': '任务',
    'nav.approvals': '审批',

    'sessions.title': '会话',
    'sessions.newSession': '新建会话',
    'sessions.dialogTitle': '新建会话',
    'sessions.titleLabel': '会话标题',
    'sessions.modelLabel': '模型渠道',
    'sessions.empty': '暂无会话',
    'sessions.filter': '过滤会话',
    'sessions.detail.send': '发送',
    'sessions.detail.input': '输入消息…',
    'sessions.detail.delete': '删除消息',
    'sessions.detail.edit': '编辑消息',
    'sessions.detail.save': '保存',
    'sessions.detail.tokens': 'Token 用量',
    'sessions.detail.rounds': '轮次',
    'sessions.detail.model': '模型',

    'plugins.title': '插件管理',
    'plugins.empty': '暂无插件',
    'plugins.install': '安装',
    'plugins.uninstall': '卸载',
    'plugins.enable': '启用',
    'plugins.disable': '停用',
    'plugins.dialogTitle': '安装插件',
    'plugins.name': '插件名称',
    'plugins.source': '来源（任意标签）',

    'models.title': '模型渠道',
    'models.empty': '暂无模型渠道',
    'models.ping': 'ping',
    'models.enable': '启用',
    'models.disable': '停用',
    'models.add': '新增渠道',
    'models.dialogTitle': '新增模型渠道',
    'models.channel': '渠道 ID',
    'models.model': '模型名',
    'models.protocol': '协议',
    'models.baseUrl': 'Base URL',
    'models.ok': '连通',

    'tasks.title': '任务调度',
    'tasks.empty': '暂无任务',
    'tasks.state': '全部状态',
    'tasks.retry': '重试失败任务',
    'tasks.cancel': '取消',
    'tasks.create': '新建任务',
    'tasks.dialogTitle': '新建任务',
    'tasks.titleLabel': '任务标题',
    'tasks.input': '任务输入（必填）',
    'tasks.profile': 'Profile',

    'approvals.title': '待审批',
    'approvals.empty': '暂无待审批请求',
    'approvals.allow': '允许',
    'approvals.always': '始终',
    'approvals.deny': '拒绝',
    'approvals.batchAllow': '批量允许',
    'approvals.batchDeny': '批量拒绝',
    'approvals.batchSelect': '已选',
    'approvals.allChecked': '全选',

    'audit.title': '审计日志',
    'audit.export': '导出 JSONL',
    'audit.empty': '暂无审计记录',

    'error.toast.title': '操作失败',
    'error.network': '网络错误',
  },
  'en-US': {
    'app.brand': 'DeepSeek Harness · Console',
    'common.refresh': 'Refresh',
    'common.cancel': 'Cancel',
    'common.delete': 'Delete',
    'common.confirm': 'OK',
    'common.close': 'Close',
    'common.search': 'Search',
    'common.theme.light': 'Light',
    'common.theme.dark': 'Dark',
    'common.language': 'Language',
    'common.connect': 'Connect',
    'common.disconnect': 'Disconnect',
    'common.connected': 'Connected',
    'common.authFailed': 'Auth failed',
    'common.disconnected': 'Disconnected',
    'common.copy': 'Copy',
    'common.export': 'Export',
    'common.retry': 'Retry',
    'common.required': 'required',

    'nav.sessions': 'Sessions',
    'nav.plugins': 'Plugins',
    'nav.models': 'Models',
    'nav.tasks': 'Tasks',
    'nav.approvals': 'Approvals',

    'sessions.title': 'Sessions',
    'sessions.newSession': 'New Session',
    'sessions.dialogTitle': 'New Session',
    'sessions.titleLabel': 'Title',
    'sessions.modelLabel': 'Model channel',
    'sessions.empty': 'No sessions yet',
    'sessions.filter': 'Filter sessions',
    'sessions.detail.send': 'Send',
    'sessions.detail.input': 'Type a message…',
    'sessions.detail.delete': 'Delete message',
    'sessions.detail.edit': 'Edit message',
    'sessions.detail.save': 'Save',
    'sessions.detail.tokens': 'Token usage',
    'sessions.detail.rounds': 'Rounds',
    'sessions.detail.model': 'Model',

    'plugins.title': 'Plugins',
    'plugins.empty': 'No plugins yet',
    'plugins.install': 'Install',
    'plugins.uninstall': 'Uninstall',
    'plugins.enable': 'Enable',
    'plugins.disable': 'Disable',
    'plugins.dialogTitle': 'Install Plugin',
    'plugins.name': 'Plugin name',
    'plugins.source': 'Source (label)',

    'models.title': 'Model Channels',
    'models.empty': 'No model channels',
    'models.ping': 'ping',
    'models.enable': 'Enable',
    'models.disable': 'Disable',
    'models.add': 'New channel',
    'models.dialogTitle': 'New Model Channel',
    'models.channel': 'Channel ID',
    'models.model': 'Model name',
    'models.protocol': 'Protocol',
    'models.baseUrl': 'Base URL',
    'models.ok': 'Reachable',

    'tasks.title': 'Tasks',
    'tasks.empty': 'No tasks yet',
    'tasks.state': 'All states',
    'tasks.retry': 'Retry failed task',
    'tasks.cancel': 'Cancel',
    'tasks.create': 'New task',
    'tasks.dialogTitle': 'New Task',
    'tasks.titleLabel': 'Title',
    'tasks.input': 'Input (required)',
    'tasks.profile': 'Profile',

    'approvals.title': 'Pending approvals',
    'approvals.empty': 'No pending approvals',
    'approvals.allow': 'Allow',
    'approvals.always': 'Always',
    'approvals.deny': 'Deny',
    'approvals.batchAllow': 'Batch allow',
    'approvals.batchDeny': 'Batch deny',
    'approvals.batchSelect': 'selected',
    'approvals.allChecked': 'Select all',

    'audit.title': 'Audit log',
    'audit.export': 'Export JSONL',
    'audit.empty': 'No audit records',

    'error.toast.title': 'Action failed',
    'error.network': 'Network error',
  },
}

export function t(key: string): string {
  const l = locale.value
  return dict[l]?.[key] ?? dict['zh-CN'][key] ?? key
}

export function setLocale(l: Locale) {
  locale.value = l
  try {
    localStorage.setItem(STORAGE_KEY, l)
  } catch {
    /* ignore */
  }
}

export function useI18n() {
  return {
    locale,
    setLocale,
    // 直接暴露 t 函数（响应式；locale 切换时调用结果会跟着变）。
    t: (k: string) => t(k),
  }
}
