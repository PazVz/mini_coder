import { readonly, shallowRef } from 'vue'
import { auth, type User } from '../api/auth'
import { APIError } from '../api/client'

const user = shallowRef<User | null>(null)

export function useSession() {
  async function refresh() {
    try {
      user.value = await auth.me()
    } catch (error) {
      if (!(error instanceof APIError) || error.status !== 401) throw error
      user.value = null
    }
    return user.value
  }

  async function logout() {
    await auth.logout()
    user.value = null
  }

  return {
    user: readonly(user),
    refresh,
    logout,
    setUser: (value: User) => {
      user.value = value
    },
  }
}
