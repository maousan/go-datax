interface ErrorEnvelope {
  error?: {
    code?: string
    message?: string
  }
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

export async function apiGet<T>(path: string): Promise<T> {
  const response = await fetch(new URL(path, window.location.origin), {
    headers: { Accept: 'application/json' },
  })

  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as ErrorEnvelope | null
    throw new ApiError(
      payload?.error?.code ?? 'request_failed',
      payload?.error?.message ?? `请求失败（HTTP ${response.status}）`,
      response.status,
    )
  }

  return (await response.json()) as T
}
