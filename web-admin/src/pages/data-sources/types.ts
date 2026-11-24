import microsoftSqlServerIcon from '../../assets/db-icons/microsoftsqlserver.svg'
import mysqlIcon from '../../assets/db-icons/mysql.svg'
import oracleIcon from '../../assets/db-icons/oracle.svg'
import postgresIcon from '../../assets/db-icons/postgresql.svg'
import sqliteIcon from '../../assets/db-icons/sqlite.svg'
import type { DataSourceType } from '../../api/dataSources'

export type DataSourceCategory = 'relational' | 'lightweight'

export interface DataSourceTypeMeta {
  value: DataSourceType
  label: string
  category: DataSourceCategory
  defaultPort: string
  brandColor: string
  icon: string
  databaseLabel: string
  databasePlaceholder: string
  urlPlaceholder: string
}

export const DATA_SOURCE_TYPES: DataSourceTypeMeta[] = [
  {
    value: 'mysql',
    label: 'MySQL',
    category: 'relational',
    defaultPort: '3306',
    brandColor: '#4479A1',
    icon: mysqlIcon,
    databaseLabel: '数据库',
    databasePlaceholder: '可选',
    urlPlaceholder: 'jdbc:mysql://127.0.0.1:3306/database',
  },
  {
    value: 'postgres',
    label: 'PostgreSQL',
    category: 'relational',
    defaultPort: '5432',
    brandColor: '#4169E1',
    icon: postgresIcon,
    databaseLabel: '数据库',
    databasePlaceholder: '可选',
    urlPlaceholder: 'jdbc:postgresql://127.0.0.1:5432/database',
  },
  {
    value: 'mssql',
    label: 'SQL Server',
    category: 'relational',
    defaultPort: '1433',
    brandColor: '#CC2927',
    icon: microsoftSqlServerIcon,
    databaseLabel: '数据库',
    databasePlaceholder: '可选',
    urlPlaceholder: 'jdbc:sqlserver://127.0.0.1:1433;DatabaseName=database',
  },
  {
    value: 'oracle',
    label: 'Oracle',
    category: 'relational',
    defaultPort: '1521',
    brandColor: '#F80000',
    icon: oracleIcon,
    databaseLabel: '服务名',
    databasePlaceholder: '例如 ORCLPDB1',
    urlPlaceholder: 'jdbc:oracle:thin:@//127.0.0.1:1521/ORCLPDB1',
  },
  {
    value: 'sqlite',
    label: 'SQLite',
    category: 'lightweight',
    defaultPort: '',
    brandColor: '#003B57',
    icon: sqliteIcon,
    databaseLabel: '数据库文件',
    databasePlaceholder: '/data/app.db',
    urlPlaceholder: 'jdbc:sqlite:/data/app.db',
  },
]

export const TYPE_CATEGORIES: Array<{ value: 'all' | DataSourceCategory; label: string }> = [
  { value: 'all', label: '全部' },
  { value: 'relational', label: '关系型数据库' },
  { value: 'lightweight', label: '轻量数据库' },
]

export const UI_COLOR_KEY = 'ui.color'

export function typeMeta(type: DataSourceType | string): DataSourceTypeMeta {
  return DATA_SOURCE_TYPES.find((item) => item.value === type) ?? DATA_SOURCE_TYPES[0]
}

export function dataSourceColor(
  type: DataSourceType | string,
  properties?: Record<string, string> | null,
): string {
  const stored = properties?.[UI_COLOR_KEY]
  return stored?.trim() || typeMeta(type).brandColor
}

/** 把 #RRGGBB 颜色转成带透明度的 rgba，非法颜色回退到中性底色。 */
export function withAlpha(color: string, alpha: number): string {
  const value = color.trim()
  if (!/^#[0-9a-f]{6}$/i.test(value)) return 'rgba(134, 144, 156, 0.12)'
  const clamped = Math.min(1, Math.max(0, alpha))
  const red = Number.parseInt(value.slice(1, 3), 16)
  const green = Number.parseInt(value.slice(3, 5), 16)
  const blue = Number.parseInt(value.slice(5, 7), 16)
  return `rgba(${red}, ${green}, ${blue}, ${clamped})`
}
