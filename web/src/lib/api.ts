const BASE = '/api/v1'

export class ApiError extends Error {
  status: number
  constructor(method: string, path: string, status: number) {
    super(`${method} ${path} -> ${status}`)
    this.name = 'ApiError'
    this.status = status
  }
}

/** Human, specific copy for a failed login attempt. */
export function loginErrorMessage(e: unknown): string {
  if (e instanceof ApiError && e.status === 401) return 'Incorrect password'
  return 'Can\'t reach the server — try again'
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const res = await fetch(BASE + path, {
    method,
    credentials: 'include',
    headers: body ? { 'Content-Type': 'application/json' } : undefined,
    body: body ? JSON.stringify(body) : undefined,
  })
  if (!res.ok) throw new ApiError(method, path, res.status)
  const text = await res.text()
  return (text ? JSON.parse(text) : null) as T
}

export const api = {
  get: <T>(p: string) => request<T>('GET', p),
  post: <T>(p: string, b?: unknown) => request<T>('POST', p, b),
  put: <T>(p: string, b?: unknown) => request<T>('PUT', p, b),
  del: <T>(p: string) => request<T>('DELETE', p),
}
