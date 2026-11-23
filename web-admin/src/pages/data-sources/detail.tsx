import {
  Alert,
  Button,
  Card,
  Empty,
  Input,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconApps,
  IconCaretDown,
  IconCaretRight,
  IconCode,
  IconEye,
  IconFile,
  IconLeft,
  IconPlayArrow,
  IconRefresh,
  IconSearch,
  IconStorage,
} from '@arco-design/web-react/icon'
import { useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { useDataSource, type DataSourceType } from '../../api/dataSources'
import {
  useColumns,
  useRunQuery,
  useSchemas,
  useTableRows,
  useTables,
  type CellValue,
  type Column,
  type QueryResult,
  type RowsResult,
  type Table as ExplorerTable,
} from '../../api/explorer'
import SqlEditor from '../../components/SqlEditor'
import styles from './detail.module.less'

const TYPE_LABELS: Record<DataSourceType, string> = {
  mysql: 'MySQL',
  postgres: 'PostgreSQL',
  sqlite: 'SQLite',
  mssql: 'SQL Server',
  oracle: 'Oracle',
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

function quoteIdentifier(type: DataSourceType, identifier: string): string {
  if (type === 'mysql') return `\`${identifier.replaceAll('`', '``')}\``
  if (type === 'mssql') return `[${identifier.replaceAll(']', ']]')}]`
  return `"${identifier.replaceAll('"', '""')}"`
}

function defaultSQL(type: DataSourceType, schema: string, table: string): string {
  const tableName =
    type === 'sqlite' || !schema
      ? quoteIdentifier(type, table)
      : `${quoteIdentifier(type, schema)}.${quoteIdentifier(type, table)}`
  return `SELECT * FROM ${tableName}`
}

function cellNode(value: CellValue) {
  if (value === null) {
    return (
      <Typography.Text type="secondary" style={{ fontStyle: 'italic' }}>
        NULL
      </Typography.Text>
    )
  }
  return String(value)
}

function gridColumns(columns: string[]): TableColumnProps<Record<string, CellValue>>[] {
  return [
    {
      title: '#',
      dataIndex: '__index',
      width: 64,
      render: (_, row) => row.__index,
    },
    ...columns.map((name) => ({
      title: name,
      dataIndex: name,
      width: 180,
      ellipsis: true,
      render: (value: CellValue) => cellNode(value),
    })),
  ]
}

function gridRows(result: RowsResult | QueryResult, offset = 0) {
  return result.rows.map((row, index) => {
    const item: Record<string, CellValue> = { __index: offset + index + 1 }
    result.columns.forEach((column, columnIndex) => {
      item[column] = row[columnIndex] ?? null
    })
    return item
  })
}

function DataGrid({
  columns,
  rows,
  scrollWidth,
}: {
  columns: string[]
  rows: Record<string, CellValue>[]
  scrollWidth: number
}) {
  return (
    <Table<Record<string, CellValue>>
      rowKey="__index"
      columns={gridColumns(columns)}
      data={rows}
      pagination={false}
      scroll={{ x: scrollWidth }}
      noDataElement={<Empty description="当前结果集没有任何记录" />}
    />
  )
}

function TableDataPane({
  dataSourceId,
  schema,
  table,
}: {
  dataSourceId: number
  schema: string
  table: string
}) {
  const [whereInput, setWhereInput] = useState('')
  const [orderInput, setOrderInput] = useState('')
  const [where, setWhere] = useState('')
  const [orderBy, setOrderBy] = useState('')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(50)

  const rowsQuery = useTableRows(dataSourceId, {
    schema,
    table,
    page,
    pageSize,
    where,
    orderBy,
  })

  const result = rowsQuery.data
  const columns = result?.columns ?? []
  const rows = useMemo(() => (result ? gridRows(result, (page - 1) * pageSize) : []), [result, page, pageSize])

  return (
    <div className={styles.pane}>
      <div className={styles.toolbar}>
        <Input
          allowClear
          prefix={<IconSearch />}
          placeholder="WHERE 条件，例如 status = 'active'"
          value={whereInput}
          onChange={setWhereInput}
          onPressEnter={() => {
            setPage(1)
            setWhere(whereInput.trim())
          }}
          style={{ width: 320 }}
        />
        <Input
          allowClear
          placeholder="ORDER BY，例如 id DESC"
          value={orderInput}
          onChange={setOrderInput}
          onPressEnter={() => {
            setPage(1)
            setOrderBy(orderInput.trim())
          }}
          style={{ width: 220 }}
        />
        <Button
          type="primary"
          icon={<IconSearch />}
          onClick={() => {
            setPage(1)
            setWhere(whereInput.trim())
            setOrderBy(orderInput.trim())
          }}
        >
          查询
        </Button>
        <Button icon={<IconRefresh />} loading={rowsQuery.isFetching} onClick={() => rowsQuery.refetch()}>
          刷新
        </Button>
        <Typography.Text type="secondary">
          {result ? `共 ${result.total} 行` : '按条件筛选表数据'}
        </Typography.Text>
      </div>

      {rowsQuery.isError ? (
        <Alert
          type="error"
          content={errorText(rowsQuery.error, '读取表数据失败')}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      <Spin loading={rowsQuery.isLoading} style={{ display: 'block' }}>
        <Table<Record<string, CellValue>>
          rowKey="__index"
          columns={gridColumns(columns)}
          data={rows}
          scroll={{ x: Math.max(columns.length * 180 + 64, 600) }}
          pagination={{
            current: page,
            pageSize,
            total: result?.total ?? 0,
            showTotal: true,
            sizeCanChange: true,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage)
              setPageSize(nextPageSize)
            },
          }}
          noDataElement={<Empty description="当前结果集没有任何记录" />}
        />
      </Spin>
    </div>
  )
}

function StructurePane({
  dataSourceId,
  schema,
  table,
}: {
  dataSourceId: number
  schema: string
  table: string
}) {
  const columnsQuery = useColumns(dataSourceId, schema, table, true)
  const columns: TableColumnProps<Column>[] = [
    { title: '序号', dataIndex: 'position', width: 64 },
    { title: '字段名', dataIndex: 'name', width: 160 },
    { title: '类型', dataIndex: 'dataType', width: 140 },
    {
      title: '可空',
      dataIndex: 'nullable',
      width: 70,
      render: (value: boolean) => (value ? '是' : '否'),
    },
    {
      title: '默认值',
      dataIndex: 'default',
      width: 120,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: '注释',
      dataIndex: 'comment',
      ellipsis: true,
      render: (value: string) => value || '-',
    },
  ]

  return (
    <div className={styles.pane}>
      {columnsQuery.isError ? (
        <Alert
          type="error"
          content={errorText(columnsQuery.error, '读取字段信息失败')}
          style={{ marginBottom: 12 }}
        />
      ) : null}
      <Table<Column>
        rowKey="name"
        tableLayoutFixed
        columns={columns}
        data={columnsQuery.data ?? []}
        loading={columnsQuery.isLoading}
        pagination={false}
        noDataElement={<Empty description="没有字段信息" />}
      />
    </div>
  )
}

function QueryPane({
  dataSourceId,
  type,
  schema,
  table,
}: {
  dataSourceId: number
  type: DataSourceType
  schema: string
  table: string
}) {
  const [sql, setSQL] = useState(() => defaultSQL(type, schema, table))
  const runQuery = useRunQuery(dataSourceId)
  const result = runQuery.data

  return (
    <div className={styles.pane}>
      <div className={styles.toolbar}>
        <Button
          type="primary"
          icon={<IconPlayArrow />}
          loading={runQuery.isPending}
          onClick={() => runQuery.mutate(sql)}
        >
          执行
        </Button>
        <Typography.Text type="secondary">只读查询，最多返回 200 行</Typography.Text>
        {result ? (
          <Typography.Text type="secondary">
            返回 {result.rows.length} 行 · {result.elapsedMs} ms
            {result.truncated ? ' · 已截断' : ''}
          </Typography.Text>
        ) : null}
      </div>

      <SqlEditor value={sql} onChange={setSQL} height="180px" />

      {runQuery.isError ? (
        <Alert
          type="error"
          content={errorText(runQuery.error, '查询执行失败')}
          style={{ margin: '12px 0' }}
        />
      ) : null}

      {result ? (
        <div className={styles.result}>
          <DataGrid
            columns={result.columns}
            rows={gridRows(result)}
            scrollWidth={Math.max(result.columns.length * 180 + 64, 600)}
          />
        </div>
      ) : (
        <div className={styles.queryPlaceholder}>
          <IconCode style={{ fontSize: 40 }} />
          <div>执行 SQL 后在这里查看结果</div>
        </div>
      )}
    </div>
  )
}

function TableTree({
  dataSourceId,
  schema,
  selectedTable,
  filter,
  onSelect,
}: {
  dataSourceId: number
  schema: string
  selectedTable: string
  filter: string
  onSelect: (table: ExplorerTable) => void
}) {
  const tablesQuery = useTables(dataSourceId, schema, true)
  const [openTables, setOpenTables] = useState(false)
  const [openViews, setOpenViews] = useState(false)
  const tables = (tablesQuery.data ?? []).filter((item) =>
    item.name.toLowerCase().includes(filter.toLowerCase()),
  )

  if (tablesQuery.isLoading) {
    return (
      <div className={styles.treeHint}>
        <Spin size={14} /> 加载表...
      </div>
    )
  }
  if (tablesQuery.isError) {
    return <div className={styles.treeHint}>{errorText(tablesQuery.error, '读取表列表失败')}</div>
  }

  const tableItems = tables.filter((item) => item.type !== 'view')
  const viewItems = tables.filter((item) => item.type === 'view')
  const tablesExpanded = filter ? true : openTables
  const viewsExpanded = filter ? true : openViews

  const renderItem = (table: ExplorerTable) => (
    <div
      key={table.name}
      className={
        selectedTable === table.name ? styles['tree-item-active'] : styles['tree-item']
      }
      onClick={() => onSelect(table)}
    >
      {table.type === 'view' ? <IconEye /> : <IconFile />}
      <span>{table.name}</span>
    </div>
  )

  return (
    <div className={styles.tables}>
      <div className={styles['tree-group']} onClick={() => setOpenTables(!openTables)}>
        {tablesExpanded ? <IconCaretDown /> : <IconCaretRight />}
        <IconApps className={styles['group-table-icon']} />
        <span>表</span>
        {tableItems.length > 0 ? (
          <span className={styles['group-count']}>{tableItems.length}</span>
        ) : null}
      </div>
      {tablesExpanded ? (
        <div className={styles['tree-group-items']}>
          {tableItems.length > 0 ? (
            tableItems.map(renderItem)
          ) : (
            <div className={styles.treeHint}>没有表</div>
          )}
        </div>
      ) : null}

      <div className={styles['tree-group']} onClick={() => setOpenViews(!openViews)}>
        {viewsExpanded ? <IconCaretDown /> : <IconCaretRight />}
        <IconEye className={styles['group-view-icon']} />
        <span>视图</span>
        {viewItems.length > 0 ? (
          <span className={styles['group-count']}>{viewItems.length}</span>
        ) : null}
      </div>
      {viewsExpanded ? (
        <div className={styles['tree-group-items']}>
          {viewItems.length > 0 ? (
            viewItems.map(renderItem)
          ) : (
            <div className={styles.treeHint}>没有视图</div>
          )}
        </div>
      ) : null}
    </div>
  )
}

export default function DataSourceDetailPage() {
  const navigate = useNavigate()
  const params = useParams()
  const dataSourceId = Number(params.id)
  const [selectedSchema, setSelectedSchema] = useState('')
  const [selectedTable, setSelectedTable] = useState('')
  const [expandedSchema, setExpandedSchema] = useState<string | null>(null)
  const [filter, setFilter] = useState('')

  const dsQuery = useDataSource(dataSourceId)
  const schemasQuery = useSchemas(dataSourceId)
  const schemas = useMemo(() => schemasQuery.data ?? [], [schemasQuery.data])
  const ds = dsQuery.data
  const preferredSchema = useMemo(() => {
    const username = ds?.username?.toLowerCase()
    const mysqlDatabase = ds?.jdbcUrl.match(/^jdbc:mysql:\/\/[^/]+\/([^?;]+)/i)?.[1]
    return (
      schemas.find((item) => username && item.name.toLowerCase() === username) ??
      schemas.find(
        (item) =>
          mysqlDatabase && item.name.toLowerCase() === decodeURIComponent(mysqlDatabase).toLowerCase(),
      ) ??
      schemas.find((item) => item.name.toLowerCase() === 'dbo') ??
      schemas.find((item) => item.name.toLowerCase() === 'public') ??
      schemas[0]
    )
  }, [schemas, ds?.username, ds?.jdbcUrl])
  const schema = selectedSchema || preferredSchema?.name || ''
  const openSchema = expandedSchema === null ? preferredSchema?.name ?? '' : expandedSchema

  const visibleSchemas = useMemo(
    () => schemas.filter((item) => item.name.toLowerCase().includes(filter.toLowerCase())),
    [schemas, filter],
  )

  if (!Number.isFinite(dataSourceId) || dataSourceId <= 0) {
    return <Alert type="error" content="无效的数据源编号" />
  }

  return (
    <Card className={styles.workspace} bodyStyle={{ padding: 0 }}>
      <div className={styles.header}>
        <Space>
          <Button icon={<IconLeft />} onClick={() => navigate('/data-sources')}>
            返回
          </Button>
          <IconStorage style={{ fontSize: 20, color: 'rgb(var(--primary-6))' }} />
          <Typography.Title heading={6} style={{ margin: 0 }}>
            {ds?.name ?? `数据源 #${dataSourceId}`}
          </Typography.Title>
          {ds ? <Tag color="arcoblue">{TYPE_LABELS[ds.type] ?? ds.type}</Tag> : null}
        </Space>
        <Typography.Text type="secondary">只读浏览</Typography.Text>
      </div>

      <div className={styles.body}>
        <aside className={styles.sidebar}>
          <div className={styles['sidebar-title']}>
            <span>数据库</span>
            <Typography.Text type="secondary">
              {schemas.length ? `${schemas.length} 个` : ''}
            </Typography.Text>
          </div>
          <Input
            allowClear
            prefix={<IconSearch />}
            placeholder="搜索库或表"
            value={filter}
            onChange={setFilter}
            style={{ marginBottom: 12 }}
          />

          {schemasQuery.isError ? (
            <Alert
              type="error"
              content={errorText(schemasQuery.error, '连接数据源失败')}
              style={{ marginBottom: 12 }}
            />
          ) : null}

          <div className={styles.tree}>
            {schemasQuery.isLoading ? (
              <div className={styles.treeHint}>
                <Spin size={14} /> 正在连接数据源...
              </div>
            ) : null}
            {!schemasQuery.isLoading && visibleSchemas.length === 0 ? (
              <Empty description="没有可访问的数据库" />
            ) : null}
            {visibleSchemas.map((item) => {
              const open = item.name === openSchema
              return (
                <div key={item.name}>
                  <div
                    className={open ? styles['schema-active'] : styles.schema}
                    onClick={() => {
                      setExpandedSchema(open ? '' : item.name)
                    }}
                  >
                    {open ? <IconCaretDown /> : <IconCaretRight />}
                    <IconStorage className={styles['schema-icon']} />
                    <span className={styles['schema-name']}>{item.name}</span>
                    <span className={styles['schema-count']}>{item.tableCount}</span>
                  </div>
                  {open ? (
                    <TableTree
                      dataSourceId={dataSourceId}
                      schema={item.name}
                      selectedTable={selectedSchema === item.name ? selectedTable : ''}
                      filter={filter}
                      onSelect={(table) => {
                        setSelectedSchema(item.name)
                        setSelectedTable(table.name)
                      }}
                    />
                  ) : null}
                </div>
              )
            })}
          </div>
        </aside>

        <section className={styles.main}>
          {dsQuery.isError ? (
            <Alert
              type="error"
              content={errorText(dsQuery.error, '读取数据源失败')}
              style={{ margin: 16 }}
            />
          ) : null}

          {!selectedTable ? (
            <div className={styles.empty}>
              <IconStorage style={{ fontSize: 52 }} />
              <div className={styles['empty-title']}>从左侧选择表或视图</div>
              <Typography.Text type="secondary">
                可以浏览表数据、查看字段结构，或执行只读 SQL 查询
              </Typography.Text>
            </div>
          ) : (
            <div className={styles['table-area']}>
              <div className={styles['table-title']}>
                <Space>
                  <IconFile />
                  <Typography.Text>
                    {schema ? `${schema}.` : ''}
                    {selectedTable}
                  </Typography.Text>
                </Space>
                <Typography.Text type="secondary">只读</Typography.Text>
              </div>
              <Tabs key={`${schema}.${selectedTable}`} defaultActiveTab="data">
                <Tabs.TabPane key="data" title="数据">
                  <TableDataPane
                    key={`data:${schema}.${selectedTable}`}
                    dataSourceId={dataSourceId}
                    schema={schema}
                    table={selectedTable}
                  />
                </Tabs.TabPane>
                <Tabs.TabPane key="structure" title="结构">
                  <StructurePane
                    dataSourceId={dataSourceId}
                    schema={schema}
                    table={selectedTable}
                  />
                </Tabs.TabPane>
                <Tabs.TabPane key="sql" title="SQL">
                  <QueryPane
                    dataSourceId={dataSourceId}
                    type={ds?.type ?? 'mysql'}
                    schema={schema}
                    table={selectedTable}
                  />
                </Tabs.TabPane>
              </Tabs>
            </div>
          )}
        </section>
      </div>
    </Card>
  )
}
