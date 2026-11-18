<script setup lang="ts">
import { ref } from 'vue'
import { auth, type User } from '../api/auth'
import { useSession } from '../composables/useSession'
import { useForm } from '../composables/useForm'
import FormFeedback from './FormFeedback.vue'

const emit = defineEmits<{ authenticated: [user: User] }>()
const username = ref('')
const password = ref('')
const { setUser } = useSession()
const { pending, error, remaining, submit } = useForm()
function login() {
  return submit(async () => {
    const user = await auth.login({ username: username.value, password: password.value })
    password.value = ''
    setUser(user)
    emit('authenticated', user)
  })
}
</script>

<template>
  <form @submit.prevent="login" :aria-busy="pending">
    <label for="login-username">用户名</label>
    <input
      id="login-username"
      v-model="username"
      autocomplete="username"
      required
      maxlength="32"
      autocapitalize="none"
      spellcheck="false"
    />
    <label for="login-password">密码</label>
    <input
      id="login-password"
      v-model="password"
      type="password"
      autocomplete="current-password"
      required
    />
    <FormFeedback :error="error" />
    <button class="primary" :disabled="pending || remaining > 0">
      {{ pending ? '正在登录…' : remaining > 0 ? remaining + ' 秒后重试' : '登录' }}
    </button>
  </form>
</template>
