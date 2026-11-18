<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { auth } from '../api/auth'
import { APIError } from '../api/client'
import { useEmailProof } from '../emailProof'
import { useSession } from '../composables/useSession'
import { useForm } from '../composables/useForm'
import LoginForm from '../components/LoginForm.vue'
import FormFeedback from '../components/FormFeedback.vue'

const { token, clearProof } = useEmailProof('/auth/email/confirm')
const ready = ref(false)
const complete = ref(false)
const { user, refresh, logout, setUser } = useSession()
const { pending, error, remaining, submit } = useForm()
function load() {
  return submit(async () => {
    await refresh()
    ready.value = true
  })
}
function confirm() {
  return submit(async () => {
    try {
      setUser(await auth.confirmEmail(token.value))
      clearProof()
      complete.value = true
    } catch (cause) {
      if (cause instanceof APIError && cause.status === 401) await refresh()
      throw cause
    }
  })
}
function switchAccount() {
  return submit(async () => {
    await logout()
  })
}
watch(token, (value) => {
  if (value) {
    complete.value = false
    error.value = null
    void load()
  }
})
onMounted(() => {
  if (token.value) void load()
})
</script>

<template>
  <div class="card">
    <p class="eyebrow">KEEP IN TOUCH</p>
    <h2>确认你的邮箱</h2>
    <p class="subtitle">登录对应账号后，确认这次邮箱验证或修改。</p>
    <FormFeedback :error="error" :success="complete ? '邮箱已确认，账号资料已更新。' : undefined" />
    <RouterLink v-if="complete" class="button primary" to="/account">查看我的账号</RouterLink>
    <template v-else-if="!token">
      <p class="notice" role="status">
        链接缺少验证信息，请从邮件重新打开；也可以前往账号页面重新申请。
      </p>
      <RouterLink class="button secondary" to="/account">前往我的账号</RouterLink>
    </template>
    <template v-else-if="ready">
      <template v-if="user">
        <p class="notice">
          当前登录：<strong>{{ user.username }}</strong
          >。请确认这是接收邮件的账号。
        </p>
        <button class="primary" :disabled="pending || remaining > 0" @click="confirm">
          {{ pending ? '正在确认…' : remaining > 0 ? remaining + ' 秒后重试' : '确认邮箱' }}
        </button>
        <button class="text-button" :disabled="pending || remaining > 0" @click="switchAccount">
          切换账号
        </button>
      </template>
      <template v-else>
        <p class="notice">请在此登录，登录后继续确认邮箱。</p>
        <LoginForm @authenticated="error = null" />
      </template>
    </template>
    <button v-else-if="error" class="secondary" @click="load">重新连接</button>
    <p v-else role="status">正在检查登录状态…</p>
  </div>
</template>
