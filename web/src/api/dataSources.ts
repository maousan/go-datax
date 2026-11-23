import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type DataSourceType = 'mysql' | 'postgres' | 'sqlite' | 'mssql' | 'oracle'

export interface DataSource {
  id: number
  name: string
  type: DataSourceType
  jdbcUrl: string
  username: string
  password: string
  properties: Record<string, string>
  createdAt: string
  updatedAt: string
}

export interface DataSourceInput {
  name: string
  type: DataSourceType
  jdbcUrl: string
  username: string
  password: string
  properties: Record<string, string>
}

export interface ConnectionTestResult {
  ok: boolean
  latencyMs: number
  message: string
}

export type ConnectionTestInput = DataSourceInput | { id: number }

export function testDataSource(body: ConnectionTestInput): Promise<ConnectionTestResult> {
  return apiPost<ConnectionTestResult>('/api/v1/data-sources/test', body)
}

export function useDataSources(query: string) {
  return useQuery({
    queryKey: ['data-sources', query],
    queryFn: () =>
      apiGet<DataSource[]>(`/api/v1/data-sources?query=${encodeURIComponent(query)}`),
  })
}

function useInvalidateDataSources() {
  const queryClient = useQueryClient()
  return () => queryClient.invalidateQueries({ queryKey: ['data-sources'] })
}

export function useCreateDataSource() {
  const invalidate = useInvalidateDataSources()
  return useMutation({
    mutationFn: (input: DataSourceInput) => apiPost<DataSource>('/api/v1/data-sources', input),
    onSuccess: invalidate,
  })
}

export function useUpdateDataSource() {
  const invalidate = useInvalidateDataSources()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: DataSourceInput }) =>
      apiPut<DataSource>(`/api/v1/data-sources/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteDataSource() {
  const invalidate = useInvalidateDataSources()
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/data-sources/${id}`),
    onSuccess: invalidate,
  })
}
