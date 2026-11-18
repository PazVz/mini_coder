export interface FieldError {
  field: string
  code: string
  detail: string
}

export class APIError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly code: string,
    readonly validations: FieldError[] = [],
    readonly retryAfter = 0,
  ) {
    super(message)
    this.name = 'APIError'
  }
}

export async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  let response: Response
  try {
    response = await fetch('/api/v1' + path, {
      ...options,
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json', ...options.headers },
    })
  } catch {
    throw new Error('无法连接服务，请检查网络后重试。')
  }
  if (response.status === 204) return undefined as T

  let data
  try {
    data = await response.json()
  } catch {
    throw new Error('服务返回了无法识别的响应，请稍后重试。')
  }
  if (!response.ok) {
    const retryAfter = Number(response.headers.get('Retry-After'))
    throw new APIError(
      data.message || '操作失败，请稍后重试。',
      response.status,
      data.code || 'unknown_error',
      data.validations || [],
      Number.isFinite(retryAfter) ? Math.max(0, retryAfter) : 0,
    )
  }
  return data as T
}

export function post<T>(path: string, body: unknown, headers?: Record<string, string>) {
  return request<T>(path, { method: 'POST', body: JSON.stringify(body), headers })
}
