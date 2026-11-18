import { post, request } from './client'

export interface User {
  id: string
  username: string
  email: string
  email_verified: boolean
  role: 'admin' | 'member'
  status: 'active' | 'disabled'
  created_at: string
  updated_at: string
}

export interface Credentials {
  username: string
  password: string
}

export const auth = {
  setupStatus: () => request<{ needs_setup: boolean }>('/auth/setup'),
  setup: (token: string, input: Credentials & { email: string }) =>
    post<User>('/auth/setup', input, { 'X-Setup-Token': token }),
  login: (input: Credentials) => post<User>('/auth/login', input),
  me: () => request<User>('/auth/me'),
  logout: () => post<void>('/auth/logout', {}),
  requestRegistration: (email: string) =>
    post<{ message: string }>('/auth/register/request', { email }),
  register: (input: Credentials & { token: string }) => post<User>('/auth/register', input),
  verifyEmail: () => post<{ message: string }>('/auth/email/verification', {}),
  changeEmail: (email: string, current_password: string) =>
    post<{ message: string }>('/auth/email/change', { email, current_password }),
  confirmEmail: (token: string) => post<User>('/auth/email/confirm', { token }),
}
