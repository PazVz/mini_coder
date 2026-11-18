<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { auth } from '../api/auth'
import { APIError } from '../api/client'
import { useSession } from '../composables/useSession'
import { useForm } from '../composables/useForm'
import FormFeedback from '../components/FormFeedback.vue'

const router = useRouter()
const ready = ref(false)
const email = ref('')
const password = ref('')
const message = ref('')
const { user, refresh, logout } = useSession()
const { pending, error, submit } = useForm()
const { pending: sending, error: emailError, remaining, submit: submitEmail, coolDown } = useForm()
function load() {
  return submit(async () => {
    if (!(await refresh())) {
      await router.replace('/auth/login')
      return
    }
    ready.value = true
  })
}
function signOut() {
  return submit(async () => {
    await logout()
    await router.replace('/auth/login')
  })
}
function sendEmail(change: boolean) {
  return submitEmail(async () => {
    message.value = ''
    try {
      if (change) await auth.changeEmail(email.value, password.value)
      else await auth.verifyEmail()
      password.value = ''
      coolDown(60)
      message.value = change
        ? '验证邮件已发送到新邮箱。确认前，当前邮箱保持不变。'
        : '验证邮件已提交，请打开最新邮件并确认。'
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 401) {
        await refresh()
        await router.replace('/auth/login')
        return
      }
      if (cause instanceof APIError && cause.code === 'email_delivery_failed') coolDown(60)
      throw cause
    }
  })
}
onMounted(load)
</script>

<template>
  <div class="card account-card">
    <p class="eyebrow">YOUR ACCOUNT</p>
    <h2>我的账号</h2>
    <p class="subtitle">管理你的身份与联系邮箱。</p>
    <FormFeedback :error="error" />
    <template v-if="ready && user">
      <div class="profile-heading">
        <span class="avatar" aria-hidden="true">{{ user.username.slice(0, 1).toUpperCase() }}</span>
        <div>
          <strong>{{ user.username }}</strong>
          <p class="hint">{{ user.role === 'admin' ? '管理员' : '普通用户' }}</p>
        </div>
        <span class="badge">已登录</span>
      </div>
      <dl class="account-details">
        <dt>主邮箱</dt>
        <dd>
          {{ user.email || '尚未设置' }}
          <span class="email-state" :class="{ verified: user.email_verified }">{{
            user.email_verified ? '已验证' : '未验证'
          }}</span>
        </dd>
        <dt>账号状态</dt>
        <dd>{{ user.status === 'active' ? '正常' : '已停用' }}</dd>
      </dl>
      <div class="account-actions">
        <button class="secondary" :disabled="pending" @click="load">刷新资料</button
        ><button class="secondary" :disabled="pending" @click="signOut">退出登录</button>
      </div>
      <section class="email-settings" aria-labelledby="email-settings-title">
        <h3 id="email-settings-title">邮箱设置</h3>
        <FormFeedback :error="emailError" :success="message" />
        <p v-if="remaining > 0" class="hint" role="status">{{ remaining }} 秒后可再次申请邮件。</p>
        <button
          v-if="user.email && !user.email_verified"
          class="secondary"
          :disabled="sending || remaining > 0"
          @click="sendEmail(false)"
        >
          验证当前邮箱
        </button>
        <details>
          <summary>{{ user.email ? '更换邮箱' : '设置邮箱' }}</summary>
          <form @submit.prevent="sendEmail(true)" :aria-busy="sending">
            <label for="change-email">新邮箱</label
            ><input
              id="change-email"
              v-model="email"
              type="email"
              autocomplete="email"
              maxlength="254"
              required
            />
            <label for="change-password">当前密码</label
            ><input
              id="change-password"
              v-model="password"
              type="password"
              autocomplete="current-password"
              required
            />
            <p class="hint">确认新邮箱后才会更新账号资料。</p>
            <button class="primary" :disabled="sending || remaining > 0">
              {{ sending ? '正在发送…' : '发送换址验证邮件' }}
            </button>
          </form>
        </details>
      </section>
    </template>
    <button v-else-if="error" class="secondary" @click="load">重新连接</button>
    <p v-else role="status">正在读取账号信息…</p>
  </div>
</template>
