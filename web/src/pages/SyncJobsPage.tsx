import { ImportOutlined, PlusOutlined } from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Col,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Row,
  Select,
  Space,
  Table,
  Tag,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../api/client'
import { useDataSources } from '../api/dataSources'
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
} from '../api/syncJobs'
import JsonEditor from '../components/JsonEditor'
import { downloadJSON, formatTime } from '../utils/format'

interface JobFormValues {
  name: string
  authoringMode: AuthoringMode
  readerDataSourceId?: number
  readerTable?: string
  readerColumns?: string
  readerWhere?: string
  readerSplitPk?: string
  writerDataSourceId?: number
  writerTable?: string
  writerColumns?: string
  writerWriteMode?: string
  channel?: number
  errorLimitRecord?: number
  errorLimitPercentage?: number
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

export default function SyncJobsPage() {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<SyncJob | null>(null)
  const [rawText, setRawText] = useState('')
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [pageError, setPageError] = useState('')

  const [importOpen, setImportOpen] = useState(false)
  const [importName, setImportName] = useState('')
  const [importText, setImportText] = useState('')
  const [importError, setImportError] = useState('')

  const [form] = Form.useForm<JobFormValues>()
  const mode = (Form.useWatch('authoringMode', form) ?? 'form') as AuthoringMode

  const jobsQuery = useSyncJobs(query)
  const dataSourcesQuery = useDataSources('')
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

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    setRawText('')
    form.resetFields()
    form.setFieldsValue({
      authoringMode: 'form',
      writerWriteMode: 'insert',
      channel: 1,
      errorLimitRecord: 0,
      errorLimitPercentage: 0,
    })
    setModalOpen(true)
  }

  const openEdit = (job: SyncJob) => {
    setEditing(job)
    setFormError('')
    form.resetFields()

    if (job.authoringMode === 'form' && job.form) {
      form.setFieldsValue({
        name: job.name,
        authoringMode: 'form',
        readerDataSourceId: job.form.reader.dataSourceId,
        readerTable: job.form.reader.table,
        readerColumns: job.form.reader.column.join(', '),
        readerWhere: job.form.reader.where,
        readerSplitPk: job.form.reader.splitPk,
        writerDataSourceId: job.form.writer.dataSourceId,
        writerTable: job.form.writer.table,
        writerColumns: job.form.writer.column.join(', '),
        writerWriteMode: job.form.writer.writeMode || 'insert',
        channel: job.form.setting.channel,
        errorLimitRecord: job.form.setting.errorLimit.record,
        errorLimitPercentage: job.form.setting.errorLimit.percentage,
      })
      setRawText('')
    } else {
      form.setFieldsValue({ name: job.name, authoringMode: 'raw' })
      setRawText(JSON.stringify(job.raw ?? {}, null, 2))
    }
    setModalOpen(true)
  }

  const closeModal = () => {
    setModalOpen(false)
    setFormError('')
    form.resetFields()
  }

