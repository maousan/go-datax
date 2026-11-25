import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export interface QueryTemplate {
  id: number
  name: string
  description: string
  dataSourceId: number
  dataSourceName: string
  sql: string
  table: string
  columns: string[]
  where: string
  splitPk: string
  createdAt: string
  updatedAt: string
}

export interface QueryTemplateInput {
  name: string
  description: string
  dataSourceId: number
  sql: string
  columns: string[]
}

export function useQueryTemplates(query = '') {
  return useQuery({
    queryKey: ['query-templates', query],
    queryFn: () =>
      apiGet<QueryTemplate[]>(
        `/api/v1/datax/datasets?query=${encodeURIComponent(query)}`,
      ),
  })
}

export function useQueryTemplate(id: number) {
  return useQuery({
    queryKey: ['query-templates', 'detail', id],
    queryFn: () => apiGet<QueryTemplate>(`/api/v1/datax/datasets/${id}`),
    enabled: Number.isFinite(id) && id > 0,
  })
}

function useInvalidateQueryTemplates() {
  const queryClient = useQueryClient()
  return () => {
    queryClient.invalidateQueries({ queryKey: ['query-templates'] })
    // 任务定义会展示数据集摘要，数据集变化后一并刷新。
    queryClient.invalidateQueries({ queryKey: ['sync-jobs'] })
  }
}

export function useCreateQueryTemplate() {
  const invalidate = useInvalidateQueryTemplates()
  return useMutation({
    mutationFn: (input: QueryTemplateInput) =>
      apiPost<QueryTemplate>('/api/v1/datax/datasets', input),
    onSuccess: invalidate,
  })
}

export function useUpdateQueryTemplate() {
  const invalidate = useInvalidateQueryTemplates()
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: QueryTemplateInput }) =>
      apiPut<QueryTemplate>(`/api/v1/datax/datasets/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteQueryTemplate() {
  const invalidate = useInvalidateQueryTemplates()
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/datax/datasets/${id}`),
    onSuccess: invalidate,
  })
}
