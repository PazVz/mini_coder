<script setup lang="ts">
import { onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { auth } from '../api/auth'
import { APIError } from '../api/client'
import { useEmailProof } from '../emailProof'
import { useForm } from '../composables/useForm'
import FormFeedback from '../components/FormFeedback.vue'

const { token, clearProof } = useEmailProof('/auth/register')
const needsSetup = ref<boolean | null>(null)
const email = ref('')
const username = ref('')
const password = ref('')
const sent = ref(false)
const registered = ref(false)
const { pending, error, remaining, submit } = useForm()
const {
  pending: sending,
  error: sendError,
  remaining: sendRemaining,
  submit: submitSend,
  coolDown,
} = useForm()
function load() {
  return submit(async () => {
    needsSetup.value = (await auth.setupStatus()).needs_setup
  })
}
function send() {
  return submitSend(async () => {
    try {
      await auth.requestRegistration(email.value)
      sent.value = true
      coolDown(60)
    } catch (cause) {
      if (cause instanceof APIError && cause.code === 'email_delivery_failed') coolDown(60)
      throw cause
    }
  })
}
function register() {
  return submit(async () => {
    await auth.register({ token: token.value, username: username.value, password: password.value })
    clearProof()
    password.value = ''
    registered.value = true
  })
}
function restart() {
  clearProof()
  password.value = ''
  error.value = null
  sent.value = false
  sendError.value = null
}
watch(token, (value) => {
  if (value) {
    registered.value = false
    sent.value = false
    sendError.value = null
    password.value = ''
    error.value = null
  }
})
onMounted(load)
</script>

<template>
  <div class="card">
    <p class="eyebrow">MAKE YOURSELF AT HOME</p>
    <h2>{{ registered ? '账号已准备好' : token ? '完成注册' : '创建你的账号' }}</h2>
    <p class="subtitle">
      {{
        token
          ? '邮箱链接已打开，设置用户名和密码即可完成注册。'
          : '先验证邮箱，再开启你的 mini_coder 之旅。'
      }}
    </p>
    <ol class="steps" aria-label="注册进度">
      <li :class="{ current: !token && !registered }">1 验证邮箱</li>
      <li :class="{ current: token }">2 创建账号</li>
      <li :class="{ current: registered }">3 登录</li>
    </ol>
    <FormFeedback
      :error="token ? error : sendError || error"
      :success="
        registered
          ? '注册成功，邮箱已验证。请使用用户名和密码登录。'
          : sent
            ? '验证邮件已提交，请查收邮件并打开最新链接。链接 30 分钟内有效。'
            : undefined
      "
    />
    <RouterLink v-if="registered" class="button primary" to="/auth/login">前往登录</RouterLink>
    <template v-else-if="needsSetup">
      <p class="notice">此实例尚未初始化，请联系部署者完成管理员初始化。</p>
      <RouterLink class="text-link" to="/auth/setup">前往初始化</RouterLink>
    </template>
    <template v-else-if="needsSetup === false">
      <form v-if="token" @submit.prevent="register" :aria-busy="pending">
        <label for="register-username">用户名</label>
        <input
          id="register-username"
          v-model="username"
          autocomplete="username"
          pattern="[a-z0-9][a-z0-9_\-]{2,31}"
          maxlength="32"
          autocapitalize="none"
          spellcheck="false"
          required
        />
        <p class="hint">3～32 位小写字母、数字、下划线或连字符，以字母或数字开头。</p>
        <label for="register-password">密码</label>
        <input
          id="register-password"
          v-model="password"
          type="password"
          autocomplete="new-password"
          minlength="12"
          required
        />
        <p class="hint">至少 12 个字符。</p>
        <button class="primary" :disabled="pending || remaining > 0">
          {{ pending ? '正在注册…' : remaining > 0 ? remaining + ' 秒后重试' : '完成注册' }}
        </button>
      </form>
      <form v-else @submit.prevent="send" :aria-busy="sending">
        <label for="register-email">邮箱</label>
        <input
          id="register-email"
          v-model="email"
          type="email"
          autocomplete="email"
          maxlength="254"
          required
        />
        <p class="hint">请输入你能接收验证邮件的邮箱。</p>
        <button class="primary" :disabled="sending || sendRemaining > 0">
          {{
            sending
              ? '正在发送…'
              : sendRemaining > 0
                ? sendRemaining + ' 秒后可重新发送'
                : sent
                  ? '重新发送验证邮件'
                  : '发送验证邮件'
          }}
        </button>
      </form>
      <button v-if="token" class="text-button" :disabled="pending" @click="restart">
        链接失效了？重新申请邮件
      </button>
    </template>
    <button v-else-if="error" class="secondary" @click="load">重新连接</button>
    <p v-else role="status">正在检查服务状态…</p>
    <p class="form-footer">已有账号？<RouterLink to="/auth/login">前往登录</RouterLink></p>
  </div>
</template>
