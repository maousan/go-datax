import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'
import type { PipelineRun } from './pipelines'

export type PipelineBlockingPolicy = 'discard' | 'overwrite' | 'queue'

export interface PipelineSchedule {
  id: number
  pipelineId: number
  pipelineName: string
  name: string
  cronExpr: string
  timezone: string
  enabled: boolean
  blockingPolicy: PipelineBlockingPolicy
  retryMax: number
  retryIntervalSeconds: number
  timeoutSeconds: number
  pinnedVersionId: number | null
  pinnedVersionNumber: number
  nextRunAt: string | null
  lastRun: PipelineRun | null
  createdAt: string
  updatedAt: string
}

export interface PipelineScheduleInput {
  name: string
  cronExpr: string
  timezone: string
  enabled: boolean
  blockingPolicy: PipelineBlockingPolicy
  retryMax: number
  retryIntervalSeconds: number
  timeoutSeconds: number
  versionId: number
}

export function usePipelineSchedules(pipelineId?: number, query = '') {
  const params = new URLSearchParams()
  if (pipelineId) params.set('pipelineId', String(pipelineId))
  if (query.trim()) params.set('query', query.trim())
  const suffix = params.toString()
  return useQuery({
    queryKey: ['pipeline-schedules', pipelineId ?? 0, query],
    queryFn: () => apiGet<PipelineSchedule[]>(`/api/v1/pipeline-schedules${suffix ? `?${suffix}` : ''}`),
  })
}

function useInvalidateSchedules(pipelineId?: number) {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({ queryKey: ['pipeline-schedules'] })
    if (pipelineId) {
      queryClient.invalidateQueries({ queryKey: ['pipeline-schedules', pipelineId] })
    }
  }
}

export function useCreatePipelineSchedule(pipelineId: number) {
  const invalidate = useInvalidateSchedules(pipelineId)
  return useMutation({
    mutationFn: (input: PipelineScheduleInput) =>
      apiPost<PipelineSchedule>(`/api/v1/pipelines/${pipelineId}/schedules`, input),
    onSuccess: invalidate,
  })
}

export function useUpdatePipelineSchedule(pipelineId?: number) {
  const invalidate = useInvalidateSchedules(pipelineId)
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: PipelineScheduleInput }) =>
      apiPut<PipelineSchedule>(`/api/v1/pipeline-schedules/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeletePipelineSchedule(pipelineId?: number) {
  const invalidate = useInvalidateSchedules(pipelineId)
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/pipeline-schedules/${id}`),
    onSuccess: invalidate,
  })
}
