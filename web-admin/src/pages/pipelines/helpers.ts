import { CONNECTOR_META, type SchemaColumn } from '../../api/endpoints'
import type {
  Pipeline,
  PipelineEndpointSummary,
  TransformStep,
  TransformStepType,
} from '../../api/pipelines'

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
  if (CONNECTOR_META[endpoint.connector]?.file) return endpoint.path || '-'
  return endpoint.table || 'SQL 查询'
}

export function pipelineChain(pipeline: Pipeline): string {
  const source = pipeline.source?.name ?? '-'
  const sink = pipeline.sink?.name ?? '-'
  return `${source} → ${sink}`
}

const TRIGGER_LABELS: Record<string, string> = {
  manual: '手动',
  rerun: '重跑',
  diagnostic: '诊断',
}

export function triggerLabel(trigger: string): string {
  return TRIGGER_LABELS[trigger] ?? trigger
}

export function defaultTransformStep(
  type: TransformStepType,
  input: SchemaColumn[],
): TransformStep {
  switch (type) {
    case 'project':
      return { type, project: [{ name: '', type: 'string', expr: '' }] }
    case 'filter':
      return { type, condition: '' }
    case 'cast':
      return { type, cast: [{ column: input[0]?.name ?? '', type: 'string' }] }
    case 'rename':
      return { type, rename: [{ from: input[0]?.name ?? '', to: '' }] }
    case 'drop':
      return { type, drop: [] }
  }
}
