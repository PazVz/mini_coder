import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIError, post, request } from './client'

afterEach(() => vi.unstubAllGlobals())

describe('API requests', () => {
  it('sends JSON with same-origin cookies', async () => {
    const fetch = vi.fn().mockResolvedValue(Response.json({ username: 'alice' }))
    vi.stubGlobal('fetch', fetch)
    await expect(
      post('/auth/login', { username: 'alice', password: 'example-password' }),
    ).resolves.toEqual({ username: 'alice' })
    expect(fetch).toHaveBeenCalledWith(
      '/api/v1/auth/login',
      expect.objectContaining({
        method: 'POST',
        credentials: 'same-origin',
        body: JSON.stringify({ username: 'alice', password: 'example-password' }),
      }),
    )
  })

  it('accepts a successful logout without a JSON body', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(null, { status: 204 })))
    await expect(post('/auth/logout', {})).resolves.toBeUndefined()
  })

  it('preserves validation details and the server cooldown', async () => {
    const validations = [{ field: 'email', code: 'invalid_format', detail: '邮箱格式不正确' }]
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          Response.json(
            { code: 'rate_limited', message: '稍后重试', validations },
            { status: 429, headers: { 'Retry-After': '45' } },
          ),
        ),
    )
    await expect(request('/auth/register/request')).rejects.toMatchObject({
      status: 429,
      code: 'rate_limited',
      validations,
      retryAfter: 45,
    })
  })

  it('distinguishes expired sessions from network failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(
          Response.json({ code: 'unauthenticated', message: '请重新登录' }, { status: 401 }),
        ),
    )
    await expect(request('/auth/me')).rejects.toBeInstanceOf(APIError)
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')))
    await expect(request('/auth/me')).rejects.toThrow('无法连接服务')
  })

  it('handles an HTML proxy error without exposing the response', async () => {
    vi.stubGlobal(
      'fetch',
      vi
        .fn()
        .mockResolvedValue(new Response('<html>internal proxy details</html>', { status: 502 })),
    )
    await expect(request('/auth/me')).rejects.toThrow('服务返回了无法识别的响应')
  })
})
