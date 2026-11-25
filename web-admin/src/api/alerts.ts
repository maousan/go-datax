import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type AlertChannelType = 'email' | 'dingtalk' | 'feishu' | 'wecom' | 'slack' | 'webhook'
export type AlertEvent = 'failed' | 'recovered' | 'succeeded' | 'canceled'
export type AlertLogStatus = 'success' | 'failed'
export type AlertSourceKind = 'datax' | 'pipeline'
export type AlertLogEvent = AlertEvent | 'test'

export interface AlertChannel {
  id: string
  name: string
  type: AlertChannelType
  enabled: boolean
  events: AlertEvent[]
  url?: string
  secret?: string
  smtpHost?: string
  smtpPort?: number
  smtpUsername?: string
  smtpPassword?: string
  from?: string
  to?: string[]
  useTLS?: boolean
}

export interface AlertConfig {
  enabled: boolean
  timeoutSeconds: number
  retentionDays: number
  channels: AlertChannel[]
}

export interface AlertLog {
  id: number
  sourceKind: AlertSourceKind
  event: AlertLogEvent
  status: AlertLogStatus
  channelId: string
  channelName: string
  channelType: AlertChannelType
  runId: number
  syncJobId: number | null
  syncJobName: string
  collectTaskId: number | null
  collectTaskName: string
  pipelineId: number | null
  pipelineName: string
  pipelineRunId: number | null
  versionNumber: number
  error: string
  durationMs: number
  createdAt: string
}

export interface AlertLogFilters {
  sourceKind?: AlertSourceKind
  event?: AlertLogEvent
  status?: AlertLogStatus
  channelType?: AlertChannelType
  query?: string
  from?: string
  to?: string
  page?: number
  pageSize?: number
}

export interface AlertLogList {
  items: AlertLog[]
  total: number
  page: number
  pageSize: number
}

export interface AlertTestInput {
  channel: AlertChannel
  timeoutSeconds?: number
}

const CONFIG_KEY = ['alerts', 'config'] as const
const LOGS_KEY = ['alerts', 'logs'] as const

export function useAlertConfig() {
  return useQuery({
    queryKey: CONFIG_KEY,
    queryFn: () => apiGet<AlertConfig>('/api/v1/alerts/config'),
  })
}

export function useUpdateAlertConfig() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: AlertConfig) =>
      apiPut<AlertConfig>('/api/v1/alerts/config', input),
    onSuccess: (data) => {
      queryClient.setQueryData(CONFIG_KEY, data)
    },
  })
}

export function useTestAlertChannel() {
  return useMutation({
    mutationFn: (input: AlertTestInput) =>
      apiPost<{ ok: boolean }>('/api/v1/alerts/config/test', input),
  })
}

function alertLogQuery(filters: AlertLogFilters): string {
  const params = new URLSearchParams()
  if (filters.sourceKind) params.set('sourceKind', filters.sourceKind)
  if (filters.event) params.set('event', filters.event)
  if (filters.status) params.set('status', filters.status)
  if (filters.channelType) params.set('channelType', filters.channelType)
  if (filters.query) params.set('query', filters.query)
  if (filters.from) params.set('from', filters.from)
  if (filters.to) params.set('to', filters.to)
  params.set('page', String(filters.page ?? 1))
  params.set('pageSize', String(filters.pageSize ?? 20))
  return params.toString()
}

export function useAlertLogs(filters: AlertLogFilters) {
  return useQuery({
    queryKey: [...LOGS_KEY, filters],
    queryFn: () => apiGet<AlertLogList>(`/api/v1/alerts/logs?${alertLogQuery(filters)}`),
    placeholderData: keepPreviousData,
    refetchInterval: 5000,
  })
}

export function useClearAlertLogs() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (before?: string) =>
      apiDelete<{ deleted: number }>(
        `/api/v1/alerts/logs${before ? `?before=${encodeURIComponent(before)}` : ''}`,
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: LOGS_KEY })
    },
  })
}
