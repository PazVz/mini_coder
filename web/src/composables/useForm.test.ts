import { createApp, defineComponent } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import { APIError } from '../api/client'
import { useForm } from './useForm'

afterEach(() => vi.useRealTimers())

it('honors Retry-After, prevents duplicate submissions, and cleans up the timer', async () => {
  vi.useFakeTimers()
  let form!: ReturnType<typeof useForm>
  const root = document.createElement('div')
  const app = createApp(
    defineComponent({
      setup() {
        form = useForm()
        return () => null
      },
    }),
  )
  app.mount(root)
  try {
    await form.submit(async () => {
      throw new APIError('稍后重试', 429, 'rate_limited', [], 3)
    })
    expect(form.remaining.value).toBe(3)
    const action = vi.fn(async () => {})
    await form.submit(action)
    expect(action).not.toHaveBeenCalled()
    vi.advanceTimersByTime(3000)
    await form.submit(action)
    expect(action).toHaveBeenCalledOnce()
    let finish!: () => void
    const pending = form.submit(
      () =>
        new Promise<void>((resolve) => {
          finish = resolve
        }),
    )
    await form.submit(action)
    expect(action).toHaveBeenCalledOnce()
    finish()
    await pending
    form.coolDown(60)
  } finally {
    app.unmount()
  }
  expect(vi.getTimerCount()).toBe(0)
})
