import { beforeEach, describe, expect, it } from 'vitest'
import {
  captureEmailProof,
  discardEmailProofExcept,
  rememberEmailProof,
  useEmailProof,
} from './emailProof'

beforeEach(() => {
  history.replaceState(null, '', '/')
  captureEmailProof()
  localStorage.clear()
  sessionStorage.clear()
})

describe('email proof handling', () => {
  it('removes the initial fragment and keeps its value only until the page leaves', () => {
    history.replaceState(null, '', '/auth/register#token=secret-proof')
    captureEmailProof()
    expect(location.hash).toBe('')
    const { token } = useEmailProof('/auth/register')
    expect(token.value).toBe('secret-proof')
    discardEmailProofExcept('/auth/register')
    expect(token.value).toBe('secret-proof')
    discardEmailProofExcept('/auth/login')
    expect(token.value).toBe('')
    expect(localStorage.length).toBe(0)
    expect(sessionStorage.length).toBe(0)
  })

  it('updates an already mounted registration page when another mail link opens', () => {
    const { token, clearProof } = useEmailProof('/auth/register')
    expect(token.value).toBe('')
    expect(rememberEmailProof('/auth/register', '#token=new-proof')).toBe(true)
    expect(token.value).toBe('new-proof')
    clearProof()
    expect(token.value).toBe('')
  })

  it('does not expose a registration proof on the confirmation page or consume an anchor', () => {
    rememberEmailProof('/auth/register', '#token=secret-proof')
    expect(useEmailProof('/auth/email/confirm').token.value).toBe('')
    expect(rememberEmailProof('/auth/register', '#main')).toBe(false)
    expect(useEmailProof('/auth/register').token.value).toBe('secret-proof')
  })

  it('does not treat query parameters as proofs', () => {
    history.replaceState(null, '', '/auth/register?token=query-proof')
    captureEmailProof()
    expect(useEmailProof('/auth/register').token.value).toBe('')
  })
})
