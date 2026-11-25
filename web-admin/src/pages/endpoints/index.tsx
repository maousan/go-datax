import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconPlus,
  IconSearch,
  IconThunderbolt,
} from '@arco-design/web-react/icon'
import { useEffect, useRef, useState } from 'react'
import { ApiError } from '../../api/client'
import { useDataSources, type DataSource } from '../../api/dataSources'
import {
  CONNECTOR_META,
  CONNECTOR_OPTIONS,
  DATA_TYPE_OPTIONS,
  connectorDataSourceLabel,
  discoverEndpoint,
  useCreateEndpoint,
  useDeleteEndpoint,
  useEndpoints,
  useUpdateEndpoint,
  type ConnectorName,
  type DataType,
  type Endpoint,
  type EndpointKind,
  type SchemaColumn,
} from '../../api/endpoints'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'
import styles from './index.module.less'
import {
  endpointObjectLabel,
  endpointSchemaLabel,
  supportsUpsert,
  toEndpointInput,
  validateEndpointDraft,
  type EndpointDraft,
  type WriteMode,
} from './helpers'

const KIND_LABEL: Record<EndpointKind, string> = {
  source: '源',
  sink: '目标',
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

function emptyDraft(): EndpointDraft {
  return {
    name: '',
    description: '',
    connector: 'sqlite',
    dataSourceId: undefined,
    table: '',
    query: '',
    path: '',
    splitPk: '',
    writeMode: 'insert',
    delimiter: '',
    header: false,
    nullFormat: '',
    schema: [],
  }
}

function draftFromEndpoint(endpoint: Endpoint): EndpointDraft {
  const options = endpoint.options ?? {}
  return {
    name: endpoint.name,
    description: endpoint.description,
    connector: endpoint.connector,
    dataSourceId: endpoint.dataSourceId,
    table: endpoint.table,
    query: endpoint.query,
    path: endpoint.path,
    splitPk: options.splitPk ?? '',
    writeMode:
      options.writeMode === 'upsert'
        ? 'upsert'
        : options.writeMode === 'truncate'
          ? 'truncate'
          : 'insert',
    delimiter: options.delimiter ?? '',
    header:
      options.header !== undefined
        ? options.header === 'true'
        : endpoint.connector === 'csv',
    nullFormat: options.nullFormat ?? '',
    schema: endpoint.schema ?? [],
  }
}

export default function EndpointsPage() {
  const [kind, setKind] = useState<EndpointKind>('source')
  const [modalOpen, setModalOpen] = useState(false)
  const [modalKind, setModalKind] = useState<EndpointKind>('source')
  const [editing, setEditing] = useState<Endpoint>()
  const dataSourcesQuery = useDataSources('')
  const dataSources = dataSourcesQuery.data ?? []

  const openCreate = (nextKind: EndpointKind) => {
    setModalKind(nextKind)
    setEditing(undefined)
    setModalOpen(true)
  }

  const openEdit = (endpoint: Endpoint) => {
    setModalKind(endpoint.kind)
    setEditing(endpoint)
    setModalOpen(true)
  }

  return (
    <Card className={listStyles.page}>
      <div className={styles.header}>
        <Typography.Title heading={6} style={{ margin: 0 }}>
          源与目标
        </Typography.Title>
        <Typography.Text type="secondary">
          可复用的读取端与写入端；Pipeline 发布时会快照它们的数据源、字段和选项。
        </Typography.Text>
      </div>

      <Tabs
        activeTab={kind}
        onChange={(key) => setKind(key as EndpointKind)}
        className={styles.tabs}
      >
        <Tabs.TabPane key="source" title="源">
          <EndpointList
            kind="source"
            onCreate={() => openCreate('source')}
            onEdit={openEdit}
          />
        </Tabs.TabPane>
        <Tabs.TabPane key="sink" title="目标">
          <EndpointList
            kind="sink"
            onCreate={() => openCreate('sink')}
            onEdit={openEdit}
          />
        </Tabs.TabPane>
      </Tabs>

      {modalOpen ? (
        <EndpointModal
          key={editing ? `edit-${editing.id}` : `new-${modalKind}`}
          kind={modalKind}
          endpoint={editing}
          dataSources={dataSources}
          onClose={() => setModalOpen(false)}
          onSaved={() => setModalOpen(false)}
        />
      ) : null}
    </Card>
  )
}

function EndpointList({
  kind,
  onCreate,
  onEdit,
}: {
  kind: EndpointKind
  onCreate: () => void
  onEdit: (endpoint: Endpoint) => void
}) {
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [pageError, setPageError] = useState('')
  const [tableRef, tableHeight] = useFillHeight(42)
  const searchTimer = useRef<number | undefined>(undefined)
  const endpointsQuery = useEndpoints(kind, query)
  const deleteMutation = useDeleteEndpoint(kind)
  const label = KIND_LABEL[kind]

  useEffect(
    () => () => {
      if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    },
    [],
  )

  const applySearch = () => {
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    setQuery(searchInput.trim())
  }

  const handleSearchChange = (value: string) => {
    setSearchInput(value)
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    if (!value.trim()) {
      setQuery('')
      return
    }
    searchTimer.current = window.setTimeout(() => setQuery(value.trim()), 300)
  }

  const handleDelete = async (endpoint: Endpoint) => {
    setPageError('')
    try {
      await deleteMutation.mutateAsync(endpoint.id)
      Message.success(`已删除${label}「${endpoint.name}」`)
    } catch (err) {
      setPageError(errorMessage(err, `删除${label}失败`))
    }
  }

  const columns: TableColumnProps<Endpoint>[] = [
    { title: '名称', dataIndex: 'name', width: 180, ellipsis: true },
    {
      title: '连接器',
      dataIndex: 'connector',
      width: 110,
      render: (value: ConnectorName) => (
        <Tag color="arcoblue">{CONNECTOR_META[value]?.label ?? value}</Tag>
      ),
    },
    {
      title: '数据源',
      dataIndex: 'dataSourceName',
      width: 150,
      ellipsis: true,
    },
    {
      title: '对象',
      width: 220,
      ellipsis: true,
      render: (_, endpoint) => {
        const objectLabel = endpointObjectLabel(endpoint)
        if (!CONNECTOR_META[endpoint.connector]?.file && endpoint.query) {
          return (
            <Tooltip
              content={
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{endpoint.query}</pre>
              }
            >
              <span>{objectLabel}</span>
            </Tooltip>
          )
        }
        return objectLabel
      },
    },
    {
      title: '字段',
      width: 260,
      ellipsis: true,
      render: (_, endpoint) => endpointSchemaLabel(endpoint),
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 175,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 100,
      fixed: 'right',
      render: (_, endpoint) => (
        <Space size={2}>
          <Tooltip content="编辑">
            <Button
              type="text"
              size="small"
              aria-label={`编辑${label} ${endpoint.name}`}
              icon={<IconEdit />}
              onClick={() => onEdit(endpoint)}
            />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content={`确认删除该${label}？Pipeline 已发布版本中的快照不会受影响。`}
              okText="确定"
              cancelText="取消"
              onOk={() => handleDelete(endpoint)}
            >
              <Button
                type="text"
                status="danger"
                size="small"
                aria-label={`删除${label} ${endpoint.name}`}
                icon={<IconDelete />}
              />
            </Popconfirm>
          </Tooltip>
        </Space>
      ),
    },
  ]

  return (
    <div className={styles.list}>
      <div className={listStyles.toolbar}>
        <Input
          allowClear
          aria-label={`${label}名称`}
          className={listStyles['search-input']}
          prefix={<IconSearch />}
          placeholder={`请输入${label}名称`}
          value={searchInput}
          onChange={handleSearchChange}
          onPressEnter={applySearch}
        />

        <div className={listStyles.actions}>
          <Button type="primary" icon={<IconPlus />} onClick={onCreate}>
            新建{label}
          </Button>
        </div>
      </div>

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          onClose={() => setPageError('')}
          style={{ marginBottom: 12 }}
        />
      ) : null}
      {endpointsQuery.isError ? (
        <Alert
          type="error"
          content={errorMessage(endpointsQuery.error, `读取${label}失败`)}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<Endpoint>
          rowKey="id"
          tableLayoutFixed
          scroll={{ x: 1195, y: tableHeight }}
          columns={columns}
          data={endpointsQuery.data ?? []}
          loading={endpointsQuery.isLoading}
          pagination={false}
          noDataElement={
            <Empty description={query ? `没有匹配的${label}` : `还没有${label}`} />
          }
        />
      </div>
    </div>
  )
}

function EndpointModal({
  kind,
  endpoint,
  dataSources,
  onClose,
  onSaved,
}: {
  kind: EndpointKind
  endpoint?: Endpoint
  dataSources: DataSource[]
  onClose: () => void
  onSaved: () => void
}) {
  const [draft, setDraft] = useState<EndpointDraft>(() =>
    endpoint ? draftFromEndpoint(endpoint) : emptyDraft(),
  )
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [discovering, setDiscovering] = useState(false)
  const createMutation = useCreateEndpoint(kind)
  const updateMutation = useUpdateEndpoint(kind)
  const label = KIND_LABEL[kind]
  const meta = CONNECTOR_META[draft.connector]
  const matchingDataSources = dataSources.filter(
    (dataSource) => dataSource.type === meta.dataSourceType,
  )

  const update = (patch: Partial<EndpointDraft>) => {
    setDraft((previous) => ({ ...previous, ...patch }))
  }

  const handleConnectorChange = (value: ConnectorName) => {
    const nextMeta = CONNECTOR_META[value]
    const selected = dataSources.find((item) => item.id === draft.dataSourceId)
    setDraft((previous) => ({
      ...previous,
      connector: value,
      dataSourceId:
        selected && selected.type === nextMeta.dataSourceType
          ? previous.dataSourceId
          : undefined,
      table: nextMeta.file ? '' : previous.table,
      query: nextMeta.file ? '' : previous.query,
      splitPk: nextMeta.file ? '' : previous.splitPk,
      path: nextMeta.file ? previous.path : '',
      delimiter: nextMeta.file ? '' : previous.delimiter,
      header: nextMeta.file ? value === 'csv' : false,
      nullFormat: nextMeta.file ? '' : previous.nullFormat,
      writeMode: supportsUpsert(value) ? previous.writeMode : 'insert',
    }))
  }

  const addColumn = () => {
    update({ schema: [...draft.schema, { name: '', type: 'string' }] })
  }

  const removeColumn = (index: number) => {
    update({ schema: draft.schema.filter((_, current) => current !== index) })
  }

  const updateColumn = (index: number, patch: Partial<SchemaColumn>) => {
    update({
      schema: draft.schema.map((column, current) =>
        current === index ? { ...column, ...patch } : column,
      ),
    })
  }

  const handleDiscover = async () => {
    const dataSourceId = draft.dataSourceId
    if (!dataSourceId) {
      setFormError('请先选择数据源')
      return
    }
    setDiscovering(true)
    setFormError('')
    try {
      const schema = await discoverEndpoint(kind, {
        connector: draft.connector,
        dataSourceId,
        table: draft.table.trim(),
        query: draft.query.trim(),
        path: draft.path.trim(),
        options: toEndpointInput(kind, draft).options,
      })
      update({ schema })
      Message.success(`已发现 ${schema.length} 个字段`)
    } catch (err) {
      setFormError(errorMessage(err, '自动发现字段失败'))
    } finally {
      setDiscovering(false)
    }
  }

  const handleSave = async () => {
    const validation = validateEndpointDraft(kind, draft, dataSources)
    if (validation) {
      setFormError(validation)
      return
    }
    setSaving(true)
    setFormError('')
    try {
      const input = toEndpointInput(kind, draft)
      if (endpoint) {
        await updateMutation.mutateAsync({ id: endpoint.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      Message.success(endpoint ? `已保存${label}` : `已创建${label}`)
      onSaved()
    } catch (err) {
      setFormError(errorMessage(err, `保存${label}失败`))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      visible
      title={`${endpoint ? '编辑' : '新建'}${label}`}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={handleSave}>
            保存
          </Button>
        </Space>
      }
      style={{ width: 'min(720px, calc(100vw - 48px))', top: 40 }}
      maskClosable={false}
      unmountOnExit
      onCancel={onClose}
    >
      <div className={styles['modal-body']}>
        {formError ? (
          <Alert
            type="error"
            content={formError}
            closable
            onClose={() => setFormError('')}
            style={{ marginBottom: 12 }}
          />
        ) : null}

        <Form layout="vertical">
          <Form.Item label="名称" required>
            <Input
              value={draft.name}
              maxLength={100}
              placeholder={`${label}名称`}
              onChange={(value) => update({ name: value })}
            />
          </Form.Item>

          <Form.Item label="说明">
            <Input.TextArea
              value={draft.description}
              maxLength={500}
              rows={2}
              placeholder="可选，说明这个读取端或写入端的用途"
              onChange={(value) => update({ description: value })}
            />
          </Form.Item>

          <Form.Item label="连接器" required>
            <Select
              value={draft.connector}
              options={CONNECTOR_OPTIONS}
              onChange={(value) => handleConnectorChange(value as ConnectorName)}
            />
          </Form.Item>

          <Form.Item
            label="数据源"
            required
            extra={
              matchingDataSources.length === 0
                ? `暂无 ${connectorDataSourceLabel(draft.connector)} 数据源，请先在「数据源」页面创建。`
                : undefined
            }
          >
            <Select
              allowClear
              showSearch
              value={draft.dataSourceId}
              placeholder="选择数据源"
              options={matchingDataSources.map((dataSource) => ({
                value: dataSource.id,
                label: `${dataSource.name}（${dataSource.type}）`,
              }))}
              onChange={(value) =>
                update({
                  dataSourceId:
                    value === undefined ? undefined : Number(value),
                })
              }
            />
          </Form.Item>

          {meta.file ? (
            <>
              <Form.Item
                label="文件路径"
                required
                extra="相对于数据源根目录，例如 exports/orders.csv"
              >
                <Input
                  value={draft.path}
                  maxLength={1000}
                  placeholder="exports/orders.csv"
                  onChange={(value) => update({ path: value })}
                />
              </Form.Item>

              {draft.connector !== 'jsonl' ? (
                <>
                  <Form.Item
                    label="分隔符"
                    extra={
                      draft.connector === 'csv'
                        ? '默认逗号；制表符可填 \\t'
                        : '默认制表符；可填 \\t 或单个字符'
                    }
                  >
                    <Input
                      value={draft.delimiter}
                      maxLength={6}
                      placeholder={draft.connector === 'csv' ? ',' : '\\t'}
                      onChange={(value) => update({ delimiter: value })}
                    />
                  </Form.Item>

                  <Form.Item label="包含表头">
                    <Switch
                      checked={draft.header}
                      onChange={(checked) => update({ header: checked })}
                    />
                  </Form.Item>

                  <Form.Item
                    label="空值标记"
                    extra="等于该文本的字段按 null 处理；留空时 CSV/TXT 的空字段即 null"
                  >
                    <Input
                      value={draft.nullFormat}
                      placeholder="例如 NULL"
                      onChange={(value) => update({ nullFormat: value })}
                    />
                  </Form.Item>
                </>
              ) : null}
            </>
          ) : (
            <>
              {kind === 'source' ? (
                <>
                  <Form.Item label="表名" extra="与查询二选一">
                    <Input
                      value={draft.table}
                      maxLength={200}
                      placeholder="orders"
                      onChange={(value) => update({ table: value })}
                    />
                  </Form.Item>

                  <Form.Item label="查询" extra="与表名二选一；可写静态 WHERE">
                    <Input.TextArea
                      value={draft.query}
                      maxLength={20000}
                      rows={4}
                      placeholder="SELECT id, name FROM orders WHERE id > 0"
                      onChange={(value) => update({ query: value })}
                    />
                  </Form.Item>

                  <Form.Item
                    label="分片字段"
                    extra="可选，按数值主键拆分为并行读取；当前最多 4 个分片"
                  >
                    <Input
                      value={draft.splitPk}
                      placeholder="id"
                      onChange={(value) => update({ splitPk: value })}
                    />
                  </Form.Item>
                </>
              ) : (
                <Form.Item label="目标表" required>
                  <Input
                    value={draft.table}
                    maxLength={200}
                    placeholder="orders_copy"
                    onChange={(value) => update({ table: value })}
                  />
                </Form.Item>
              )}
            </>
          )}

          {kind === 'sink' ? (
            <Form.Item label="写入模式">
              <Select
                value={draft.writeMode}
                options={[
                  { value: 'insert', label: '追加写入（insert）' },
                  { value: 'truncate', label: '清空后写入（truncate）' },
                  ...(supportsUpsert(draft.connector)
                    ? [{ value: 'upsert', label: '主键冲突时更新（upsert）' }]
                    : []),
                ]}
                onChange={(value) => update({ writeMode: value as WriteMode })}
              />
            </Form.Item>
          ) : null}

          <Form.Item
            label="字段"
            required
            extra="按顺序声明输出字段；源和目标字段需一一对应。"
          >
            <div>
              <div className={styles['schema-toolbar']}>
                <Button size="small" icon={<IconPlus />} onClick={addColumn}>
                  添加字段
                </Button>
                <Button
                  size="small"
                  icon={<IconThunderbolt />}
                  loading={discovering}
                  onClick={handleDiscover}
                >
                  自动发现
                </Button>
              </div>

              {draft.schema.length === 0 ? (
                <div className={styles['schema-empty']}>
                  还没有字段，点击“自动发现”或“添加字段”。
                </div>
              ) : (
                draft.schema.map((column, index) => (
                  <div key={index} className={styles['schema-row']}>
                    <Input
                      value={column.name}
                      aria-label={`字段 ${index + 1} 名称`}
                      placeholder="字段名"
                      onChange={(value) => updateColumn(index, { name: value })}
                    />
                    <Select
                      value={column.type}
                      options={DATA_TYPE_OPTIONS}
                      onChange={(value) =>
                        updateColumn(index, { type: value as DataType })
                      }
                    />
                    <Tooltip content="删除字段">
                      <Button
                        type="text"
                        status="danger"
                        aria-label={`删除字段 ${column.name || index + 1}`}
                        icon={<IconDelete />}
                        onClick={() => removeColumn(index)}
                      />
                    </Tooltip>
                  </div>
                ))
              )}
            </div>
          </Form.Item>
        </Form>
      </div>
    </Modal>
  )
}