  const submit = async () => {
    let values: JobFormValues
    try {
      values = await form.validateFields()
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
        input = { name: values.name, authoringMode: 'raw', raw: parsed }
      } else {
        const readerColumns = splitColumns(values.readerColumns)
        const writerColumns = splitColumns(values.writerColumns)
        if (readerColumns.length !== writerColumns.length) {
          setFormError('读取列与写入列数量必须一致')
          return
        }
        input = {
          name: values.name,
          authoringMode: 'form',
          form: {
            reader: {
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
              writeMode: values.writerWriteMode || 'insert',
            },
            setting: {
              channel: values.channel ?? 1,
              errorLimit: {
                record: values.errorLimitRecord ?? 0,
                percentage: values.errorLimitPercentage ?? 0,
              },
            },
          },
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
    } catch (err) {
      setImportError(errorMessage(err, '导入失败'))
    }
  }

  const columns: ColumnsType<SyncJob> = [
    { title: '名称', dataIndex: 'name', key: 'name', width: 200, ellipsis: true },
    {
      title: '模式',
      dataIndex: 'authoringMode',
      key: 'authoringMode',
      width: 110,
      render: (value: AuthoringMode) => (
        <Tag color={value === 'form' ? 'blue' : 'purple'}>{MODE_LABELS[value]}</Tag>
      ),
    },
    { title: '摘要', dataIndex: 'summary', key: 'summary', ellipsis: true },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      key: 'updatedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 330,
      render: (_, job) => (
        <Space size={4}>
          <Button type="link" onClick={() => handleRun(job)}>
            运行
          </Button>
          <Button type="link" onClick={() => openEdit(job)}>
            编辑
          </Button>
          <Button type="link" onClick={() => duplicateMutation.mutate(job.id)}>
            复制
          </Button>
          <Popconfirm
            title="导出文件包含数据库连接凭据，确认导出？"
            okText="确定"
            cancelText="取消"
            onConfirm={() => handleExport(job)}
          >
            <Button type="link">导出</Button>
          </Popconfirm>
          <Popconfirm
            title="确认删除该任务？"
            description="运行历史会保留。"
            okText="确定"
            cancelText="取消"
            onConfirm={() => handleDelete(job)}
          >
            <Button type="link" danger>
              删除
            </Button>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  return (
    <Card
      title="同步任务"
      extra={
        <Space>
          <Button icon={<ImportOutlined />} onClick={() => setImportOpen(true)}>
            导入 JSON
          </Button>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
            新建任务
          </Button>
        </Space>
      }
    >
      <Space style={{ marginBottom: 16 }}>
        <Input.Search
          allowClear
          placeholder="按名称搜索"
          style={{ width: 280 }}
          onSearch={(value) => setQuery(value.trim())}
        />
      </Space>

      {pageError ? (
        <Alert
          closable
          showIcon
          type="error"
          message={pageError}
          style={{ marginBottom: 16 }}
          onClose={() => setPageError('')}
        />
      ) : null}

      <Table<SyncJob>
        rowKey="id"
        tableLayout="fixed"
        columns={columns}
        dataSource={jobsQuery.data ?? []}
        loading={jobsQuery.isLoading}
        pagination={false}
        locale={{ emptyText: '还没有同步任务' }}
      />

      <Modal
        title={editing ? '编辑同步任务' : '新建同步任务'}
        open={modalOpen}
        width={780}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        onOk={submit}
        onCancel={closeModal}
        destroyOnHidden
      >
        {formError ? (
          <Alert type="error" message={formError} showIcon style={{ marginBottom: 16 }} />
        ) : null}

        <Form form={form} layout="vertical">
          <Row gutter={16}>
            <Col span={12}>
              <Form.Item
                name="name"
                label="任务名称"
                rules={[{ required: true, message: '请输入任务名称' }]}
              >
                <Input placeholder="例如：orders-sync" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item
                name="authoringMode"
                label="编写模式"
                rules={[{ required: true, message: '请选择编写模式' }]}
              >
                <Select
                  disabled={Boolean(editing)}
                  options={[
                    { value: 'form', label: '表单模式' },
                    { value: 'raw', label: '原始 JSON' },
                  ]}
                />
              </Form.Item>
            </Col>
          </Row>

          {mode === 'raw' ? (
            <Form.Item label="DataX 配置">
              <JsonEditor value={rawText} onChange={setRawText} />
            </Form.Item>
          ) : (
            <>
              <Card type="inner" title="读取端" size="small" style={{ marginBottom: 16 }}>
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item
                      name="readerDataSourceId"
                      label="数据源"
                      rules={[{ required: true, message: '请选择读取数据源' }]}
                    >
                      <Select options={dataSourceOptions} placeholder="选择数据源" />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item
                      name="readerTable"
                      label="表名"
                      rules={[{ required: true, message: '请输入表名' }]}
                    >
                      <Input placeholder="例如：orders" />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item
                  name="readerColumns"
                  label="列（逗号或换行分隔）"
                  rules={[{ required: true, message: '请输入读取列' }]}
                >
                  <Input.TextArea rows={2} placeholder="id, name, email" />
                </Form.Item>
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item name="readerWhere" label="WHERE 条件">
                      <Input placeholder="可选，例如 status = 'active'" />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item name="readerSplitPk" label="分片字段">
                      <Input placeholder="可选，例如 id" />
                    </Form.Item>
                  </Col>
                </Row>
              </Card>

              <Card type="inner" title="写入端" size="small" style={{ marginBottom: 16 }}>
                <Row gutter={16}>
                  <Col span={12}>
                    <Form.Item
                      name="writerDataSourceId"
                      label="数据源"
                      rules={[{ required: true, message: '请选择写入数据源' }]}
                    >
                      <Select options={dataSourceOptions} placeholder="选择数据源" />
                    </Form.Item>
                  </Col>
                  <Col span={12}>
                    <Form.Item
                      name="writerTable"
                      label="表名"
                      rules={[{ required: true, message: '请输入表名' }]}
                    >
                      <Input placeholder="例如：orders_copy" />
                    </Form.Item>
                  </Col>
                </Row>
                <Form.Item
                  name="writerColumns"
                  label="列（与读取列数量一致）"
                  rules={[{ required: true, message: '请输入写入列' }]}
                >
                  <Input.TextArea rows={2} placeholder="id, name, email" />
                </Form.Item>
                <Form.Item name="writerWriteMode" label="写入模式">
                  <Select
                    options={[
                      { value: 'insert', label: 'insert' },
                      { value: 'replace', label: 'replace' },
                      { value: 'update', label: 'update' },
                    ]}
                  />
                </Form.Item>
              </Card>

              <Card type="inner" title="运行设置" size="small">
                <Row gutter={16}>
                  <Col span={8}>
                    <Form.Item name="channel" label="并发通道数">
                      <InputNumber min={1} max={32} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={8}>
                    <Form.Item name="errorLimitRecord" label="错误记录上限">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                  <Col span={8}>
                    <Form.Item name="errorLimitPercentage" label="错误率上限">
                      <InputNumber min={0} max={1} step={0.01} style={{ width: '100%' }} />
                    </Form.Item>
                  </Col>
                </Row>
              </Card>
            </>
          )}
        </Form>
      </Modal>

      <Modal
        title="导入 DataX JSON"
        open={importOpen}
        width={720}
        okText="导入"
        cancelText="取消"
        confirmLoading={importMutation.isPending}
        onOk={submitImport}
        onCancel={() => setImportOpen(false)}
        destroyOnHidden
      >
        {importError ? (
          <Alert type="error" message={importError} showIcon style={{ marginBottom: 16 }} />
        ) : null}
        <Form layout="vertical">
          <Form.Item label="任务名称" required>
            <Input
              value={importName}
              onChange={(event) => setImportName(event.target.value)}
              placeholder="例如：legacy-orders"
            />
          </Form.Item>
          <Form.Item label="从文件读取">
            <input
              type="file"
              accept=".json,application/json"
              onChange={async (event) => {
                const file = event.target.files?.[0]
                if (file) {
                  setImportText(await file.text())
                }
              }}
            />
          </Form.Item>
          <Form.Item label="DataX 配置">
            <JsonEditor value={importText} onChange={setImportText} height="300px" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  )
}
