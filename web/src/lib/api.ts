const BASE = '/api/v1'

export class ApiError extends Error {
  status: number
  body: Record<string, unknown> | null
  constructor(method: string, path: string, status: number, body: Record<string, unknown> | null = null) {
    super(`${method} ${path} -> ${status}`)
    this.name = 'ApiError'
    this.status = status
    this.body = body
  }
}

/** Human, specific copy for a failed login attempt. */
export function loginErrorMessage(e: unknown): string {
  if (e instanceof ApiError && e.status === 401) return 'Incorrect username or password'
  return 'Can\'t reach the server — try again'
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) {
    const text = await res.text().catch(() => '')
    let errBody: Record<string, unknown> | null = null
    try { errBody = text ? (JSON.parse(text) as Record<string, unknown>) : null } catch { /* ignore */ }
    throw new ApiError(method, path, res.status, errBody)
  }
  const text = await res.text()
  return (text ? JSON.parse(text) : null) as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
  patch: <T>(p: string, b?: unknown) => request<T>('PATCH', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}
