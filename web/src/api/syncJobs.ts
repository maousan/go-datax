import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'
import type { Run } from './runs'

export type AuthoringMode = 'form' | 'raw'

export interface FormEndpoint {
  dataSourceId: number
  table: string
  column: string[]
  where?: string
  splitPk?: string
  writeMode?: string
}

export interface FormJobDefinition {
  reader: FormEndpoint
  writer: FormEndpoint
  setting: {
    channel: number
    errorLimit: { record: number; percentage: number }
  }
}

export interface SyncJob {
  id: number
  name: string
  authoringMode: AuthoringMode
  form?: FormJobDefinition
  raw?: unknown
  summary: string
  createdAt: string
  updatedAt: string
}

export interface SyncJobInput {
  name: string
  authoringMode: AuthoringMode
  form?: FormJobDefinition
  raw?: unknown
}

export function useSyncJobs(query: string) {
  return useQuery({
    queryKey: ['sync-jobs', query],
    queryFn: () => apiGet<SyncJob[]>(`/api/v1/sync-jobs?query=${encodeURIComponent(query)}`),
  })
}

function useInvalidateSyncJobs() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: ['sync-jobs'] })
}

export function useCreateSyncJob() {
  const invalidate = useInvalidateSyncJobs()
  return useMutation({
    mutationFn: (input: SyncJobInput) => apiPost<SyncJob>('/api/v1/sync-jobs', input),
    onSuccess: invalidate,
  })
}

export function useUpdateSyncJob() {
  const invalidate = useInvalidateSyncJobs()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: SyncJobInput }) =>
      apiPut<SyncJob>(`/api/v1/sync-jobs/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteSyncJob() {
  const invalidate = useInvalidateSyncJobs()
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/sync-jobs/${id}`),
    onSuccess: invalidate,
  })
}

export function useDuplicateSyncJob() {
  const invalidate = useInvalidateSyncJobs()
  return useMutation({
    mutationFn: (id: number) => apiPost<SyncJob>(`/api/v1/sync-jobs/${id}/duplicate`, {}),
    onSuccess: invalidate,
  })
}

export function useImportSyncJob() {
  const invalidate = useInvalidateSyncJobs()
  return useMutation({
    mutationFn: (input: { name: string; config: unknown }) =>
      apiPost<SyncJob>('/api/v1/sync-jobs/import', input),
    onSuccess: invalidate,
  })
}

export function useStartRun() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (jobId: number) => apiPost<Run>(`/api/v1/sync-jobs/${jobId}/runs`, {}),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['runs'] })
      queryClient.invalidateQueries({ queryKey: ['overview'] })
    },
  })
}

export async function exportSyncJob(id: number): Promise<unknown> {
  return apiGet<unknown>(`/api/v1/sync-jobs/${id}/export`)
}
