import {
  Alert,
  Button,
  Card,
  Descriptions,
  Form,
  Grid,
  Input,
  InputNumber,
  Link,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
  Upload,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconCopy,
  IconDelete,
  IconEdit,
  IconExport,
  IconPlayArrow,
  IconPlus,
  IconSearch,
  IconUpload,
} from '@arco-design/web-react/icon'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import { useDataSources } from '../../api/dataSources'
import { useQueryTemplates } from '../../api/queryTemplates'
import {
  exportSyncJob,
  useCreateSyncJob,
  useDeleteSyncJob,
  useDuplicateSyncJob,
  useImportSyncJob,
  useStartRun,
  useSyncJobs,
  useUpdateSyncJob,
  type AuthoringMode,
  type SyncJob,
  type SyncJobInput,
} from '../../api/syncJobs'
import JsonEditor from '../../components/JsonEditor'
import RunStatusTag from '../../components/RunStatusTag'
import listStyles from '../../style/list.module.less'
import { downloadJSON, formatTime } from '../../utils/format'

interface JobFormValues {
  name: string
  authoringMode: AuthoringMode
  readerTemplateId?: number
  readerDataSourceId?: number
  readerTable?: string
  readerColumns?: string
  readerWhere?: string
  readerSplitPk?: string
  writerDataSourceId?: number
  writerTable?: string
  writerColumns?: string
  writerWriteMode?: string
  writerUpdateKeys?: string
  channel?: number
  errorLimitRecord?: number
  errorLimitPercentage?: number
  retryMax?: number
  retryIntervalSeconds?: number
}

const MODE_LABELS: Record<AuthoringMode, string> = {
  form: '表单模式',
  raw: '原始 JSON',
}

function splitColumns(value?: string): string[] {
  if (!value) return []
  return value
    .split(/[\n,]/)
    .map((item) => item.trim())
    .filter(Boolean)
}

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) return err.message
  return fallback
}

function readJsonFile(file: File, onLoad: (content: string) => void, onError: () => void) {
  const reader = new FileReader()
  reader.onload = () => onLoad(String(reader.result ?? ''))
  reader.onerror = onError
  reader.readAsText(file)
}

