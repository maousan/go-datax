import { clearToken, getToken } from '../auth/storage'

export const UNAUTHORIZED_EVENT = 'datax:unauthorized'

interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
  }
}

interface RequestOptions {
  /** 是否附带 Bearer token 并在 401 时清理会话。登录请求应设为 false。 */
  authenticated?: boolean
}

export class ApiError extends Error {
  readonly code: string
  readonly status: number

  constructor(code: string, message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.code = code
    this.status = status
  }
}

async function apiRequest<T>(
  method: string,
  path: string,
  body?: unknown,
  options: RequestOptions = {},
): Promise<T> {
  const { authenticated = true } = options

  const headers: Record<string, string> = { Accept: 'application/json' }
  const token = authenticated ? getToken() : null
  if (token) {
    headers.Authorization = `Bearer ${token}`
  }

  let payload: string | undefined
  if (body !== undefined) {
    headers['Content-Type'] = 'application/json'
    payload = JSON.stringify(body)
  }

  const response = await fetch(new URL(path, window.location.origin), {
    method,
    headers,
    body: payload,
  })

  if (response.status === 401 && token) {
    clearToken()
    window.dispatchEvent(new Event(UNAUTHORIZED_EVENT))
  }

  if (!response.ok) {
    const errorBody = (await response.json().catch(() => null)) as ErrorEnvelope | null
    throw new ApiError(
      errorBody?.error?.code ?? 'request_failed',
      errorBody?.error?.message ?? `请求失败（HTTP ${response.status}）`,
      response.status,
    )
  }

  if (response.status === 204) {
    return undefined as T
  }
  return (await response.json()) as T
}

export function apiGet<T>(path: string): Promise<T> {
  return apiRequest<T>('GET', path)
}

export function apiPost<T>(
  path: string,
  body: unknown,
  options?: RequestOptions,
): Promise<T> {
  return apiRequest<T>('POST', path, body, options)
}
