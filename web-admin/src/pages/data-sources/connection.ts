import { ApiError } from '../../api/client'
import type { ConnectionTestResult, DataSourceType } from '../../api/dataSources'
import { typeMeta } from './types'

export type SslMode = 'default' | 'disable' | 'require' | 'verify'
export type Charset = '' | 'utf8mb4' | 'gbk' | 'latin1'

export interface ConnectionParam {
  key: string
  value: string
}

export interface ConnectionFields {
  host: string
  port: string
  database: string
  path: string
  params: ConnectionParam[]
  charset: Charset
  sslMode: SslMode
}

export function defaultConnection(type: DataSourceType): ConnectionFields {
  return {
    host: '',
    port: typeMeta(type).defaultPort,
    database: '',
    path: '',
    params: [],
    charset: '',
    sslMode: 'default',
  }
}

function decode(value: string): string {
  try {
    return decodeURIComponent(value)
  } catch {
    return value
  }
}

function parseQuery(raw: string): Record<string, string> {
  const result: Record<string, string> = {}
  if (!raw) return result
  new URLSearchParams(raw).forEach((value, key) => {
    if (key) result[key] = value
  })
  return result
}

function parseHostPort(raw: string): { host: string; port: string } | null {
  const match = /^(\[[0-9a-fA-F:.]+\]|[^/:]+)(?::(\d+))?$/.exec(raw.trim())
  if (!match) return null
  return { host: match[1], port: match[2] ?? '' }
}

function findParamKey(params: Record<string, string>, name: string): string | undefined {
  const lower = name.toLowerCase()
  return Object.keys(params).find((key) => key.toLowerCase() === lower)
}

function peekParam(params: Record<string, string>, name: string): string | undefined {
  const key = findParamKey(params, name)
  return key === undefined ? undefined : params[key]
}

function takeParam(params: Record<string, string>, name: string): string | undefined {
  const key = findParamKey(params, name)
  if (key === undefined) return undefined
  const value = params[key]
  delete params[key]
  return value
}

function readMySQLCharset(params: Record<string, string>): Charset {
  const encoding = peekParam(params, 'characterEncoding')?.trim()
  const collation = peekParam(params, 'connectionCollation')?.trim().toLowerCase() ?? ''
  const upper = encoding?.toUpperCase() ?? ''

  if (collation.startsWith('utf8mb4')) {
    takeParam(params, 'characterEncoding')
    takeParam(params, 'connectionCollation')
    return 'utf8mb4'
  }
  if (!collation && (upper === 'UTF-8' || upper === 'UTF8')) {
    takeParam(params, 'characterEncoding')
    return 'utf8mb4'
  }
  if (upper === 'GBK') {
    takeParam(params, 'characterEncoding')
    return 'gbk'
  }
  if (upper === 'ISO-8859-1' || upper === 'ISO8859-1' || upper === 'CP1252' || upper === 'LATIN1') {
    takeParam(params, 'characterEncoding')
    return 'latin1'
  }
  return ''
}

function readMySQLSSL(params: Record<string, string>): SslMode {
  const value = peekParam(params, 'sslMode')?.trim().toUpperCase()
  let mode: SslMode = 'default'
  if (value === 'DISABLED') mode = 'disable'
  else if (value === 'REQUIRED') mode = 'require'
  else if (value === 'VERIFY_CA' || value === 'VERIFY_IDENTITY') mode = 'verify'
  if (mode !== 'default') takeParam(params, 'sslMode')
  return mode
}

function readPostgresSSL(params: Record<string, string>): SslMode {
  const value = peekParam(params, 'sslmode')?.trim().toLowerCase()
  let mode: SslMode = 'default'
  if (value === 'disable') mode = 'disable'
  else if (value === 'require') mode = 'require'
  else if (value === 'verify-ca' || value === 'verify-full') mode = 'verify'
  if (mode !== 'default') takeParam(params, 'sslmode')
  return mode
}

