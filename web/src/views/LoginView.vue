<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { auth } from '../api/auth'
import LoginForm from '../components/LoginForm.vue'
import FormFeedback from '../components/FormFeedback.vue'
import { useSession } from '../composables/useSession'
import { useForm } from '../composables/useForm'

const router = useRouter()
const ready = ref(false)
const { refresh } = useSession()
const { error, submit } = useForm()
function load() {
  return submit(async () => {
    if ((await auth.setupStatus()).needs_setup) {
      await router.replace('/auth/setup')
      return
    }
    if (await refresh()) {
      await router.replace('/account')
      return
    }
    ready.value = true
  })
}
onMounted(load)
</script>

<template>
  <div class="card">
    <p class="eyebrow">WELCOME BACK</p>
    <h2>欢迎回来</h2>
    <p class="subtitle">登录你的 mini_coder 账号。</p>
    <FormFeedback :error="error" />
    <button v-if="error" class="secondary" @click="load">重新连接</button>
    <LoginForm v-if="ready" @authenticated="router.replace('/account')" />
    <p v-else-if="!error" role="status">正在检查服务状态…</p>
    <p class="form-footer">还没有账号？<RouterLink to="/auth/register">使用邮箱注册</RouterLink></p>
  </div>
</template>
