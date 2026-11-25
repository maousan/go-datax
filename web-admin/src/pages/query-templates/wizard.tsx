import {
  Alert,
  Button,
  Card,
  Checkbox,
  Descriptions,
  Divider,
  Grid,
  Input,
  Message,
  Select,
  Space,
  Spin,
  Steps,
  Table,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconCode,
  IconLeft,
  IconPlayArrow,
  IconRight,
  IconSave,
} from '@arco-design/web-react/icon'
import {
  DockviewDefaultTab,
  DockviewReact,
  themeLight,
  type DockviewApi,
  type IDockviewPanelHeaderProps,
} from 'dockview-react'
import 'dockview-react/dist/styles/dockview.css'
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { useDataSources } from '../../api/dataSources'
import { useRunQuery, type CellValue, type QueryResult } from '../../api/explorer'
import {
  useCreateQueryTemplate,
  useQueryTemplate,
  useUpdateQueryTemplate,
  type QueryTemplate,
  type QueryTemplateInput,
} from '../../api/queryTemplates'
import SqlEditor from '../../components/SqlEditor'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import styles from './wizard.module.less'

const { Row, Col } = Grid

const STEP_ITEMS = [
  { title: '基本信息', description: '名称与数据源' },
  { title: 'SQL 控制台', description: '编写并预览查询' },
  { title: '结果字段', description: '选择输出列' },
]

const DEMO_SQL = `SELECT id, order_no, customer_name, amount, status, created_at
FROM orders
WHERE created_at >= '2026-11-01'`

const DEMO_RESULT: QueryResult = {
  columns: ['id', 'order_no', 'customer_name', 'amount', 'status', 'created_at'],
  rows: [
    [1001, 'SO-20261124-001', '华东仓储', 1280.5, 'PAID', '2026-11-24 09:12:00'],
    [1002, 'SO-20261124-002', '华南零售', 860, 'PAID', '2026-11-24 09:18:00'],
    [1003, 'SO-20261124-003', '华北分销', 2450.75, 'PENDING', '2026-11-24 09:25:00'],
    [1004, 'SO-20261124-004', '西南直营', 530, 'REFUNDED', '2026-11-24 09:31:00'],
    [1005, 'SO-20261124-005', '东北仓储', 1920, 'PAID', '2026-11-24 09:40:00'],
  ],
  elapsedMs: 12,
  truncated: false,
}

interface ConsoleContextValue {
  sql: string
  setSql: (value: string) => void
  result?: QueryResult
}

const ConsoleContext = createContext<ConsoleContextValue | null>(null)

function useConsoleContext(): ConsoleContextValue {
  const context = useContext(ConsoleContext)
  if (!context) throw new Error('ConsoleContext is not available')
  return context
}

function SqlEditorPanel() {
  const { sql, setSql } = useConsoleContext()
  return (
    <div className={styles['dock-panel']}>
      <SqlEditor value={sql} onChange={setSql} height="100%" theme="dark" />
    </div>
  )
}

function ResultPanel() {
  const { result } = useConsoleContext()
  const [tableRef, tableHeight] = useFillHeight(42)

  if (!result) return null

  return (
    <div ref={tableRef} className={styles['dock-panel']}>
      <ResultTable result={result} height={tableHeight} />
    </div>
  )
}

function ConsoleTab(props: IDockviewPanelHeaderProps) {
  return <DockviewDefaultTab {...props} hideClose />
}

const DOCK_COMPONENTS = {
  'sql-editor': SqlEditorPanel,
  result: ResultPanel,
}

