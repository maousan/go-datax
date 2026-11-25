import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { getToken } from '../auth/storage'
import { ApiError, apiGet, apiPost, apiPut } from './client'
import type { ConnectorName, SchemaColumn } from './endpoints'
import type { RunStatus } from './runs'

export type PipelineRunStatus = RunStatus

export interface PipelineEndpointSummary {
  id: number
  name: string
  connector: ConnectorName
  dataSourceName: string
  table: string
  query: string
  path: string
  schema: SchemaColumn[]
  options: Record<string, string>
}

export interface PipelineVersionSummary {
  id: number
  versionNumber: number
  sourceName: string
  sinkName: string
  summary: string
  createdAt: string
}

export interface PipelineRun {
  id: number
  pipelineId: number | null
  versionId: number | null
  versionNumber: number
  pipelineName: string
  status: PipelineRunStatus
  trigger: string
  startedAt: string
  finishedAt: string | null
  readRecords: number
  writeRecords: number
  readBytes: number
  errorSummary: string
  createdAt: string
}

export interface Pipeline {
  id: number
  name: string
  description: string
  sourceId: number
  sinkId: number
  source: PipelineEndpointSummary | null
  sink: PipelineEndpointSummary | null
  draftVersionNumber: number
  latestVersion: PipelineVersionSummary | null
  lastRun: PipelineRun | null
  createdAt: string
  updatedAt: string
}

export interface PipelineInput {
  name: string
  description: string
  sourceId: number
  sinkId: number
}

export interface PipelineEndpointSnapshot {
  endpointId: number
  name: string
  connector: ConnectorName
  dataSourceId: number
  dataSourceName: string
  table: string
  query: string
  path: string
  schema: SchemaColumn[]
  options: Record<string, string>
}

export interface PipelineVersion {
  id: number
  pipelineId: number
  versionNumber: number
  sourceName: string
  sinkName: string
  summary: string
  source: PipelineEndpointSnapshot
  sink: PipelineEndpointSnapshot
  createdAt: string
}

export interface PipelineRunFilters {
  pipelineId?: number
  status?: string
  page?: number
  pageSize?: number
}

export interface PipelineRunList {
  items: PipelineRun[]
  total: number
  page: number
  pageSize: number
}

export interface PipelineRunEvent {
  type: 'log' | 'progress' | 'done' | 'ping'
  runId?: string
  message?: string
  status?: PipelineRunStatus
  readRecords?: number
  writeRecords?: number
  readBytes?: number
  error?: string
}

function runQuery(filters: PipelineRunFilters): string {
  const params = new URLSearchParams()
  if (filters.pipelineId) params.set('pipelineId', String(filters.pipelineId))
  if (filters.status) params.set('status', filters.status)
  params.set('page', String(filters.page ?? 1))
  params.set('pageSize', String(filters.pageSize ?? 20))
  return params.toString()
}

export function usePipelines(query: string) {
  return useQuery({
    queryKey: ['pipelines', query],
    queryFn: () => apiGet<Pipeline[]>(`/api/v1/pipelines?query=${encodeURIComponent(query)}`),
  })
}

export function usePipeline(id: number) {
  return useQuery({
    queryKey: ['pipeline', id],
    queryFn: () => apiGet<Pipeline>(`/api/v1/pipelines/${id}`),
    enabled: Number.isFinite(id) && id > 0,
  })
}

export function usePipelineVersions(id: number) {
  return useQuery({
    queryKey: ['pipeline-versions', id],
    queryFn: () => apiGet<PipelineVersion[]>(`/api/v1/pipelines/${id}/versions`),
    enabled: Number.isFinite(id) && id > 0,
  })
}

export function usePipelineRuns(filters: PipelineRunFilters) {
  return useQuery({
    queryKey: ['pipeline-runs', filters],
    queryFn: () => apiGet<PipelineRunList>(`/api/v1/pipeline-runs?${runQuery(filters)}`),
    refetchInterval: (query) =>
      query.state.data?.items.some((run) => run.status === 'running') ? 2000 : false,
  })
}

