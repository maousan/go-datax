import { describe, expect, it } from 'vitest'
import type { DataSource } from '../../api/dataSources'
import {
  endpointObjectLabel,
  endpointOptions,
  endpointSchemaLabel,
  toEndpointInput,
  validateDelimiter,
  validateEndpointDraft,
  type EndpointDraft,
} from './helpers'

function dataSource(id: number, type: DataSource['type']): DataSource {
  return {
    id,
    name: `ds-${id}`,
    type,
    jdbcUrl: '',
    username: '',
    password: '',
    version: '',
    properties: {},
    createdAt: '',
    updatedAt: '',
  }
}

const fileSource = dataSource(1, 'filesystem')
const sqliteSource = dataSource(2, 'sqlite')

function draft(overrides: Partial<EndpointDraft> = {}): EndpointDraft {
  return {
    name: 'orders',
    description: '',
    connector: 'csv',
    dataSourceId: fileSource.id,
    table: '',
    query: '',
    path: 'orders.csv',
    splitPk: '',
    writeMode: 'insert',
    delimiter: '',
    header: true,
    nullFormat: '',
    schema: [{ name: 'id', type: 'int64' }],
    ...overrides,
  }
}

describe('endpoint options', () => {
  it('keeps sqlite source options source-only', () => {
    expect(
      endpointOptions(
        'source',
        draft({
          connector: 'sqlite',
          dataSourceId: sqliteSource.id,
          table: 'orders',
          path: '',
          splitPk: ' id ',
        }),
      ),
    ).toEqual({ splitPk: 'id' })
  })

  it('writes the sqlite sink mode', () => {
    expect(
      endpointOptions(
        'sink',
        draft({
          connector: 'sqlite',
          dataSourceId: sqliteSource.id,
          table: 'orders_copy',
          path: '',
          writeMode: 'truncate',
        }),
      ),
    ).toEqual({ writeMode: 'truncate' })
  })

  it('maps csv fields and omits source write mode', () => {
    expect(
      endpointOptions(
        'source',
        draft({ delimiter: '\\t', header: false, nullFormat: 'NULL' }),
      ),
    ).toEqual({ delimiter: '\\t', header: 'false', nullFormat: 'NULL' })
  })

  it('maps csv sink write mode', () => {
    expect(endpointOptions('sink', draft({ writeMode: 'truncate' }))).toEqual({
      header: 'true',
      writeMode: 'truncate',
    })
  })

  it('does not send delimited options for jsonl', () => {
    expect(
      endpointOptions('sink', draft({ connector: 'jsonl', writeMode: 'truncate' })),
    ).toEqual({ writeMode: 'truncate' })
  })
})

describe('endpoint validation', () => {
  it('accepts a valid file source', () => {
    expect(validateEndpointDraft('source', draft(), [fileSource, sqliteSource])).toBeNull()
  })

  it('rejects paths outside the data source root', () => {
    expect(
      validateEndpointDraft('source', draft({ path: '/etc/passwd' }), [fileSource]),
    ).toContain('相对路径')
    expect(
      validateEndpointDraft('source', draft({ path: '../escape.csv' }), [fileSource]),
    ).toContain('相对路径')
    expect(
      validateEndpointDraft('source', draft({ path: 'C:\\data\\orders.csv' }), [fileSource]),
    ).toContain('相对路径')
  })

  it('requires exactly one of table or query for sqlite sources', () => {
    const base = draft({
      connector: 'sqlite',
      dataSourceId: sqliteSource.id,
      path: '',
    })
    expect(validateEndpointDraft('source', base, [sqliteSource])).toContain('表名或查询')
    expect(
      validateEndpointDraft(
        'source',
        { ...base, table: 'orders', query: 'SELECT 1' },
        [sqliteSource],
      ),
    ).toContain('只能填写一个')
  })

  it('rejects a data source of the wrong type', () => {
    expect(
      validateEndpointDraft('source', draft({ dataSourceId: sqliteSource.id }), [
        fileSource,
        sqliteSource,
      ]),
    ).toContain('文件系统')
  })

  it('rejects empty and duplicate fields', () => {
    expect(
      validateEndpointDraft('source', draft({ schema: [] }), [fileSource]),
    ).toContain('至少添加一个字段')
    expect(
      validateEndpointDraft(
        'source',
        draft({
          schema: [
            { name: 'id', type: 'int64' },
            { name: 'ID', type: 'string' },
          ],
        }),
        [fileSource],
      ),
    ).toContain('字段名重复')
  })

  it('validates file delimiters', () => {
    expect(validateDelimiter('')).toBeNull()
    expect(validateDelimiter('|')).toBeNull()
    expect(validateDelimiter('\\t')).toBeNull()
    expect(validateDelimiter('\\u0001')).toBeNull()
    expect(validateDelimiter('||')).toContain('单个字符')
    expect(validateDelimiter('\\n')).toContain('单个字符')
    expect(validateDelimiter('\n')).toContain('换行')
    expect(
      validateEndpointDraft('source', draft({ delimiter: '||' }), [fileSource]),
    ).toContain('单个字符')
  })
})

describe('endpoint input and labels', () => {
  it('trims names and schema columns', () => {
    const input = toEndpointInput(
      'source',
      draft({
        name: ' orders ',
        schema: [{ name: ' id ', type: 'int64' }],
      }),
    )
    expect(input.name).toBe('orders')
    expect(input.schema).toEqual([{ name: 'id', type: 'int64' }])
  })

  it('labels sqlite queries and file paths', () => {
    expect(
      endpointObjectLabel({
        id: 1,
        kind: 'source',
        name: 's',
        description: '',
        connector: 'sqlite',
        dataSourceId: 1,
        dataSourceName: 'db',
        table: 'orders',
        query: '',
        path: '',
        schema: [],
        options: {},
        createdAt: '',
        updatedAt: '',
      }),
    ).toBe('orders')
    expect(endpointSchemaLabel({ schema: [{ name: 'id', type: 'int64' }] } as never)).toBe(
      'id:int64',
    )
  })
})
