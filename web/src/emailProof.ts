import { computed, shallowRef } from 'vue'

const proof = shallowRef<{ path: string; token: string } | null>(null)

// Both initial loads and same-document mail links use the same in-memory proof.
export function rememberEmailProof(path: string, hash: string): boolean {
  if (!['/auth/register', '/auth/email/confirm'].includes(path)) return false
  const parameters = new URLSearchParams(hash.slice(1))
  if (!parameters.has('token')) return false
  proof.value = { path, token: parameters.get('token') || '' }
  return true
}

// Run before creating history so the router never stores the initial fragment.
export function captureEmailProof() {
  proof.value = null
  if (rememberEmailProof(location.pathname, location.hash)) {
    history.replaceState(history.state, '', location.pathname + location.search)
  }
}

export function discardEmailProofExcept(path: string) {
  if (proof.value?.path !== path) proof.value = null
}

export function useEmailProof(path: string) {
  return {
    token: computed(() => (proof.value?.path === path ? proof.value.token : '')),
    clearProof: () => {
      proof.value = null
    },
  }
}
