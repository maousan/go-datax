import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'
import type { Run } from './runs'

export type BlockingPolicy = 'discard' | 'overwrite'

export interface CollectTask {
  id: number
  name: string
  syncJobId: number
  syncJobName: string
  cronExpr: string
  enabled: boolean
  blockingPolicy: BlockingPolicy
  retryMax: number
  retryIntervalSeconds: number
  timeoutSeconds: number
  nextRunAt: string | null
  lastRun?: Run
  createdAt: string
  updatedAt: string
}

export interface CollectTaskInput {
  name: string
  syncJobId: number
  cronExpr: string
  enabled: boolean
  blockingPolicy: BlockingPolicy
  retryMax: number
  retryIntervalSeconds: number
  timeoutSeconds: number
}

export interface CollectTaskFilters {
  query?: string
  syncJobId?: number
}

function filterQuery(filters: CollectTaskFilters): string {
  const params = new URLSearchParams()
  if (filters.query) params.set('query', filters.query)
  if (filters.syncJobId) params.set('syncJobId', String(filters.syncJobId))
  return params.toString()
}

export function useCollectTasks(filters: CollectTaskFilters = {}) {
  return useQuery({
    queryKey: ['collect-tasks', filters],
    queryFn: () => apiGet<CollectTask[]>(`/api/v1/collect-tasks?${filterQuery(filters)}`),
    refetchInterval: 10_000,
  })
}

function useInvalidateCollectTasks() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: ['collect-tasks'] })
}

export function useCreateCollectTask() {
  const invalidate = useInvalidateCollectTasks()
  return useMutation({
    mutationFn: (input: CollectTaskInput) =>
      apiPost<CollectTask>('/api/v1/collect-tasks', input),
    onSuccess: invalidate,
  })
}

export function useUpdateCollectTask() {
  const invalidate = useInvalidateCollectTasks()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: CollectTaskInput }) =>
      apiPut<CollectTask>(`/api/v1/collect-tasks/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteCollectTask() {
  const invalidate = useInvalidateCollectTasks()
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/collect-tasks/${id}`),
    onSuccess: invalidate,
  })
}

export function useRunCollectTask() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => apiPost<Run>(`/api/v1/collect-tasks/${id}/run`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      queryClient.invalidateQueries({ queryKey: ['collect-tasks'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
  })
}