function readSQLServerSSL(params: Record<string, string>): SslMode {
  const encrypt = peekParam(params, 'encrypt')?.trim().toLowerCase()
  const trust = peekParam(params, 'trustServerCertificate')?.trim().toLowerCase()
  const truthy = (value?: string) => value === 'true' || value === 'yes' || value === '1'
  const falsy = (value?: string) => value === 'false' || value === 'no' || value === '0'

  if (falsy(encrypt)) {
    takeParam(params, 'encrypt')
    takeParam(params, 'trustServerCertificate')
    return 'disable'
  }
  if (truthy(encrypt)) {
    const verified = !truthy(trust)
    takeParam(params, 'encrypt')
    takeParam(params, 'trustServerCertificate')
    return verified ? 'verify' : 'require'
  }
  return 'default'
}

function paramsFromObject(params: Record<string, string>): ConnectionParam[] {
  return Object.entries(params).map(([key, value]) => ({ key, value }))
}

function parseStandardUrl(
  url: string,
  prefix: RegExp,
  type: DataSourceType,
): ConnectionFields | null {
  const match = prefix.exec(url)
  if (!match) return null

  let rest = url.slice(match[0].length)
  let query = ''
  const queryIndex = rest.indexOf('?')
  if (queryIndex >= 0) {
    query = rest.slice(queryIndex + 1)
    rest = rest.slice(0, queryIndex)
  }

  const slashIndex = rest.indexOf('/')
  const hostPortRaw = slashIndex >= 0 ? rest.slice(0, slashIndex) : rest
  const database = slashIndex >= 0 ? decode(rest.slice(slashIndex + 1)) : ''
  const hostPort = parseHostPort(hostPortRaw)
  if (!hostPort) return null
  if (type === 'oracle' && !database.trim()) return null

  const params = parseQuery(query)
  const fields: ConnectionFields = {
    ...defaultConnection(type),
    host: hostPort.host,
    port: hostPort.port || typeMeta(type).defaultPort,
    database,
  }

  if (type === 'mysql') {
    fields.charset = readMySQLCharset(params)
    fields.sslMode = readMySQLSSL(params)
  } else if (type === 'postgres') {
    fields.sslMode = readPostgresSSL(params)
  }

  fields.params = paramsFromObject(params)
  return fields
}

