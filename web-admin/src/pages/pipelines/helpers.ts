import type { SchemaColumn } from '../../api/endpoints'
import type { Pipeline, PipelineEndpointSummary } from '../../api/pipelines'

export function schemasMatch(left: SchemaColumn[], right: SchemaColumn[]): boolean {
  if (left.length !== right.length) return false
  return left.every(
    (column, index) =>
      column.type === right[index].type &&
      column.name.trim().toLowerCase() === right[index].name.trim().toLowerCase(),
  )
}

export function schemaText(schema: SchemaColumn[]): string {
  if (schema.length === 0) return '-'
  return schema.map((column) => `${column.name}:${column.type}`).join(', ')
}

export function endpointObjectText(endpoint: PipelineEndpointSummary): string {
  if (endpoint.connector === 'sqlite') return endpoint.table || 'SQL 查询'
  return endpoint.path || '-'
}

export function pipelineChain(pipeline: Pipeline): string {
  const source = pipeline.source?.name ?? '-'
  const sink = pipeline.sink?.name ?? '-'
  return `${source} → ${sink}`
}
