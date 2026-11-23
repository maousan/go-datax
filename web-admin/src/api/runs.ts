import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getToken } from '../auth/storage'
import { ApiError, apiGet } from './client'

export type RunStatus = 'running' | 'succeeded' | 'failed' | 'canceled'

export interface Run {
  id: number
  syncJobId: number | null
  syncJobName: string
  status: RunStatus
  trigger: string
  startedAt: string
  finishedAt: string | null
  exitCode: number | null
  errorSummary: string
  logSize: number
  createdAt: string
}

export interface RunList {
  items: Run[]
  total: number
  page: number
  pageSize: number
}

export interface RunFilters {
  jobId?: number
  status?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

function runQuery(filters: RunFilters): string {
  const params = new URLSearchParams()
  if (filters.jobId) params.set('jobId', String(filters.jobId))
  if (filters.status) params.set('status', filters.status)
  if (filters.from) params.set('from', filters.from)
  if (filters.to) params.set('to', filters.to)
  params.set('page', String(filters.page ?? 1))
  params.set('pageSize', String(filters.pageSize ?? 20))
  return params.toString()
}

export function useRuns(filters: RunFilters) {
  return useQuery({
    queryKey: ['runs', filters],
    queryFn: () => apiGet<RunList>(`/api/v1/runs?${runQuery(filters)}`),
    refetchInterval: (query) =>
      query.state.data?.items.some((run) => run.status === 'running') ? 2000 : false,
  })
}

export function useRun(id: number) {
  return useQuery({
    queryKey: ['run', id],
    queryFn: () => apiGet<Run>(`/api/v1/runs/${id}`),
    enabled: id > 0,
    refetchInterval: (query) => (query.state.data?.status === 'running' ? 2000 : false),
  })
}

export function useCancelRun() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiFetch(`/api/v1/runs/${id}/cancel`, { method: 'POST' }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      queryClient.invalidateQueries({ queryKey: ['run'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
  })
}

export interface Overview {
  dataSourceCount: number
  syncJobCount: number
  windowHours: number
  totalRuns: number
  successRate: number
  recentRuns: Run[]
}

export function useOverview(hours = 24) {
  return useQuery({
    queryKey: ['overview', hours],
    queryFn: () => apiGet<Overview>(`/api/v1/overview?hours=${hours}`),
    refetchInterval: 5000,
  })
}

async function apiFetch(path: string, init: RequestInit): Promise<unknown> {
  const token = getToken()
  const headers: Record<string, string> = { Accept: 'application/json' }
  if (token) headers.Authorization = `Bearer ${token}`
  const response = await fetch(new URL(path, window.location.origin), { ...init, headers })
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as {
      error?: { code?: string; message?: string }
    } | null
    throw new ApiError(
      body?.error?.code ?? 'request_failed',
      body?.error?.message ?? `请求失败（HTTP ${response.status}）`,
      response.status,
    )
  }
  if (response.status === 204) return undefined
  return response.json()
}

export async function downloadRunArtifact(id: number, kind: 'log' | 'snapshot'): Promise<void> {
  const token = getToken()
  const path = kind === 'log' ? `/api/v1/runs/${id}/log` : `/api/v1/runs/${id}/snapshot`
  const response = await fetch(new URL(path, window.location.origin), {
    headers: token ? { Authorization: `Bearer ${token}` } : {},
  })
  if (!response.ok) {
    throw new ApiError('download_failed', `下载失败（HTTP ${response.status}）`, response.status)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = kind === 'log' ? `run-${id}.log` : `run-${id}.json`
  anchor.click()
  URL.revokeObjectURL(url)
}

export interface RunEvent {
  type: 'log' | 'status' | 'end' | 'ping'
  line?: string
  seq?: number
  status?: RunStatus
  exitCode?: number
  finishedAt?: string
}

export async function streamRunLogs(
  id: number,
  offset: number,
  onEvent: (event: RunEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const token = getToken()
  const response = await fetch(
    new URL(`/api/v1/runs/${id}/logs/stream?offset=${offset}`, window.location.origin),
    {
      headers: {
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      signal,
    },
  )
  if (!response.ok || !response.body) {
    throw new ApiError('stream_failed', `读取日志失败（HTTP ${response.status}）`, response.status)
  }

  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  while (true) {
    const { done, value } = await reader.read()
    if (done) return
    buffer += decoder.decode(value, { stream: true })
    let index = buffer.indexOf('\n\n')
    while (index >= 0) {
      const chunk = buffer.slice(0, index)
      buffer = buffer.slice(index + 2)
      const dataLine = chunk.split('\n').find((line) => line.startsWith('data: '))
      if (dataLine) {
        try {
          onEvent(JSON.parse(dataLine.slice(6)) as RunEvent)
        } catch {
          // 忽略无法解析的片段
        }
      }
      index = buffer.indexOf('\n\n')
    }
  }
}