export function parseJdbcUrl(type: DataSourceType, raw: string): ConnectionFields | null {
  const url = raw.trim()
  if (!url) return null

  switch (type) {
    case 'mysql':
      return parseStandardUrl(url, /^jdbc:mysql:\/\//i, type)
    case 'postgres':
      return parseStandardUrl(url, /^jdbc:postgresql:\/\//i, type)
    case 'oracle':
      return (
        parseStandardUrl(url, /^jdbc:oracle:thin:@\/\//i, type) ??
        parseStandardUrl(url, /^oracle:\/\//i, type)
      )
    case 'sqlite': {
      const match = /^jdbc:sqlite:/i.exec(url)
      if (!match) return null
      const path = url.slice(match[0].length)
      if (!path.trim()) return null
      return { ...defaultConnection(type), path }
    }
    case 'mssql': {
      const match = /^jdbc:sqlserver:\/\//i.exec(url)
      if (!match) return null
      const rest = url.slice(match[0].length)
      const parts = rest.split(';')
      const hostPort = parseHostPort(parts.shift() ?? '')
      if (!hostPort) return null

      const params: Record<string, string> = {}
      for (const part of parts) {
        const separator = part.indexOf('=')
        if (separator <= 0) continue
        const key = part.slice(0, separator).trim()
        if (key) params[key] = part.slice(separator + 1).trim()
      }

      const database = takeParam(params, 'DatabaseName')?.trim() ?? ''
      const sslMode = readSQLServerSSL(params)
      return {
        ...defaultConnection(type),
        host: hostPort.host,
        port: hostPort.port || typeMeta(type).defaultPort,
        database,
        sslMode,
        params: paramsFromObject(params),
      }
    }
    default:
      return null
  }
}

function cleanParams(params: ConnectionParam[]): Record<string, string> {
  const result: Record<string, string> = {}
  params.forEach(({ key, value }) => {
    const name = key.trim()
    if (name) result[name] = value.trim()
  })
  return result
}

function reservedParams(type: DataSourceType, fields: ConnectionFields): Record<string, string> {
  const params: Record<string, string> = {}

  if (type === 'mysql') {
    if (fields.charset === 'utf8mb4') {
      params.characterEncoding = 'UTF-8'
      params.connectionCollation = 'utf8mb4_general_ci'
    } else if (fields.charset === 'gbk') {
      params.characterEncoding = 'GBK'
    } else if (fields.charset === 'latin1') {
      params.characterEncoding = 'ISO-8859-1'
    }

    if (fields.sslMode === 'disable') params.sslMode = 'DISABLED'
    else if (fields.sslMode === 'require') params.sslMode = 'REQUIRED'
    else if (fields.sslMode === 'verify') params.sslMode = 'VERIFY_IDENTITY'
  } else if (type === 'postgres') {
    if (fields.sslMode === 'disable') params.sslmode = 'disable'
    else if (fields.sslMode === 'require') params.sslmode = 'require'
    else if (fields.sslMode === 'verify') params.sslmode = 'verify-full'
  } else if (type === 'mssql') {
    if (fields.sslMode === 'disable') {
      params.encrypt = 'false'
    } else if (fields.sslMode === 'require') {
      params.encrypt = 'true'
      params.trustServerCertificate = 'true'
    } else if (fields.sslMode === 'verify') {
      params.encrypt = 'true'
      params.trustServerCertificate = 'false'
    }
  }

  return params
}

function queryString(params: Record<string, string>): string {
  return Object.entries(params)
    .map(([key, value]) => `${encodeURIComponent(key)}=${encodeURIComponent(value)}`)
    .join('&')
}

export function buildJdbcUrl(type: DataSourceType, fields: ConnectionFields): string {
  const host = fields.host.trim()
  const database = fields.database.trim()

  if (type === 'sqlite') {
    const path = fields.path.trim()
    return path ? `jdbc:sqlite:${path}` : ''
  }
  if (!host) return ''

  const meta = typeMeta(type)
  const port = fields.port.trim() || meta.defaultPort
  if (!port) return ''
  if (type === 'mssql' && /[;=]/.test(database)) return ''
  if (type === 'oracle' && !database) return ''

  const allParams = { ...reservedParams(type, fields), ...cleanParams(fields.params) }

  switch (type) {
    case 'mysql':
    case 'postgres': {
      const scheme = type === 'mysql' ? 'jdbc:mysql://' : 'jdbc:postgresql://'
      const query = queryString(allParams)
      return `${scheme}${host}:${port}${database ? `/${database}` : ''}${query ? `?${query}` : ''}`
    }
    case 'mssql': {
      const parts = [`jdbc:sqlserver://${host}:${port}`]
      if (database) parts.push(`DatabaseName=${database}`)
      Object.entries(allParams).forEach(([key, value]) => parts.push(`${key}=${value}`))
      return parts.join(';')
    }
    case 'oracle': {
      const query = queryString(allParams)
      return `jdbc:oracle:thin:@//${host}:${port}/${database}${query ? `?${query}` : ''}`
    }
    default:
      return ''
  }
}

export function validateConnectionParams(type: DataSourceType, params: ConnectionParam[]): string {
  if (type === 'sqlite') {
    const hasValue = params.some(({ key, value }) => key.trim() || value.trim())
    return hasValue ? 'SQLite 暂不支持自定义连接参数' : ''
  }

  const seen = new Set<string>()
  for (const param of params) {
    const key = param.key.trim()
    const value = param.value.trim()
    if (!key && !value) continue
    if (!key) return '连接参数名不能为空'
    if (!/^[A-Za-z0-9_.-]+$/.test(key)) {
      return `连接参数“${key}”只能包含字母、数字、下划线、点和短横线`
    }
    const lower = key.toLowerCase()
    if (seen.has(lower)) return `连接参数“${key}”重复`
    seen.add(lower)
    if (type === 'mssql' && /[;=]/.test(value)) {
      return `连接参数“${key}”的值不能包含分号或等号`
    }
  }
  return ''
}

export function describeTestResult(result: ConnectionTestResult): string {
  return result.ok ? result.message : `连接失败：${result.message}`
}

export function testFailure(err: unknown): ConnectionTestResult {
  return {
    ok: false,
    latencyMs: 0,
    message: err instanceof ApiError ? err.message : '测试失败，请稍后重试',
  }
}
