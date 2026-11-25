import { describe, expect, it } from 'vitest'
import type { SchemaColumn } from '../../api/endpoints'
import type { Pipeline, PipelineEndpointSummary } from '../../api/pipelines'
import {
  defaultTransformStep,
  endpointObjectText,
  pipelineChain,
  schemaText,
  schemasMatch,
  triggerLabel,
} from './helpers'

const schema: SchemaColumn[] = [
  { name: 'id', type: 'int64' },
  { name: 'name', type: 'string' },
]

function endpoint(overrides: Partial<PipelineEndpointSummary> = {}): PipelineEndpointSummary {
  return {
    id: 1,
    name: 'orders',
    connector: 'sqlite',
    dataSourceName: 'db',
    table: 'orders',
    query: '',
    path: '',
    schema,
    options: {},
    ...overrides,
  }
}

describe('pipeline helpers', () => {
  it('compares schemas by ordered names and types', () => {
    expect(schemasMatch(schema, [...schema])).toBe(true)
    expect(schemasMatch(schema, [{ name: 'ID', type: 'int64' }, { name: 'name', type: 'string' }])).toBe(true)
    expect(schemasMatch(schema, [{ name: 'id', type: 'string' }, { name: 'name', type: 'string' }])).toBe(false)
    expect(schemasMatch(schema, [schema[0]])).toBe(false)
  })

  it('renders schema and endpoint object summaries', () => {
    expect(schemaText(schema)).toBe('id:int64, name:string')
    expect(endpointObjectText(endpoint())).toBe('orders')
    expect(endpointObjectText(endpoint({ connector: 'csv', path: 'exports/orders.csv' }))).toBe(
      'exports/orders.csv',
    )
    expect(endpointObjectText(endpoint({ connector: 'mysql', table: 'orders' }))).toBe('orders')
  })

  it('renders the pipeline chain', () => {
    const pipeline = {
      source: endpoint({ name: 'src' }),
      sink: endpoint({ name: 'dst' }),
    } as Pipeline
    expect(pipelineChain(pipeline)).toBe('src → dst')
  })

  it('labels run triggers', () => {
    expect(triggerLabel('manual')).toBe('手动')
    expect(triggerLabel('rerun')).toBe('重跑')
    expect(triggerLabel('schedule')).toBe('定时调度')
    expect(triggerLabel('retry')).toBe('失败重试')
  })

  it('creates default transform steps from the input schema', () => {
    expect(defaultTransformStep('cast', schema)).toEqual({
      type: 'cast',
      cast: [{ column: 'id', type: 'string' }],
    })
    expect(defaultTransformStep('rename', [])).toEqual({
      type: 'rename',
      rename: [{ from: '', to: '' }],
    })
    expect(defaultTransformStep('filter', schema)).toEqual({ type: 'filter', condition: '' })
  })
})
