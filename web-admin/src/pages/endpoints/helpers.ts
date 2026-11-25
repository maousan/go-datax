import {
  CONNECTOR_META,
  type ConnectorName,
  type Endpoint,
  type EndpointInput,
  type EndpointKind,
  type SchemaColumn,
} from '../../api/endpoints'
import type { DataSource } from '../../api/dataSources'

export type WriteMode = 'insert' | 'truncate'

export interface EndpointDraft {
  name: string
  description: string
  connector: ConnectorName
  dataSourceId?: number
  table: string
  query: string
  path: string
  splitPk: string
  writeMode: WriteMode
  delimiter: string
  header: boolean
  nullFormat: string
  schema: SchemaColumn[]
}

export function endpointOptions(kind: EndpointKind, draft: EndpointDraft): Record<string, string> {
  const options: Record<string, string> = {}
  const meta = CONNECTOR_META[draft.connector]

  if (!meta.file) {
    if (kind === 'source') {
      if (draft.splitPk.trim()) options.splitPk = draft.splitPk.trim()
    } else {
      options.writeMode = draft.writeMode
    }
    return options
  }

  if (draft.connector !== 'jsonl') {
    if (draft.delimiter) options.delimiter = draft.delimiter
    options.header = draft.header ? 'true' : 'false'
    if (draft.nullFormat) options.nullFormat = draft.nullFormat
  }
  if (kind === 'sink') {
    options.writeMode = draft.writeMode
  }
  return options
}

export function toEndpointInput(kind: EndpointKind, draft: EndpointDraft): EndpointInput {
  return {
    name: draft.name.trim(),
    description: draft.description.trim(),
    connector: draft.connector,
    dataSourceId: draft.dataSourceId ?? 0,
    table: draft.table.trim(),
    query: draft.query.trim(),
    path: draft.path.trim(),
    schema: draft.schema.map((column) => ({
      name: column.name.trim(),
      type: column.type,
    })),
    options: endpointOptions(kind, draft),
  }
}

export function validateDelimiter(value: string): string | null {
  if (!value) return null
  let decoded: string
  if (value === '\\t') {
    decoded = '\t'
  } else if (value === '\\\\') {
    decoded = '\\'
  } else if (/^\\u[0-9a-fA-F]{4}$/.test(value)) {
    decoded = String.fromCharCode(Number.parseInt(value.slice(2), 16))
  } else if (Array.from(value).length === 1) {
    decoded = value
  } else {
    return '分隔符必须是单个字符，或使用 \\t / \\u0001'
  }
  if (decoded === '\n' || decoded === '\r' || decoded === '\0') {
    return '分隔符不能是换行或空字符'
  }
  return null
}

export function validateEndpointDraft(
  kind: EndpointKind,
  draft: EndpointDraft,
  dataSources: DataSource[],
): string | null {
  if (!draft.name.trim()) return '请输入名称'
  if (draft.name.trim().length > 100) return '名称不能超过 100 个字符'
  if (draft.description.trim().length > 500) return '说明不能超过 500 个字符'

  const meta = CONNECTOR_META[draft.connector]
  if (!meta) return '请选择连接器'
  if (!draft.dataSourceId) return '请选择数据源'
  const dataSource = dataSources.find((item) => item.id === draft.dataSourceId)
  if (!dataSource) return '数据源不存在'
  if (dataSource.type !== meta.dataSourceType) {
    return `该连接器需要 ${meta.dataSourceType === 'sqlite' ? 'SQLite' : '文件系统'} 类型的数据源`
  }

  if (!meta.file) {
    if (kind === 'source') {
      if (!draft.table.trim() && !draft.query.trim()) return '请填写表名或查询'
      if (draft.table.trim() && draft.query.trim()) return '表名与查询只能填写一个'
    } else if (!draft.table.trim()) {
      return '请输入目标表'
    }
  } else {
    const path = draft.path.trim()
    if (!path) return '请输入文件路径'
    if (
      path.startsWith('/') ||
      path.startsWith('\\') ||
      /^[A-Za-z]:[\\/]/.test(path) ||
      path.split(/[\\/]/).includes('..')
    ) {
      return '文件路径必须是数据源根目录内的相对路径'
    }
    if (draft.connector !== 'jsonl') {
      const delimiterError = validateDelimiter(draft.delimiter)
      if (delimiterError) return delimiterError
    }
  }

  if (draft.schema.length === 0) return '请至少添加一个字段'
  const seen = new Set<string>()
  for (const [index, column] of draft.schema.entries()) {
    const name = column.name.trim()
    if (!name) return `第 ${index + 1} 个字段名不能为空`
    const key = name.toLowerCase()
    if (seen.has(key)) return `字段名重复：${name}`
    seen.add(key)
  }
  return null
}

export function endpointObjectLabel(endpoint: Endpoint): string {
  if (CONNECTOR_META[endpoint.connector]?.file) return endpoint.path
  return endpoint.table || 'SQL 查询'
}

export function endpointSchemaLabel(endpoint: Endpoint): string {
  return endpoint.schema.map((column) => `${column.name}:${column.type}`).join(', ')
}

export function connectorDataSourceType(connector: ConnectorName): DataSource['type'] {
  return CONNECTOR_META[connector].dataSourceType
}