export function usePipelineRun(id: number) {
  return useQuery({
    queryKey: ['pipeline-run', id],
    queryFn: () => apiGet<PipelineRun>(`/api/v1/pipeline-runs/${id}`),
    enabled: Number.isFinite(id) && id > 0,
    refetchInterval: (query) =>
      query.state.data?.status === 'running' ? 2000 : false,
  })
}

function useInvalidatePipeline(id?: number) {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    if (id) queryClient.invalidateQueries({ queryKey: ['pipeline', id] })
  }
}

export function useCreatePipeline() {
  const invalidate = useInvalidatePipeline()
  return useMutation({
    mutationFn: (input: PipelineInput) => apiPost<Pipeline>('/api/v1/pipelines', input),
    onSuccess: invalidate,
  })
}

export function useUpdatePipeline(id: number) {
  const invalidate = useInvalidatePipeline(id)
  return useMutation({
    mutationFn: (input: PipelineInput) => apiPut<Pipeline>(`/api/v1/pipelines/${id}`, input),
    onSuccess: invalidate,
  })
}

export function usePublishPipeline(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiPost<PipelineVersion>(`/api/v1/pipelines/${id}/publish`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline', id] })
      queryClient.invalidateQueries({ queryKey: ['pipeline-versions', id] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export function useRunPipeline(id: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiPost<PipelineRun>(`/api/v1/pipelines/${id}/runs`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline', id] })
      queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export function useRunPipelineVersion() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (versionId: number) =>
      apiPost<PipelineRun>(`/api/v1/pipeline-versions/${versionId}/runs`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export function useRestorePipelineVersion(pipelineId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (versionId: number) =>
      apiPost<Pipeline>(
        `/api/v1/pipelines/${pipelineId}/versions/${versionId}/restore`,
        {},
      ),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline', pipelineId] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export function useCancelPipelineRun(runId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiPost(`/api/v1/pipeline-runs/${runId}/cancel`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline-run', runId] })
      queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      queryClient.invalidateQueries({ queryKey: ['pipeline'] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export function useRerunPipelineRun(runId: number) {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: () => apiPost<PipelineRun>(`/api/v1/pipeline-runs/${runId}/rerun`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
      queryClient.invalidateQueries({ queryKey: ['pipeline'] })
      queryClient.invalidateQueries({ queryKey: ['pipelines'] })
    },
  })
}

export async function streamPipelineRunEvents(
  id: number,
  offset: number,
  onEvent: (event: PipelineRunEvent) => void,
  signal: AbortSignal,
): Promise<void> {
  const token = getToken()
  const response = await fetch(
    new URL(`/api/v1/pipeline-runs/${id}/events?offset=${offset}`, window.location.origin),
    {
      headers: {
        Accept: 'text/event-stream',
        ...(token ? { Authorization: `Bearer ${token}` } : {}),
      },
      signal,
    },
  )
  if (!response.ok || !response.body) {
    throw new ApiError('stream_failed', `读取运行日志失败（HTTP ${response.status}）`, response.status)
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
          onEvent(JSON.parse(dataLine.slice(6)) as PipelineRunEvent)
        } catch {
          // 忽略无法解析的片段
        }
      }
      index = buffer.indexOf('\n\n')
    }
  }
}

export async function downloadPipelineRunLog(id: number): Promise<void> {
  const token = getToken()
  const response = await fetch(
    new URL(`/api/v1/pipeline-runs/${id}/log`, window.location.origin),
    { headers: token ? { Authorization: `Bearer ${token}` } : {} },
  )
  if (!response.ok) {
    throw new ApiError('download_failed', `下载日志失败（HTTP ${response.status}）`, response.status)
  }
  const blob = await response.blob()
  const url = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `pipeline-run-${id}.log`
  anchor.click()
  URL.revokeObjectURL(url)
}
