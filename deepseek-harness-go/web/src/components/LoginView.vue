<script setup lang="ts">
/**
 * 登录页 —— 输入后端配置的 Bearer token。
 *
 * 后端用常量时间比较校验 token（internal/server/server.go），
 * 因此这里只做"非空 + 能通过一次真实请求"的校验。
 */
import { ref } from 'vue'
import { LoaderCircle, Lock, ShieldCheck } from 'lucide-vue-next'
import * as api from '@/api/client'
import { setAuthToken } from '@/composables/useChat'

const emit = defineEmits<{
  (e: 'success'): void
}>()

const value = ref('')
const loading = ref(false)
const error = ref('')

async function submit(): Promise<void> {
  const t = value.value.trim()
  if (!t) {
    error.value = '请输入访问令牌'
    return
  }
  loading.value = true
  error.value = ''

  // 先验证、后提交：只有确认 token 有效才写入全局状态与 localStorage。
  // 反过来做（先 setAuthToken 再验证）会让 App.vue 的 loggedIn 立刻为真，
  // 主界面先闪一下再退回登录页。
  try {
    await api.verifyToken(t)
    setAuthToken(t)
    emit('success')
  } catch (e) {
    // 验证失败：保持未登录态。
    // 必须依据 HTTP 状态码判断 401：后端错误体是纯文本 "unauthorized"，
    // 不含状态码，靠 message.includes('401') 永远匹配不到。
    if (e instanceof api.ApiError && e.status === 401) {
      error.value = '令牌无效：与后端 cfg.server.auth-token 不一致'
    } else {
      error.value = `验证失败：${e instanceof Error ? e.message : String(e)}`
    }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <form class="login__card" @submit.prevent="submit">
      <div class="login__icon" aria-hidden="true">
        <ShieldCheck :size="26" />
      </div>

      <h1 class="login__title">DeepSeek Harness</h1>
      <p class="login__subtitle">请输入后端配置的访问令牌以继续</p>

      <label class="login__field">
        <span class="login__label">
          <Lock :size="13" />
          访问令牌
        </span>
        <input
          v-model="value"
          class="login__input"
          type="password"
          placeholder="cfg.server.auth-token 的值"
          autocomplete="current-password"
          :disabled="loading"
          autofocus
        />
      </label>

      <p v-if="error" class="login__error" role="alert">{{ error }}</p>

      <button class="login__submit" type="submit" :disabled="loading">
        <LoaderCircle v-if="loading" :size="16" class="spin" />
        <span>{{ loading ? '验证中…' : '进入' }}</span>
      </button>

      <p class="login__note">
        令牌对应服务端配置的 <code>server.auth-token</code>，
        或环境变量 <code>DSH_SERVER_AUTH_TOKEN</code>。
      </p>
    </form>
  </div>
</template>

<style scoped>
.login {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  background: var(--bg-app);
}

.login__card {
  width: 100%;
  max-width: 380px;
  padding: 32px 28px;
  background: var(--bg-elevated);
  border: 1px solid var(--border-subtle);
  border-radius: var(--radius-lg);
  box-shadow: var(--shadow-md);
  text-align: center;
}

.login__icon {
  width: 52px;
  height: 52px;
  margin: 0 auto 16px;
  border-radius: 14px;
  background: var(--accent-soft);
  color: var(--accent-text);
  display: flex;
  align-items: center;
  justify-content: center;
}

.login__title {
  margin: 0 0 6px;
  font-size: 21px;
  font-weight: 600;
  letter-spacing: -0.01em;
}

.login__subtitle {
  margin: 0 0 22px;
  font-size: 14px;
  color: var(--text-secondary);
}

.login__field {
  display: block;
  text-align: left;
  margin-bottom: 14px;
}

.login__label {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12.5px;
  font-weight: 500;
  color: var(--text-secondary);
  margin-bottom: 6px;
}

.login__input {
  width: 100%;
  padding: 10px 13px;
  border-radius: var(--radius-md);
  border: 1px solid var(--border-strong);
  background: var(--bg-input);
  font-size: 14.5px;
  outline: none;
  transition: border-color var(--transition), box-shadow var(--transition);
}

.login__input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 3px var(--accent-soft);
}

.login__error {
  margin: 0 0 14px;
  padding: 9px 12px;
  border-radius: var(--radius-sm);
  background: var(--danger-soft);
  color: var(--danger);
  font-size: 13px;
  text-align: left;
}

.login__submit {
  width: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 11px 16px;
  border-radius: var(--radius-md);
  background: var(--text-primary);
  color: var(--text-inverse);
  font-size: 14.5px;
  font-weight: 500;
  transition: opacity var(--transition), transform var(--transition);
}

[data-theme='dark'] .login__submit {
  background: var(--accent);
  color: #0d0d0d;
}

.login__submit:hover:not(:disabled) {
  transform: translateY(-1px);
}

.login__submit:disabled {
  opacity: 0.6;
  cursor: not-allowed;
}

.login__note {
  margin: 18px 0 0;
  font-size: 12px;
  color: var(--text-tertiary);
  line-height: 1.6;
}

.login__note code {
  font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
  font-size: 11.5px;
  background: var(--bg-code);
  border: 1px solid var(--border-subtle);
  border-radius: 4px;
  padding: 0.1em 0.35em;
}

.spin {
  animation: spin 900ms linear infinite;
}
</style>