export default function SyncJobsPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get('q') ?? ''
  const [searchInput, setSearchInput] = useState(query)
  const searchTimer = useRef<number | undefined>(undefined)
  const [modalOpen, setModalOpen] = useState(searchParams.get('action') === 'create')
  const [editing, setEditing] = useState<SyncJob | null>(null)
  const [mode, setMode] = useState<AuthoringMode>('form')
  const [rawText, setRawText] = useState('')
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [pageError, setPageError] = useState('')

  const [importOpen, setImportOpen] = useState(searchParams.get('action') === 'import')
  const [importName, setImportName] = useState('')
  const [importText, setImportText] = useState('')
  const [importFileName, setImportFileName] = useState('')
  const [importError, setImportError] = useState('')

  const [form] = Form.useForm<JobFormValues>()
  const writerDataSourceId = Form.useWatch('writerDataSourceId', form)
  const writerWriteMode = Form.useWatch('writerWriteMode', form)
  const readerTemplateId = Form.useWatch('readerTemplateId', form)
  const jobsQuery = useSyncJobs(query)
  const dataSourcesQuery = useDataSources('')
  const templatesQuery = useQueryTemplates('')
  const createMutation = useCreateSyncJob()
  const updateMutation = useUpdateSyncJob()
  const deleteMutation = useDeleteSyncJob()
  const duplicateMutation = useDuplicateSyncJob()
  const importMutation = useImportSyncJob()
  const startRunMutation = useStartRun()

  const dataSourceOptions = (dataSourcesQuery.data ?? []).map((item) => ({
    value: item.id,
    label: `${item.name}（${item.type}）`,
  }))
  const templateOptions = (templatesQuery.data ?? []).map((template) => ({
    value: template.id,
    label: template.sql
      ? `${template.name}（SQL 数据集）`
      : `${template.name}（${template.table}）`,
  }))
  const selectedReaderTemplate = (templatesQuery.data ?? []).find(
    (template) => template.id === readerTemplateId,
  )
  const dataSourceName = (id: number) =>
    dataSourcesQuery.data?.find((item) => item.id === id)?.name ?? `#${id}`
  const writerDataSource = (dataSourcesQuery.data ?? []).find(
    (item) => item.id === writerDataSourceId,
  )
  const writerType = writerDataSource?.type
  const supportsWriteMode = writerType !== 'postgres' && writerType !== 'mssql'
  const showOracleUpdateKeys =
    writerType === 'oracle' && (writerWriteMode ?? '').toLowerCase() === 'update'

  const navigateToSearch = (value: string) => {
    const keyword = value.trim()
    navigate(keyword ? `/sync-jobs?q=${encodeURIComponent(keyword)}` : '/sync-jobs', {
      replace: true,
    })
  }

  const applySearch = () => {
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    navigateToSearch(searchInput)
  }

  const handleSearchChange = (value: string) => {
    setSearchInput(value)
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)

    if (!value.trim()) {
      navigateToSearch('')
      return
    }
    searchTimer.current = window.setTimeout(() => navigateToSearch(value), 300)
  }

  useEffect(
    () => () => {
      if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    },
    [],
  )

  const openCreate = () => {
    setEditing(null)
    setMode('form')
    setFormError('')
    setRawText('')
    form.resetFields()
    form.setFieldsValue({
      authoringMode: 'form',
      readerTemplateId: 0,
      writerWriteMode: 'insert',
      channel: 1,
      errorLimitRecord: 0,
      errorLimitPercentage: 0,
      retryMax: 0,
      retryIntervalSeconds: 60,
    })
    setModalOpen(true)
  }

  const openEdit = useCallback(
    (job: SyncJob) => {
      setEditing(job)
      setMode(job.authoringMode)
      setFormError('')
      form.resetFields()

      if (job.authoringMode === 'form' && job.form) {
        const rawWriteMode = (job.form.writer.writeMode || 'insert').trim()
        const updateMatch = /^update\s*\((.*)\)$/i.exec(rawWriteMode)
        const writeModeValue = updateMatch ? 'update' : rawWriteMode.toLowerCase()

        form.setFieldsValue({
          name: job.name,
          authoringMode: 'form',
          readerTemplateId: job.form.reader.templateId ?? 0,
          readerDataSourceId: job.form.reader.dataSourceId,
          readerTable: job.form.reader.table,
          readerColumns: job.form.reader.column.join(', '),
          readerWhere: job.form.reader.where,
          readerSplitPk: job.form.reader.splitPk,
          writerDataSourceId: job.form.writer.dataSourceId,
          writerTable: job.form.writer.table,
          writerColumns: job.form.writer.column.join(', '),
          writerWriteMode: writeModeValue,
          writerUpdateKeys: updateMatch ? updateMatch[1] : undefined,
          channel: job.form.setting.channel,
          errorLimitRecord: job.form.setting.errorLimit.record,
          errorLimitPercentage: job.form.setting.errorLimit.percentage,
        })
        setRawText('')
      } else {
        form.setFieldsValue({ name: job.name, authoringMode: 'raw' })
        setRawText(JSON.stringify(job.raw ?? {}, null, 2))
      }
      form.setFieldsValue({
        retryMax: job.retryMax ?? 0,
        retryIntervalSeconds: job.retryIntervalSeconds || 60,
      })
      setModalOpen(true)
    },
    [form],
  )

  const editID = Number(searchParams.get('edit') ?? 0)
  useEffect(() => {
    if (!Number.isFinite(editID) || editID <= 0 || modalOpen) return
    const job = jobsQuery.data?.find((item) => item.id === editID)
    // 从任务详情页带 ?edit= 返回时，等列表加载完再打开编辑弹窗。
    // eslint-disable-next-line react-hooks/set-state-in-effect
    if (job) openEdit(job)
  }, [editID, jobsQuery.data, modalOpen, openEdit])

  const clearAction = () => {
    if (!searchParams.has('action') && !searchParams.has('edit')) return
    const next = new URLSearchParams(searchParams)
    next.delete('action')
    next.delete('edit')
    setSearchParams(next, { replace: true })
  }

  const closeModal = () => {
    setModalOpen(false)
    setFormError('')
    form.resetFields()
    clearAction()
  }

  const submit = async () => {
    let values: JobFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }

    setSaving(true)
    setFormError('')
    try {
      let input: SyncJobInput
      if (mode === 'raw') {
        let parsed: unknown
        try {
          parsed = JSON.parse(rawText)
        } catch {
          setFormError('JSON 格式不正确')
          return
        }
        input = {
          name: values.name,
          authoringMode: 'raw',
          raw: parsed,
          retryMax: values.retryMax ?? 0,
          retryIntervalSeconds: values.retryIntervalSeconds ?? 60,
        }
      } else {
        const readerTemplateId = values.readerTemplateId ?? 0
        if (readerTemplateId > 0 && !selectedReaderTemplate) {
          setFormError('数据集不存在，请重新选择')
          return
        }
        const readerColumns =
          readerTemplateId > 0
            ? (selectedReaderTemplate?.columns ?? [])
            : splitColumns(values.readerColumns)
        const writerColumns = splitColumns(values.writerColumns)
        if (readerColumns.length !== writerColumns.length) {
          setFormError('读取列与写入列数量必须一致')
          return
        }

        const writerSource = (dataSourcesQuery.data ?? []).find(
          (item) => item.id === values.writerDataSourceId,
        )
        let writeMode = (values.writerWriteMode || 'insert').toLowerCase()
        if (writerSource?.type === 'postgres' || writerSource?.type === 'mssql') {
          // 这两种 writer 忽略 writeMode，固定写入 insert 以免产生错误预期。
          writeMode = 'insert'
        } else if (writerSource?.type === 'oracle') {
          if (writeMode === 'update') {
            const keys = splitColumns(values.writerUpdateKeys)
            if (keys.length === 0) {
              setFormError('Oracle 的 update 模式需要填写更新主键列')
              return
            }
            const missing = keys.find(
              (key) =>
                !writerColumns.some((column) => column.toLowerCase() === key.toLowerCase()),
            )
            if (missing) {
              setFormError(`更新主键列 ${missing} 不在写入列中`)
              return
            }
            writeMode = `update(${keys.join(',')})`
          } else {
            writeMode = writeMode.toUpperCase()
          }
        }

        input = {
          name: values.name,
          authoringMode: 'form',
          form: {
            reader:
              readerTemplateId > 0
                ? { dataSourceId: 0, templateId: readerTemplateId, table: '', column: [] }
                : {
                    dataSourceId: values.readerDataSourceId ?? 0,
                    table: values.readerTable ?? '',
                    column: readerColumns,
                    where: values.readerWhere?.trim() || undefined,
                    splitPk: values.readerSplitPk?.trim() || undefined,
                  },
            writer: {
              dataSourceId: values.writerDataSourceId ?? 0,
              table: values.writerTable ?? '',
              column: writerColumns,
              writeMode,
            },
            setting: {
              channel: values.channel ?? 1,
              errorLimit: {
                record: values.errorLimitRecord ?? 0,
                percentage: values.errorLimitPercentage ?? 0,
              },
            },
          },
          retryMax: values.retryMax ?? 0,
          retryIntervalSeconds: values.retryIntervalSeconds ?? 60,
        }
      }

      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      closeModal()
    } catch (err) {
      setFormError(
        err instanceof ApiError && err.code === 'duplicate_name'
          ? '任务名称已存在'
          : errorMessage(err, '保存失败，请稍后重试'),
      )
    } finally {
      setSaving(false)
    }
  }

  const handleRun = async (job: SyncJob) => {
    setPageError('')
    try {
      const run = await startRunMutation.mutateAsync(job.id)
      navigate(`/runs/${run.id}`)
    } catch (err) {
      setPageError(errorMessage(err, '启动运行失败'))
    }
  }

  const handleExport = async (job: SyncJob) => {
    setPageError('')
    try {
      downloadJSON(`${job.name}.json`, await exportSyncJob(job.id))
    } catch (err) {
      setPageError(errorMessage(err, '导出失败'))
    }
  }

  const handleDelete = async (job: SyncJob) => {
    setPageError('')
    try {
      await deleteMutation.mutateAsync(job.id)
    } catch (err) {
      setPageError(errorMessage(err, '删除失败'))
    }
  }

  const submitImport = async () => {
    setImportError('')
    if (!importName.trim()) {
      setImportError('请输入任务名称')
      return
    }
    let config: unknown
    try {
      config = JSON.parse(importText)
    } catch {
      setImportError('JSON 格式不正确')
      return
    }

    try {
      await importMutation.mutateAsync({ name: importName.trim(), config })
      setImportOpen(false)
      setImportName('')
      setImportText('')
      setImportFileName('')
      clearAction()
    } catch (err) {
      setImportError(errorMessage(err, '导入失败'))
    }
  }

  const columns: TableColumnProps<SyncJob>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 200,
      ellipsis: true,
      render: (value: string, job) => (
        <Link onClick={() => navigate(`/sync-jobs/${job.id}`)}>{value}</Link>
      ),
    },
    {
      title: '模式',
      dataIndex: 'authoringMode',
      width: 110,
      render: (value: AuthoringMode) => (
        <Tag color={value === 'form' ? 'blue' : 'purple'}>{MODE_LABELS[value]}</Tag>
      ),
    },
    { title: '摘要', dataIndex: 'summary', ellipsis: true },
    {
      title: '最近运行',
      width: 230,
      render: (_, job) => {
        const lastRun = job.lastRun
        if (!lastRun) {
          return <Typography.Text type="secondary">未运行</Typography.Text>
        }
        return (
          <Space size={4}>
            <RunStatusTag status={lastRun.status} />
            <Link onClick={() => navigate(`/runs/${lastRun.id}`)}>
              {formatTime(lastRun.startedAt)}
            </Link>
          </Space>
        )
      },
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 220,
      fixed: 'right',
      render: (_, job) => (
        <Space size={2}>
          <Tooltip content="运行">
            <Button
              type="text"
              size="small"
              icon={<IconPlayArrow />}
              onClick={() => handleRun(job)}
            />
          </Tooltip>
          <Tooltip content="编辑">
            <Button type="text" size="small" icon={<IconEdit />} onClick={() => openEdit(job)} />
          </Tooltip>
          <Tooltip content="复制">
            <Button
              type="text"
              size="small"
              icon={<IconCopy />}
              onClick={() => duplicateMutation.mutate(job.id)}
            />
          </Tooltip>
          <Tooltip content="导出（含凭据）">
            <Popconfirm
              content="导出文件包含数据库连接凭据，确认导出？"
              okText="确定"
              cancelText="取消"
              onOk={() => handleExport(job)}
            >
              <Button type="text" size="small" icon={<IconExport />} />
            </Popconfirm>
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该任务？运行历史会保留。"
              okText="确定"
              cancelText="取消"
              onOk={() => handleDelete(job)}
            >
              <Button type="text" status="danger" size="small" icon={<IconDelete />} />
            </Popconfirm>
          </Tooltip>
        </Space>
      ),
    },
  ]

  return (
    <Card>
      <Typography.Title heading={6}>datax任务</Typography.Title>

      <div className={listStyles.toolbar}>
        <Input
          allowClear
          aria-label="任务名称"
          className={listStyles['search-input']}
          prefix={<IconSearch />}
          placeholder="请输入任务名称"
          value={searchInput}
          onChange={handleSearchChange}
          onPressEnter={applySearch}
        />

        <div className={listStyles.actions}>
          <Space>
            <Button icon={<IconUpload />} onClick={() => setImportOpen(true)}>
              导入 JSON
            </Button>
            <Button type="primary" icon={<IconPlus />} onClick={openCreate}>
              新建任务
            </Button>
          </Space>
        </div>
      </div>

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          style={{ marginBottom: 16 }}
          onClose={() => setPageError('')}
        />
      ) : null}

      <Table<SyncJob>
        rowKey="id"
        tableLayoutFixed
        columns={columns}
        data={jobsQuery.data ?? []}
        loading={jobsQuery.isLoading}
        pagination={false}
        scroll={{ x: 1240 }}
        noDataElement={<span>还没有 datax任务</span>}
      />

      <Modal
        title={editing ? '编辑 datax任务' : '新建 datax任务'}
        visible={modalOpen}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        onOk={submit}
        onCancel={closeModal}
        unmountOnExit
        style={{ width: 800 }}
      >
        {formError ? <Alert type="error" content={formError} style={{ marginBottom: 16 }} /> : null}

        <Form
          form={form}
          layout="vertical"
          initialValues={{
            authoringMode: 'form',
            readerTemplateId: 0,
            writerWriteMode: 'insert',
            channel: 1,
            errorLimitRecord: 0,
            errorLimitPercentage: 0,
            retryMax: 0,
            retryIntervalSeconds: 60,
          }}
        >
          <Grid.Row gutter={16}>
            <Grid.Col span={12}>
              <Form.Item
                field="name"
                label="任务名称"
                rules={[{ required: true, message: '请输入任务名称' }]}
              >
                <Input placeholder="例如：orders-sync" />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={12}>
              <Form.Item
                field="authoringMode"
                label="编写模式"
                rules={[{ required: true, message: '请选择编写模式' }]}
              >
                <Select
                  disabled={Boolean(editing)}
                  options={[
                    { value: 'form', label: '表单模式' },
                    { value: 'raw', label: '原始 JSON' },
                  ]}
                  onChange={(value) => setMode(value as AuthoringMode)}
                />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>

          {mode === 'raw' ? (
            <>
              <Alert
                type="info"
                content="当前引擎仅执行 job.content[0]；transformer 已支持 dx_substr、dx_pad、dx_replace、dx_filter、dx_digest 与自定义脚本 dx_script，保存时会校验配置。"
                style={{ marginBottom: 12 }}
              />
              <Form.Item label="DataX 配置">
                <JsonEditor value={rawText} onChange={setRawText} />
              </Form.Item>
            </>
          ) : (
            <>
              <Card title="读取端" style={{ marginBottom: 16 }}>
                <Form.Item
                  field="readerTemplateId"
                  label="读取数据集"
                  extra="选择数据集后，读取端由数据集统一维护；不选则使用下方自定义读取"
                >
                  <Select
                    options={[{ value: 0, label: '自定义读取' }, ...templateOptions]}
                    placeholder="自定义读取"
                  />
                </Form.Item>

                {selectedReaderTemplate ? (
                  <div
                    style={{
                      padding: '10px 12px',
                      borderRadius: 6,
                      background: 'var(--color-fill-2)',
                    }}
                  >
                    <Descriptions
                      column={2}
                      data={
                        selectedReaderTemplate.sql
                          ? [
                              { label: '数据集', value: selectedReaderTemplate.name },
                              {
                                label: '数据源',
                                value: dataSourceName(selectedReaderTemplate.dataSourceId),
                              },
                              { label: '读取模式', value: 'SQL 单通道（querySql）' },
                              {
                                label: '列',
                                value: selectedReaderTemplate.columns.join(', '),
                              },
                            ]
                          : [
                              { label: '数据集', value: selectedReaderTemplate.name },
                              {
                                label: '数据源',
                                value: dataSourceName(selectedReaderTemplate.dataSourceId),
                              },
                              { label: '表', value: selectedReaderTemplate.table },
                              {
                                label: '列',
                                value: selectedReaderTemplate.columns.join(', '),
                              },
                              { label: 'WHERE', value: selectedReaderTemplate.where || '-' },
                              {
                                label: '分片字段',
                                value: selectedReaderTemplate.splitPk || '-',
                              },
                            ]
                      }
                    />
                    {selectedReaderTemplate.sql ? (
                      <pre
                        style={{
                          margin: '0 0 8px',
                          padding: 10,
                          borderRadius: 6,
                          background: 'var(--color-bg-2)',
                          fontSize: 12,
                          lineHeight: 1.6,
                          maxHeight: 160,
                          overflow: 'auto',
                          whiteSpace: 'pre-wrap',
                          wordBreak: 'break-all',
                        }}
                      >
                        {selectedReaderTemplate.sql}
                      </pre>
                    ) : null}
                    <Link onClick={() => navigate('/datasets')}>管理数据集</Link>
                  </div>
                ) : (
                  <>
                    <Grid.Row gutter={16}>
                      <Grid.Col span={12}>
                        <Form.Item
                          field="readerDataSourceId"
                          label="数据源"
                          rules={[{ required: true, message: '请选择读取数据源' }]}
                        >
                          <Select options={dataSourceOptions} placeholder="选择数据源" />
                        </Form.Item>
                      </Grid.Col>
                      <Grid.Col span={12}>
                        <Form.Item
                          field="readerTable"
                          label="表名"
                          rules={[{ required: true, message: '请输入表名' }]}
                        >
                          <Input placeholder="例如：orders" />
                        </Form.Item>
                      </Grid.Col>
                    </Grid.Row>
                    <Form.Item
                      field="readerColumns"
                      label="列（逗号或换行分隔）"
                      rules={[{ required: true, message: '请输入读取列' }]}
                    >
                      <Input.TextArea rows={2} placeholder="id, name, email" />
                    </Form.Item>
                    <Grid.Row gutter={16}>
                      <Grid.Col span={12}>
                        <Form.Item field="readerWhere" label="WHERE 条件">
                          <Input placeholder="可选，例如 status = 'active'" />
                        </Form.Item>
                      </Grid.Col>
                      <Grid.Col span={12}>
                        <Form.Item field="readerSplitPk" label="分片字段">
                          <Input placeholder="可选，例如 id" />
                        </Form.Item>
                      </Grid.Col>
                    </Grid.Row>
                  </>
                )}
              </Card>

              <Card title="写入端" style={{ marginBottom: 16 }}>
                <Grid.Row gutter={16}>
                  <Grid.Col span={12}>
                    <Form.Item
                      field="writerDataSourceId"
                      label="数据源"
                      rules={[{ required: true, message: '请选择写入数据源' }]}
                    >
                      <Select options={dataSourceOptions} placeholder="选择数据源" />
                    </Form.Item>
                  </Grid.Col>
                  <Grid.Col span={12}>
                    <Form.Item
                      field="writerTable"
                      label="表名"
                      rules={[{ required: true, message: '请输入表名' }]}
                    >
                      <Input placeholder="例如：orders_copy" />
                    </Form.Item>
                  </Grid.Col>
                </Grid.Row>
                <Form.Item
                  field="writerColumns"
                  label="列（与读取列数量一致）"
                  rules={[{ required: true, message: '请输入写入列' }]}
                >
                  <Input.TextArea rows={2} placeholder="id, name, email" />
                </Form.Item>
                {supportsWriteMode ? (
                  <Form.Item field="writerWriteMode" label="写入模式">
                    <Select
                      options={[
                        { value: 'insert', label: 'insert' },
                        { value: 'replace', label: 'replace' },
                        { value: 'update', label: 'update' },
                      ]}
                    />
                  </Form.Item>
                ) : (
                  <Form.Item label="写入模式">
                    <Typography.Text type="secondary">
                      {writerType === 'postgres' ? 'PostgreSQL' : 'SQL Server'}{' '}
                      写入器按主键自动 upsert，不支持 writeMode
                    </Typography.Text>
                  </Form.Item>
                )}
                {showOracleUpdateKeys ? (
                  <Form.Item
                    field="writerUpdateKeys"
                    label="更新主键列"
                    extra="逗号分隔，必须包含在写入列中，例如 id, code"
                    rules={[{ required: true, message: '请输入更新主键列' }]}
                  >
                    <Input placeholder="id, code" />
                  </Form.Item>
                ) : null}
              </Card>

              <Card title="运行设置">
                <Alert
                  type="info"
                  content="错误记录/错误率上限当前对 Transformer 产生的脏数据生效；Reader/Writer 自身的失败暂未纳入该限制。"
                  style={{ marginBottom: 12 }}
                />
                <Grid.Row gutter={16}>
                  <Grid.Col span={8}>
                    <Form.Item field="channel" label="并发通道数">
                      <InputNumber min={1} max={32} style={{ width: '100%' }} />
                    </Form.Item>
                  </Grid.Col>
                  <Grid.Col span={8}>
                    <Form.Item field="errorLimitRecord" label="错误记录上限">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Grid.Col>
                  <Grid.Col span={8}>
                    <Form.Item field="errorLimitPercentage" label="错误率上限">
                      <InputNumber min={0} max={1} step={0.01} style={{ width: '100%' }} />
                    </Form.Item>
                  </Grid.Col>
                </Grid.Row>
              </Card>
            </>
          )}

          <Card title="运行策略">
            <Grid.Row gutter={16}>
              <Grid.Col span={8}>
                <Form.Item
                  field="retryMax"
                  label="失败重试次数"
                  extra="0 表示不自动重试，最多 10 次"
                >
                  <InputNumber min={0} max={10} style={{ width: '100%' }} />
                </Form.Item>
              </Grid.Col>
              <Grid.Col span={8}>
                <Form.Item field="retryIntervalSeconds" label="重试间隔（秒）">
                  <InputNumber min={0} max={86400} style={{ width: '100%' }} />
                </Form.Item>
              </Grid.Col>
            </Grid.Row>
          </Card>
        </Form>
      </Modal>

      <Modal
        title="导入 DataX JSON"
        visible={importOpen}
        okText="导入"
        cancelText="取消"
        confirmLoading={importMutation.isPending}
        onOk={submitImport}
        onCancel={() => {
          setImportOpen(false)
          clearAction()
        }}
        unmountOnExit
        style={{ width: 760 }}
      >
        {importError ? (
          <Alert type="error" content={importError} style={{ marginBottom: 16 }} />
        ) : null}
        <Form layout="vertical">
          <Form.Item label="任务名称" required>
            <Input
              value={importName}
              onChange={setImportName}
              placeholder="例如：legacy-orders"
            />
          </Form.Item>
          <Form.Item label="从文件读取">
            <Space direction="vertical">
              <Upload
                accept=".json,application/json"
                showUploadList={false}
                beforeUpload={(file) => {
                  readJsonFile(
                    file,
                    (content) => {
                      setImportText(content)
                      setImportFileName(file.name)
                      setImportError('')
                    },
                    () => setImportError('读取文件失败'),
                  )
                  return false
                }}
              >
                <Button icon={<IconUpload />}>选择 JSON 文件</Button>
              </Upload>
              {importFileName ? (
                <Typography.Text type="secondary">已读取：{importFileName}</Typography.Text>
              ) : null}
            </Space>
          </Form.Item>
          <Form.Item label="DataX 配置">
            <JsonEditor value={importText} onChange={setImportText} height="300px" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
