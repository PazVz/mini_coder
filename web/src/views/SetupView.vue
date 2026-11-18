<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { auth } from '../api/auth'
import { useForm } from '../composables/useForm'
import FormFeedback from '../components/FormFeedback.vue'

const needsSetup = ref<boolean | null>(null)
const complete = ref(false)
const token = ref('')
const username = ref('')
const email = ref('')
const password = ref('')
const { pending, error, remaining, submit } = useForm()
function load() {
  return submit(async () => {
    needsSetup.value = (await auth.setupStatus()).needs_setup
  })
}
function setup() {
  return submit(async () => {
    await auth.setup(token.value, {
      username: username.value,
      email: email.value,
      password: password.value,
    })
    token.value = ''
    password.value = ''
    complete.value = true
  })
}
onMounted(load)
</script>

<template>
  <div class="card">
    <p class="eyebrow">FIRST THINGS FIRST</p>
    <h2>初始化管理员</h2>
    <p class="subtitle">首次使用时，创建管理员账号。完成后即可开放用户注册。</p>
    <FormFeedback :error="error" :success="complete ? '管理员已创建，请登录。' : undefined" />
    <template v-if="complete || needsSetup === false">
      <p v-if="!complete" class="notice" role="status">此实例已完成初始化。</p>
      <RouterLink class="button primary" to="/auth/login">前往登录</RouterLink>
    </template>
    <form v-else-if="needsSetup" @submit.prevent="setup" :aria-busy="pending">
      <label for="setup-token">初始化令牌</label>
      <input id="setup-token" v-model="token" type="password" autocomplete="off" required />
      <p class="hint">由部署者提供，仅用于这次管理员初始化。</p>
      <label for="setup-username">用户名</label>
      <input
        id="setup-username"
        v-model="username"
        autocomplete="username"
        pattern="[a-z0-9][a-z0-9_\-]{2,31}"
        maxlength="32"
        autocapitalize="none"
        spellcheck="false"
        required
      />
      <p class="hint">3～32 位小写字母、数字、下划线或连字符，以字母或数字开头。</p>
      <label for="setup-email">邮箱</label>
      <input
        id="setup-email"
        v-model="email"
        type="email"
        autocomplete="email"
        maxlength="254"
        required
      />
      <label for="setup-password">密码</label>
      <input
        id="setup-password"
        v-model="password"
        type="password"
        autocomplete="new-password"
        minlength="12"
        required
      />
      <p class="hint">至少 12 个字符。</p>
      <button class="primary" :disabled="pending || remaining > 0">
        {{ pending ? '正在创建…' : remaining > 0 ? remaining + ' 秒后重试' : '创建管理员' }}
      </button>
    </form>
    <button v-else-if="error" class="secondary" @click="load">重新连接</button>
    <p v-else role="status">正在检查初始化状态…</p>
  </div>
</template>
