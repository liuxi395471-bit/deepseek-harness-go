<script setup lang="ts">
// LoginView — v8.1 JWT 登录页
//
// 表单：用户名 + 密码。提交时调 /auth/login；成功后写入 userStore，
// 跳到原目标 / 默认 /sessions。

import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { LogIn, AlertCircle } from 'lucide-vue-next'
import { login } from '@/api/auth'
import { useI18n } from '@/i18n'

const router = useRouter()
const route = useRoute()
const { t } = useI18n()

const username = ref('')
const password = ref('')
const submitting = ref(false)
const errorMsg = ref('')

async function onSubmit() {
  if (!username.value.trim() || !password.value) {
    errorMsg.value = '请输入用户名和密码'
    return
  }
  submitting.value = true
  errorMsg.value = ''
  try {
    await login(username.value.trim(), password.value)
    const target = (route.query.redirect as string) || '/sessions'
    router.push(target)
  } catch (e) {
    errorMsg.value = (e as Error).message || t('login.error')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <div class="login-page">
    <div class="login-card">
      <div class="brand">
        <span class="logo">⚡</span>
        <h1>{{ t('app.brand') }}</h1>
      </div>
      <p class="hint">{{ t('login.hint') }}</p>
      <form @submit.prevent="onSubmit">
        <label class="field">
          <span>{{ t('login.username') }}</span>
          <input
            v-model="username"
            type="text"
            autocomplete="username"
            autofocus
            :placeholder="t('login.username')"
            :disabled="submitting"
          />
        </label>
        <label class="field">
          <span>{{ t('login.password') }}</span>
          <input
            v-model="password"
            type="password"
            autocomplete="current-password"
            :placeholder="t('login.password')"
            :disabled="submitting"
          />
        </label>
        <div v-if="errorMsg" class="error">
          <AlertCircle :size="14" />
          <span>{{ errorMsg }}</span>
        </div>
        <button type="submit" class="btn btn-primary" :disabled="submitting">
          <LogIn :size="14" />
          <span>{{ submitting ? t('login.submitting') : t('login.submit') }}</span>
        </button>
      </form>
    </div>
  </div>
</template>

<style scoped>
.login-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100vh;
  background: var(--bg);
}
.login-card {
  width: 360px;
  padding: 28px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: 0 4px 24px rgba(0, 0, 0, 0.08);
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-bottom: 6px;
}
.brand .logo {
  font-size: 26px;
}
.brand h1 {
  font-size: 16px;
  margin: 0;
  color: var(--text);
}
.hint {
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 18px;
}
.field {
  display: block;
  margin-bottom: 12px;
}
.field span {
  display: block;
  font-size: 12px;
  color: var(--text-muted);
  margin-bottom: 4px;
}
.field input {
  width: 100%;
  padding: 8px 10px;
  background: var(--bg);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: 6px;
  font-size: 14px;
  outline: none;
}
.field input:focus {
  border-color: var(--accent);
}
.error {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  color: var(--danger);
  background: var(--danger-bg, #fef2f2);
  padding: 6px 8px;
  border-radius: 4px;
  margin-bottom: 12px;
}
.btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 8px 14px;
  font-size: 13px;
  border-radius: 6px;
  border: 1px solid var(--border);
  background: var(--bg);
  color: var(--text);
  cursor: pointer;
  transition: background 0.1s;
}
.btn-primary {
  background: var(--accent);
  color: #fff;
  border-color: var(--accent);
}
.btn:hover:not(:disabled) {
  opacity: 0.9;
}
.btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
