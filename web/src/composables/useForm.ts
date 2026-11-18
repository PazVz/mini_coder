import { onUnmounted, ref, shallowRef } from 'vue'
import { APIError } from '../api/client'

export function useForm() {
  const pending = ref(false)
  const error = shallowRef<Error | null>(null)
  const remaining = ref(0)
  let timer: ReturnType<typeof setInterval> | undefined

  function coolDown(seconds: number) {
    clearInterval(timer)
    const deadline = Date.now() + seconds * 1000
    const update = () => {
      remaining.value = Math.max(0, Math.ceil((deadline - Date.now()) / 1000))
      if (remaining.value === 0) clearInterval(timer)
    }
    update()
    timer = setInterval(update, 1000)
  }

  async function submit(action: () => Promise<void>) {
    if (pending.value || remaining.value > 0) return
    pending.value = true
    error.value = null
    try {
      await action()
    } catch (cause) {
      error.value = cause instanceof Error ? cause : new Error('操作失败，请重试。')
      if (cause instanceof APIError && cause.retryAfter > 0) coolDown(cause.retryAfter)
    } finally {
      pending.value = false
    }
  }

  onUnmounted(() => clearInterval(timer))
  return { pending, error, remaining, submit, coolDown }
}
