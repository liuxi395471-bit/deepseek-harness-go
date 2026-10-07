<script setup lang="ts">
// LoginView — 登录页（dsh 桌面端风格）

import { ref } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { LogIn, AlertCircle, Zap } from 'lucide-vue-next'
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
        <span class="brand-mark"><Zap :size="20" /></span>
        <div class="brand-text">
          <h1>{{ t('app.brand') }}</h1>
          <p class="hint">{{ t('login.hint') }}</p>
        </div>
      </div>
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
          <AlertCircle :size="13" />
          <span>{{ errorMsg }}</span>
        </div>
        <button type="submit" class="btn btn-primary submit-btn" :disabled="submitting">
          <LogIn :size="13" />
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
  padding: 20px;
}
.login-card {
  width: 360px;
  padding: 22px;
  background: var(--bg-card);
  border: 1px solid var(--border);
  border-radius: 12px;
  box-shadow: var(--shadow-lg);
}
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-bottom: 16px;
}
.brand-mark {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 36px;
  height: 36px;
  border-radius: 8px;
  background: linear-gradient(135deg, var(--accent), var(--accent-hover));
  color: #fff;
  flex-shrink: 0;
}
.brand-text { flex: 1; }
.brand-text h1 {
  font-size: 14px;
  margin: 0;
  color: var(--text);
  font-weight: 600;
}
.hint {
  font-size: 11px;
  color: var(--text-faint);
  margin: 2px 0 0;
}
.field {
  display: block;
  margin-bottom: 10px;
}
.field span {
  display: block;
  font-size: 11px;
  color: var(--text-faint);
  margin-bottom: 4px;
  font-weight: 500;
}
.field input {
  width: 100%;
  padding: 7px 10px;
  background: var(--bg-elevated);
  color: var(--text);
  border: 1px solid var(--border);
  border-radius: var(--ds-radius-sm);
  font-size: 13px;
  outline: none;
}
.field input:focus {
  border-color: var(--accent);
  box-shadow: 0 0 0 2px color-mix(in srgb, var(--accent) 25%, transparent);
}
.error {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--danger);
  background: var(--danger-bg);
  padding: 6px 8px;
  border-radius: var(--ds-radius-sm);
  margin-bottom: 10px;
  border: 1px solid color-mix(in srgb, var(--danger) 30%, transparent);
}
.submit-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  width: 100%;
  padding: 8px 14px;
  font-size: 13px;
  font-weight: 600;
  border-radius: var(--ds-radius-sm);
  border: 1px solid var(--accent);
  background: var(--accent);
  color: #fff;
  cursor: pointer;
  margin-top: 4px;
  transition: background var(--ds-duration-fast) var(--ds-ease-in-out);
}
.submit-btn:hover:not(:disabled) { background: var(--accent-hover); }
.submit-btn:disabled { opacity: 0.5; cursor: not-allowed; }
</style>
