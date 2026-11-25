import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiDelete, apiGet, apiPost, apiPut } from './client'

export type EndpointKind = 'source' | 'sink'
export type ConnectorName = 'mysql' | 'postgres' | 'sqlite' | 'csv' | 'txt' | 'jsonl'
export type DataType =
  | 'int64'
  | 'float64'
  | 'decimal'
  | 'string'
  | 'bytes'
  | 'bool'
  | 'timestamp'

export interface SchemaColumn {
  name: string
  type: DataType
}

export interface Endpoint {
  id: number
  kind: EndpointKind
  name: string
  description: string
  connector: ConnectorName
  dataSourceId: number
  dataSourceName: string
  table: string
  query: string
  path: string
  schema: SchemaColumn[]
  options: Record<string, string>
  createdAt: string
  updatedAt: string
}

export interface EndpointInput {
  name: string
  description: string
  connector: ConnectorName
  dataSourceId: number
  table: string
  query: string
  path: string
  schema: SchemaColumn[]
  options: Record<string, string>
}

export interface DiscoverInput {
  connector: ConnectorName
  dataSourceId: number
  table: string
  query: string
  path: string
  options: Record<string, string>
}

export interface ConnectorMeta {
  label: string
  dataSourceType: 'mysql' | 'postgres' | 'sqlite' | 'filesystem'
  file: boolean
  upsert: boolean
}

export const CONNECTOR_META: Record<ConnectorName, ConnectorMeta> = {
  mysql: { label: 'MySQL', dataSourceType: 'mysql', file: false, upsert: true },
  postgres: { label: 'PostgreSQL', dataSourceType: 'postgres', file: false, upsert: true },
  sqlite: { label: 'SQLite', dataSourceType: 'sqlite', file: false, upsert: false },
  csv: { label: 'CSV', dataSourceType: 'filesystem', file: true, upsert: false },
  txt: { label: 'TXT', dataSourceType: 'filesystem', file: true, upsert: false },
  jsonl: { label: 'JSONL', dataSourceType: 'filesystem', file: true, upsert: false },
}

export const CONNECTOR_OPTIONS = (Object.keys(CONNECTOR_META) as ConnectorName[]).map(
  (value) => ({ value, label: CONNECTOR_META[value].label }),
)

export function connectorDataSourceLabel(connector: ConnectorName): string {
  const meta = CONNECTOR_META[connector]
  return meta.dataSourceType === 'filesystem' ? '文件系统' : meta.label
}

export const DATA_TYPE_OPTIONS: { value: DataType; label: string }[] = [
  { value: 'int64', label: 'int64' },
  { value: 'float64', label: 'float64' },
  { value: 'decimal', label: 'decimal（字符串）' },
  { value: 'string', label: 'string' },
  { value: 'bytes', label: 'bytes' },
  { value: 'bool', label: 'bool' },
  { value: 'timestamp', label: 'timestamp' },
]

export function endpointCollection(kind: EndpointKind): 'sources' | 'sinks' {
  return kind === 'source' ? 'sources' : 'sinks'
}

export function useEndpoints(kind: EndpointKind, query: string) {
  return useQuery({
    queryKey: ['endpoints', kind, query],
    queryFn: () =>
      apiGet<Endpoint[]>(
        `/api/v1/${endpointCollection(kind)}?query=${encodeURIComponent(query)}`,
      ),
  })
}

function useInvalidateEndpoints(kind: EndpointKind) {
  const queryClient = useQueryClient()
  return () =>
    queryClient.invalidateQueries({ queryKey: ['endpoints', kind] })
}

export function useCreateEndpoint(kind: EndpointKind) {
  const invalidate = useInvalidateEndpoints(kind)
  return useMutation({
    mutationFn: (input: EndpointInput) =>
      apiPost<Endpoint>(`/api/v1/${endpointCollection(kind)}`, input),
    onSuccess: invalidate,
  })
}

export function useUpdateEndpoint(kind: EndpointKind) {
  const invalidate = useInvalidateEndpoints(kind)
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: EndpointInput }) =>
      apiPut<Endpoint>(`/api/v1/${endpointCollection(kind)}/${id}`, input),
    onSuccess: invalidate,
  })
}

export function useDeleteEndpoint(kind: EndpointKind) {
  const invalidate = useInvalidateEndpoints(kind)
  return useMutation({
    mutationFn: (id: number) => apiDelete(`/api/v1/${endpointCollection(kind)}/${id}`),
    onSuccess: invalidate,
  })
}

export async function discoverEndpoint(
  kind: EndpointKind,
  input: DiscoverInput,
): Promise<SchemaColumn[]> {
  const result = await apiPost<{ schema: SchemaColumn[] }>(
    `/api/v1/${endpointCollection(kind)}/discover`,
    input,
  )
  return result.schema ?? []
}