function templateInitialSQL(template?: QueryTemplate): string {
  if (!template) return ''
  if (template.sql) return template.sql
  // 兼容早期的表模式数据集：按已保存的输出列还原一段可预览的 SQL。
  const where = template.where ? `\nWHERE ${template.where}` : ''
  return `SELECT ${template.columns.join(', ')}\nFROM ${template.table}${where}`
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

export default function QueryTemplateWizardPage() {
  const { id } = useParams()
  const templateId = Number(id ?? 0)
  const isEdit = Number.isFinite(templateId) && templateId > 0
  const templateQuery = useQueryTemplate(isEdit ? templateId : 0)

  if (isEdit && templateQuery.isLoading) {
    return <Spin style={{ display: 'block', margin: '80px auto' }} />
  }
  if (isEdit && templateQuery.isError) {
    return (
      <Alert
        type="error"
        content={errorMessage(templateQuery.error, '读取数据集失败')}
      />
    )
  }

  return <QueryTemplateWizard template={isEdit ? templateQuery.data : undefined} />
}

function QueryTemplateWizard({ template }: { template?: QueryTemplate }) {
  const navigate = useNavigate()
  const sourcesQuery = useDataSources('')
  const createMutation = useCreateQueryTemplate()
  const updateMutation = useUpdateQueryTemplate()

  const [step, setStep] = useState(0)
  const [name, setName] = useState(template?.name ?? '')
  const [description, setDescription] = useState(template?.description ?? '')
  const [dataSourceId, setDataSourceIdState] = useState<number | undefined>(
    template?.dataSourceId,
  )
  const [sql, setSql] = useState(() => templateInitialSQL(template))
  const [result, setResult] = useState<QueryResult>()
  const [resultSql, setResultSql] = useState('')
  const [selectedColumns, setSelectedColumns] = useState<string[]>(
    template?.columns ?? [],
  )
  const [error, setError] = useState('')
  const [saveError, setSaveError] = useState('')
  const [saving, setSaving] = useState(false)
  const runQuery = useRunQuery(dataSourceId ?? 0)
  const savedSql = templateInitialSQL(template)

  const dataSourceOptions = (sourcesQuery.data ?? [])
    .filter((ds) => ds.type !== 'filesystem')
    .map((ds) => ({
      value: ds.id,
      label: `${ds.name}（${ds.type}）`,
    }))
  const dataSourceName =
    (sourcesQuery.data ?? []).find((ds) => ds.id === dataSourceId)?.name ?? '-'
  const stale = Boolean(result) && sql !== resultSql

  const setDataSourceId = (value?: number) => {
    setDataSourceIdState(value)
    setResult(undefined)
    setResultSql('')
    setSelectedColumns([])
    setError('')
  }

  const execute = async () => {
    if (!dataSourceId) {
      setError('请先在第一步选择数据源')
      return
    }
    if (!sql.trim()) {
      setError('SQL 不能为空')
      return
    }
    setError('')
    try {
      const next = await runQuery.mutateAsync(sql)
      setResult(next)
      setResultSql(sql)
      setSelectedColumns(next.columns)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : '查询执行失败')
    }
  }

  const loadDemo = () => {
    setSql(DEMO_SQL)
    setResult(DEMO_RESULT)
    setResultSql(DEMO_SQL)
    setSelectedColumns(DEMO_RESULT.columns)
    setError('')
  }

  const canNext = useMemo(() => {
    if (step === 0) return Boolean(name.trim() && dataSourceId)
    if (step === 1) {
      if (result) return !stale
      // 编辑已有数据集时，不重新预览也可以沿用已保存的输出字段。
      return Boolean(template && selectedColumns.length > 0 && sql === savedSql)
    }
    return selectedColumns.length > 0
  }, [step, name, dataSourceId, result, stale, template, selectedColumns, sql, savedSql])

  const next = () => {
    if (canNext && step < 2) setStep(step + 1)
  }
  const prev = () => {
    if (step > 0) setStep(step - 1)
  }

  const save = async () => {
    if (!name.trim() || !dataSourceId) {
      setSaveError('请先补全名称并选择数据源')
      setStep(0)
      return
    }
    if (!sql.trim()) {
      setSaveError('SQL 不能为空')
      setStep(1)
      return
    }
    if (selectedColumns.length === 0) {
      setSaveError('请至少选择一个结果字段')
      return
    }

    const input: QueryTemplateInput = {
      name: name.trim(),
      description: description.trim(),
      dataSourceId,
      sql: sql.trim(),
      columns: selectedColumns,
    }
    setSaving(true)
    setSaveError('')
    try {
      if (template) {
        await updateMutation.mutateAsync({ id: template.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      Message.success(template ? '数据集已更新' : '数据集已创建')
      navigate('/datasets')
    } catch (err) {
      setSaveError(errorMessage(err, '保存数据集失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card
      title={template ? '编辑数据集' : '新建数据集'}
      extra={template ? <Tag color="arcoblue">#{template.id}</Tag> : undefined}
      className={listStyles.page}
      style={{ maxWidth: 1180, margin: '0 auto' }}
    >
      <Steps
        current={step + 1}
        onChange={(current) => setStep(current - 1)}
        style={{ marginBottom: 24 }}
      >
        {STEP_ITEMS.map((item) => (
          <Steps.Step key={item.title} title={item.title} description={item.description} />
        ))}
      </Steps>

      {saveError ? (
        <Alert
          type="error"
          content={saveError}
          closable
          onClose={() => setSaveError('')}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <div className={styles['wizard-body']}>
        {step === 0 ? (
          <div style={{ maxWidth: 620, margin: '0 auto' }}>
            <BasicInfo
              name={name}
              setName={setName}
              description={description}
              setDescription={setDescription}
              dataSourceId={dataSourceId}
              setDataSourceId={setDataSourceId}
              dataSourceOptions={dataSourceOptions}
              loading={sourcesQuery.isLoading}
            />
          </div>
        ) : null}
        {step === 1 ? (
          <SqlConsole
            sql={sql}
            setSql={setSql}
            dataSourceId={dataSourceId}
            running={runQuery.isPending}
            result={result}
            stale={stale}
            error={error}
            setError={setError}
            execute={execute}
            loadDemo={loadDemo}
          />
        ) : null}
        {step === 2 ? (
          <ResultFields
            result={result}
            selectedColumns={selectedColumns}
            setSelectedColumns={setSelectedColumns}
            dataSourceName={dataSourceName}
            name={name}
            description={description}
          />
        ) : null}
      </div>

      <Divider />
      <div style={step === 0 ? { maxWidth: 620, margin: '0 auto' } : undefined}>
        <Footer
          step={step}
          canNext={canNext}
          saving={saving}
          prev={prev}
          next={next}
          save={save}
        />
      </div>
    </Card>
  )
}

function BasicInfo({
  name,
  setName,
  description,
  setDescription,
  dataSourceId,
  setDataSourceId,
  dataSourceOptions,
  loading,
}: {
  name: string
  setName: (value: string) => void
  description: string
  setDescription: (value: string) => void
  dataSourceId?: number
  setDataSourceId: (value?: number) => void
  dataSourceOptions: { value: number; label: string }[]
  loading: boolean
}) {
  return (
    <div>
      <Typography.Paragraph type="secondary">
        数据集保存后会作为 datax任务定义的读取端复用。名称用于在任务里选择。
      </Typography.Paragraph>

      <FieldLabel text="名称" required>
        <Input
          value={name}
          onChange={setName}
          placeholder="例如：订单日增量读取"
          maxLength={100}
        />
      </FieldLabel>

      <FieldLabel text="数据源" required>
        <Select
          showSearch
          options={dataSourceOptions}
          value={dataSourceId}
          onChange={setDataSourceId}
          placeholder="选择 SQL 控制台使用哪个数据源"
          loading={loading}
        />
      </FieldLabel>

      <FieldLabel text="说明">
        <Input.TextArea
          value={description}
          onChange={setDescription}
          rows={3}
          maxLength={500}
          placeholder="可选：记录这个数据集的用途、约定或负责人"
        />
      </FieldLabel>
    </div>
  )
}

function FieldLabel({
  text,
  required,
  children,
}: {
  text: string
  required?: boolean
  children: ReactNode
}) {
  return (
    <div style={{ marginBottom: 18 }}>
      <Typography.Text style={{ display: 'block', marginBottom: 6, fontSize: 13 }}>
        {text}
        {required ? <span style={{ color: 'rgb(var(--danger-6))' }}> *</span> : null}
      </Typography.Text>
      {children}
    </div>
  )
}

function SqlConsole({
  sql,
  setSql,
  dataSourceId,
  running,
  result,
  stale,
  error,
  setError,
  execute,
  loadDemo,
}: {
  sql: string
  setSql: (value: string) => void
  dataSourceId?: number
  running: boolean
  result?: QueryResult
  stale: boolean
  error: string
  setError: (value: string) => void
  execute: () => Promise<void>
  loadDemo: () => void
}) {
  const [dockApi, setDockApi] = useState<DockviewApi>()

  useEffect(() => {
    if (!dockApi) return

    if (!dockApi.getPanel('sql-editor')) {
      dockApi.addPanel({ id: 'sql-editor', component: 'sql-editor', title: 'SQL' })
    }

    const resultPanel = dockApi.getPanel('result')
    if (!result) {
      if (resultPanel) dockApi.removePanel(resultPanel)
      return
    }
    if (resultPanel) {
      resultPanel.api.setActive()
      return
    }

    dockApi.addPanel({
      id: 'result',
      component: 'result',
      title: '查询结果',
      position: { referencePanel: 'sql-editor', direction: 'below' },
      initialHeight: 260,
    })
  }, [dockApi, result])

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12, flex: 1, minHeight: 0 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <Button
          type="primary"
          icon={<IconPlayArrow />}
          loading={running}
          disabled={!dataSourceId}
          onClick={execute}
        >
          执行预览
        </Button>
        <Button icon={<IconCode />} onClick={loadDemo}>
          使用示例数据
        </Button>
        <Typography.Text type="secondary">只读查询，最多返回 200 行</Typography.Text>
        {result ? (
          <Typography.Text type="secondary">
            返回 {result.rows.length} 行 · {result.elapsedMs} ms
            {result.truncated ? ' · 已截断' : ''}
          </Typography.Text>
        ) : null}
        {!dataSourceId ? (
          <Typography.Text type="warning">请先在第一步选择数据源</Typography.Text>
        ) : null}
      </div>

      {error ? (
        <Alert type="error" content={error} closable onClose={() => setError('')} />
      ) : null}
      {stale ? (
        <Alert
          type="warning"
          content="SQL 已修改，请重新执行预览后再进入下一步。"
        />
      ) : null}

      <ConsoleContext.Provider value={{ sql, setSql, result }}>
        <div className={styles['dock-area']}>
          <DockviewReact
            components={DOCK_COMPONENTS}
            defaultTabComponent={ConsoleTab}
            theme={themeLight}
            onReady={(event) => setDockApi(event.api)}
          />
        </div>
      </ConsoleContext.Provider>

      <Typography.Text type="secondary">
        数据集会以 SQL 单通道读取（querySql）；同步不依赖行序，保存后会忽略最外层
        ORDER BY。
      </Typography.Text>
    </div>
  )
}

function ResultTable({ result, height = 240 }: { result: QueryResult; height?: number }) {
  const rows = result.rows.map((row, index) => {
    const item: Record<string, CellValue> = { __index: index + 1 }
    result.columns.forEach((column, columnIndex) => {
      item[column] = row[columnIndex] ?? null
    })
    return item
  })

  const columns: TableColumnProps<Record<string, CellValue>>[] = [
    { title: '#', dataIndex: '__index', width: 56 },
    ...result.columns.map((column) => ({
      title: column,
      dataIndex: column,
      width: 160,
      ellipsis: true,
      render: (value: CellValue) =>
        value === null ? (
          <Typography.Text type="secondary" style={{ fontStyle: 'italic' }}>
            NULL
          </Typography.Text>
        ) : (
          String(value)
        ),
    })),
  ]

  return (
    <Table<Record<string, CellValue>>
      rowKey="__index"
      size="small"
      tableLayoutFixed
      columns={columns}
      data={rows}
      pagination={false}
      scroll={{ x: Math.max(result.columns.length * 160 + 56, 600), y: height }}
    />
  )
}

function ResultFields({
  result,
  selectedColumns,
  setSelectedColumns,
  dataSourceName,
  name,
  description,
}: {
  result?: QueryResult
  selectedColumns: string[]
  setSelectedColumns: (columns: string[]) => void
  dataSourceName: string
  name: string
  description: string
}) {
  const availableColumns = result?.columns ?? selectedColumns
  const selectedSet = new Set(selectedColumns)
  const numeric = useMemo(() => {
    const resultSet = new Set<string>()
    if (!result) return resultSet
    result.rows.slice(0, 20).forEach((row) => {
      result.columns.forEach((column, index) => {
        if (typeof row[index] === 'number') resultSet.add(column)
      })
    })
    return resultSet
  }, [result])

  const toggleColumn = (column: string, checked: boolean) => {
    if (checked) {
      setSelectedColumns(
        availableColumns.filter((item) => item === column || selectedSet.has(item)),
      )
    } else {
      setSelectedColumns(selectedColumns.filter((item) => item !== column))
    }
  }

  return (
    <Row gutter={24}>
      <Col span={14}>
        <Card
          title="输出字段"
          extra={
            <Space>
              <Button size="small" onClick={() => setSelectedColumns(availableColumns)}>
                全选
              </Button>
              <Button size="small" onClick={() => setSelectedColumns([])}>
                清空
              </Button>
            </Space>
          }
        >
          <Typography.Paragraph type="secondary">
            结果字段就是数据集对外暴露的读取列：
            {result
              ? `SQL 返回 ${result.columns.length} 列，当前勾选 ${selectedColumns.length} 列。`
              : `当前保留 ${selectedColumns.length} 个输出字段。`}
            datax任务的写入端列数必须与勾选数量一致，顺序沿用 SQL 结果的列顺序。
          </Typography.Paragraph>
          {availableColumns.length === 0 ? (
            <Alert
              type="warning"
              content="还没有可选的输出字段。请回到第二步执行一次 SQL 预览。"
            />
          ) : (
            <Space direction="vertical" style={{ width: '100%' }}>
              {availableColumns.map((column) => (
                <Checkbox
                  key={column}
                  checked={selectedSet.has(column)}
                  onChange={(checked) => toggleColumn(column, checked)}
                >
                  <Space>
                    <span style={{ fontFamily: 'Consolas, monospace' }}>{column}</span>
                    {numeric.has(column) ? <Tag color="green">数值</Tag> : null}
                  </Space>
                </Checkbox>
              ))}
            </Space>
          )}
        </Card>
      </Col>

      <Col span={10}>
        <Card title="执行方式">
          <Descriptions
            column={1}
            data={[
              { label: '读取模式', value: 'SQL 单通道' },
              { label: '分片', value: '不支持' },
              { label: '行序', value: '忽略最外层 ORDER BY' },
            ]}
          />
          <Typography.Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
            SQL 数据集以 querySql 模式执行，DataX 读取器在该模式下不做分片。需要并行分片时，
            请在 datax任务里使用表模式的内联读取配置。
          </Typography.Paragraph>
        </Card>

        <Card title="数据集摘要" style={{ marginTop: 16 }}>
          <Descriptions
            column={1}
            data={[
              { label: '名称', value: name || '-' },
              { label: '数据源', value: dataSourceName },
              { label: '输出字段', value: `${selectedColumns.length} 个` },
              { label: '说明', value: description || '-' },
            ]}
          />
        </Card>
      </Col>
    </Row>
  )
}

function Footer({
  step,
  canNext,
  saving,
  prev,
  next,
  save,
}: {
  step: number
  canNext: boolean
  saving: boolean
  prev: () => void
  next: () => void
  save: () => void
}) {
  return (
    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
      <Button icon={<IconLeft />} disabled={step === 0} onClick={prev}>
        上一步
      </Button>
      <Space>
        {step < 2 ? (
          <Button type="primary" disabled={!canNext} onClick={next}>
            下一步 <IconRight />
          </Button>
        ) : (
          <Button
            type="primary"
            icon={<IconSave />}
            loading={saving}
            disabled={!canNext}
            onClick={save}
          >
            保存数据集
          </Button>
        )}
      </Space>
    </div>
  )
}
