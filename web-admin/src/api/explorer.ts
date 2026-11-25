import { useMutation, useQuery } from '@tanstack/react-query'
import { apiGet, apiPost } from './client'

export interface Schema {
  name: string
  tableCount: number
}

export interface Table {
  name: string
  type: 'table' | 'view'
}

export interface Column {
  name: string
  dataType: string
  nullable: boolean
  default: string
  comment: string
  position: number
}

export type CellValue = string | number | boolean | null

export interface RowsResult {
  columns: string[]
  rows: CellValue[][]
  total: number
  page: number
  pageSize: number
}

export interface QueryResult {
  columns: string[]
  rows: CellValue[][]
  elapsedMs: number
  truncated: boolean
}

export interface RowsParams {
  schema: string
  table: string
  page: number
  pageSize: number
  where: string
  orderBy: string
}

function queryString(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams()
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== '') {
      search.set(key, String(value))
    }
  })
  return search.toString()
}

export function useSchemas(dataSourceId: number, enabled = true) {
  return useQuery({
    queryKey: ['explorer', dataSourceId, 'schemas'],
    queryFn: () =>
      apiGet<{ schemas: Schema[] }>(`/api/v1/data-sources/${dataSourceId}/schemas`).then(
        (data) => data.schemas ?? [],
      ),
    enabled: enabled && Number.isFinite(dataSourceId) && dataSourceId > 0,
  })
}

export function useTables(dataSourceId: number, schema: string, enabled: boolean) {
  return useQuery({
    queryKey: ['explorer', dataSourceId, 'tables', schema],
    queryFn: () =>
      apiGet<{ tables: Table[] }>(
        `/api/v1/data-sources/${dataSourceId}/tables?${queryString({ schema })}`,
      ).then((data) => data.tables ?? []),
    enabled,
  })
}

export function useColumns(
  dataSourceId: number,
  schema: string,
  table: string,
  enabled: boolean,
) {
  return useQuery({
    queryKey: ['explorer', dataSourceId, 'columns', schema, table],
    queryFn: () =>
      apiGet<{ columns: Column[] }>(
        `/api/v1/data-sources/${dataSourceId}/columns?${queryString({ schema, table })}`,
      ).then((data) => data.columns ?? []),
    enabled: enabled && Boolean(table),
  })
}

export function useTableRows(dataSourceId: number, params: RowsParams | null) {
  return useQuery({
    queryKey: ['explorer', dataSourceId, 'rows', params],
    queryFn: () =>
      apiGet<RowsResult>(
        `/api/v1/data-sources/${dataSourceId}/rows?${queryString({
          schema: params?.schema,
          table: params?.table,
          page: params?.page,
          pageSize: params?.pageSize,
          where: params?.where,
          orderBy: params?.orderBy,
        })}`,
      ),
    enabled: Boolean(params?.table),
  })
}

export function useRunQuery(dataSourceId: number) {
  return useMutation({
    mutationFn: (sql: string) =>
      apiPost<QueryResult>(`/api/v1/data-sources/${dataSourceId}/query`, {
        sql,
        limit: 200,
      }),
  })
}
