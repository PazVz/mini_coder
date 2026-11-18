import { afterEach, expect, it, vi } from 'vitest'
import { useSession } from './useSession'
import type { User } from '../api/auth'

const alice: User = {
  id: '1',
  username: 'alice',
  email: 'alice@example.com',
  email_verified: true,
  role: 'member',
  status: 'active',
  created_at: '',
  updated_at: '',
}
afterEach(() => vi.unstubAllGlobals())

it('restores the current user from the server and clears expired sessions', async () => {
  const session = useSession()
  vi.stubGlobal(
    'fetch',
    vi
      .fn()
      .mockResolvedValueOnce(Response.json(alice))
      .mockResolvedValueOnce(
        Response.json({ message: '请重新登录', code: 'unauthenticated' }, { status: 401 }),
      ),
  )
  await session.refresh()
  expect(session.user.value).toEqual(alice)
  await session.refresh()
  expect(session.user.value).toBeNull()
})

it('reports a failed logout without claiming the session was revoked', async () => {
  const session = useSession()
  session.setUser(alice)
  vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
  await expect(session.logout()).rejects.toThrow('无法连接服务')
  expect(session.user.value).toEqual(alice)
  vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })))
  await session.logout()
  expect(session.user.value).toBeNull()
})
