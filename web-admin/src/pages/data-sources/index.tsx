import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Grid,
  Input,
  Link,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconEye,
  IconPlus,
  IconRefresh,
  IconSearch,
  IconSync,
} from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import {
  testDataSource,
  useCreateDataSource,
  useDataSources,
  useDeleteDataSource,
  useUpdateDataSource,
  type ConnectionTestResult,
  type DataSource,
  type DataSourceInput,
  type DataSourceType,
} from '../../api/dataSources'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'

const DATA_SOURCE_TYPES: Array<{ value: DataSourceType; label: string; placeholder: string }> = [
  { value: 'mysql', label: 'MySQL', placeholder: 'jdbc:mysql://host:3306/db' },
  { value: 'postgres', label: 'PostgreSQL', placeholder: 'jdbc:postgresql://host:5432/db' },
  { value: 'sqlite', label: 'SQLite', placeholder: 'jdbc:sqlite:/path/to/database.db' },
  { value: 'mssql', label: 'SQL Server', placeholder: 'jdbc:sqlserver://host:1433;DatabaseName=db' },
  { value: 'oracle', label: 'Oracle', placeholder: 'jdbc:oracle:thin:@//host:1521/service' },
]

const TYPE_LABELS = Object.fromEntries(
  DATA_SOURCE_TYPES.map(({ value, label }) => [value, label]),
) as Record<DataSourceType, string>

const JDBC_PLACEHOLDERS = Object.fromEntries(
  DATA_SOURCE_TYPES.map(({ value, placeholder }) => [value, placeholder]),
) as Record<DataSourceType, string>

function describeTestResult(result: ConnectionTestResult): string {
  return result.ok ? `${result.message}（${result.latencyMs} ms）` : `连接失败：${result.message}`
}

function testFailure(err: unknown): ConnectionTestResult {
  return {
    ok: false,
    latencyMs: 0,
    message: err instanceof ApiError ? err.message : '测试失败，请稍后重试',
  }
}

export default function DataSourcesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState('')
  const [modalOpen, setModalOpen] = useState(searchParams.get('action') === 'create')
  const [editing, setEditing] = useState<DataSource | null>(null)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null)
  const [pageTestResult, setPageTestResult] = useState<ConnectionTestResult | null>(null)
  const [pageError, setPageError] = useState('')
  const [formType, setFormType] = useState<DataSourceType>('mysql')

  const [form] = Form.useForm<DataSourceInput>()
  const [searchForm] = Form.useForm<{ name?: string }>()
  const listQuery = useDataSources(query)
  const createMutation = useCreateDataSource()
  const updateMutation = useUpdateDataSource()
  const deleteMutation = useDeleteDataSource()

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    setTestResult(null)
    setFormType('mysql')
    form.resetFields()
    form.setFieldsValue({ type: 'mysql' })
    setModalOpen(true)
  }

  const clearAction = () => {
    if (!searchParams.has('action')) return
    const next = new URLSearchParams(searchParams)
    next.delete('action')
    setSearchParams(next, { replace: true })
  }

  const openEdit = (item: DataSource) => {
    setEditing(item)
    setFormError('')
    setTestResult(null)
    setFormType(item.type)
    form.resetFields()
    form.setFieldsValue({
      name: item.name,
      type: item.type,
      jdbcUrl: item.jdbcUrl,
      username: item.username,
      password: '',
      properties: item.properties,
    })
    setModalOpen(true)
  }

  const closeModal = () => {
    setModalOpen(false)
    setFormError('')
    setTestResult(null)
    form.resetFields()
    clearAction()
  }

  const submit = async () => {
    let values: DataSourceInput
    try {
      values = await form.validate()
    } catch {
      return
    }

    setSaving(true)
    setFormError('')
    try {
      const input: DataSourceInput = {
        ...values,
        properties: editing ? editing.properties : {},
      }
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      closeModal()
    } catch (err) {
      if (err instanceof ApiError && err.code === 'duplicate_name') {
        setFormError('数据源名称已存在')
      } else if (err instanceof ApiError) {
        setFormError(err.message)
      } else {
        setFormError('保存失败，请稍后重试')
      }
    } finally {
      setSaving(false)
    }
  }

  const testUnsaved = async () => {
    let values: DataSourceInput
    try {
      values = await form.validate()
    } catch {
      return
    }

    setTesting(true)
    setTestResult(null)
    try {
      const body = editing
        ? { id: editing.id, ...values, properties: editing.properties }
        : { ...values, properties: {} }
      const result = await testDataSource(body)
      if (result.ok) {
        Message.success(`连接成功（${result.latencyMs} ms）`)
      } else {
        setTestResult(result)
      }
    } catch (err) {
      setTestResult(testFailure(err))
    } finally {
      setTesting(false)
    }
  }

  const testSaved = async (item: DataSource) => {
    setPageTestResult(null)
    try {
      const result = await testDataSource({ id: item.id })
      if (result.ok) {
        Message.success(`连接成功（${result.latencyMs} ms）`)
      } else {
        setPageTestResult(result)
      }
    } catch (err) {
      setPageTestResult(testFailure(err))
    }
  }

  const columns: TableColumnProps<DataSource>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 130,
      ellipsis: true,
      render: (value: string, item) => (
        <Link onClick={() => navigate(`/data-sources/${item.id}`)}>{value}</Link>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 110,
      render: (value: DataSourceType) => TYPE_LABELS[value] ?? value,
    },
    { title: '连接地址', dataIndex: 'jdbcUrl', ellipsis: true },
    {
      title: '版本号',
      dataIndex: 'version',
      width: 130,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      width: 195,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 180,
      render: (_, item) => (
        <Space size={2}>
          <Tooltip content="详情">
            <Button
              type="text"
              size="small"
              icon={<IconEye />}
              onClick={() => navigate(`/data-sources/${item.id}`)}
            />
          </Tooltip>
          <Tooltip content="测试连接">
            <Button
              type="text"
              size="small"
              icon={<IconSync />}
              onClick={() => testSaved(item)}
            />
          </Tooltip>
          <Tooltip content="编辑">
            <Button type="text" size="small" icon={<IconEdit />} onClick={() => openEdit(item)} />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该数据源？"
              okText="确定"
              cancelText="取消"
              onOk={async () => {
                setPageError('')
                try {
                  await deleteMutation.mutateAsync(item.id)
                } catch (err) {
                  setPageError(err instanceof ApiError ? err.message : '删除失败，请稍后重试')
                }
              }}
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
      <Typography.Title heading={6}>数据源</Typography.Title>

      <div className={listStyles['search-form-wrapper']}>
        <Form
          form={searchForm}
          className={listStyles['search-form']}
          labelAlign="left"
          labelCol={{ span: 6 }}
          wrapperCol={{ span: 18 }}
        >
          <Grid.Row gutter={24}>
            <Grid.Col span={8}>
              <Form.Item label="名称" field="name">
                <Input
                  allowClear
                  placeholder="请输入数据源名称"
                  onPressEnter={() => {
                    const values = searchForm.getFieldsValue()
                    setQuery((values.name ?? '').trim())
                  }}
                />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>
        </Form>
        <div className={listStyles['right-button']}>
          <Button
            type="primary"
            icon={<IconSearch />}
            onClick={() => setQuery((searchForm.getFieldValue('name') ?? '').trim())}
          >
            查询
          </Button>
          <Button
            icon={<IconRefresh />}
            onClick={() => {
              searchForm.resetFields()
              setQuery('')
            }}
          >
            重置
          </Button>
        </div>
      </div>

      <div className={listStyles['button-group']}>
        <Space>
          <Button type="primary" icon={<IconPlus />} onClick={openCreate}>
            新建数据源
          </Button>
        </Space>
      </div>

      {listQuery.isError ? (
        <Alert type="error" content="读取数据源失败，请稍后重试" style={{ marginBottom: 16 }} />
      ) : null}

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          style={{ marginBottom: 16 }}
          onClose={() => setPageError('')}
        />
      ) : null}

      {pageTestResult ? (
        <Alert
          type={pageTestResult.ok ? 'success' : 'error'}
          content={describeTestResult(pageTestResult)}
          closable
          style={{ marginBottom: 16 }}
          onClose={() => setPageTestResult(null)}
        />
      ) : null}

      <Table<DataSource>
        rowKey="id"
        tableLayoutFixed
        columns={columns}
        data={listQuery.data ?? []}
        loading={listQuery.isLoading}
        pagination={false}
        noDataElement={<Empty description="还没有数据源" />}
      />

      <Modal
        title={editing ? '编辑数据源' : '新建数据源'}
        visible={modalOpen}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        onOk={submit}
        onCancel={closeModal}
        unmountOnExit
        style={{ width: 680 }}
      >
        {formError ? <Alert type="error" content={formError} style={{ marginBottom: 16 }} /> : null}

        <Form form={form} layout="vertical" initialValues={{ type: 'mysql' }}>
          <Form.Item
            field="name"
            label="名称"
            rules={[{ required: true, message: '请输入名称' }]}
          >
            <Input placeholder="例如：prod-mysql" />
          </Form.Item>

          <Form.Item field="type" label="类型" rules={[{ required: true, message: '请选择类型' }]}>
            <Select
              options={DATA_SOURCE_TYPES.map(({ value, label }) => ({ value, label }))}
              onChange={(value) => setFormType(value as DataSourceType)}
            />
          </Form.Item>

          <Form.Item
            field="jdbcUrl"
            label="连接地址"
            rules={[{ required: true, message: '请输入连接地址' }]}
          >
            <Input placeholder={JDBC_PLACEHOLDERS[formType]} />
          </Form.Item>

          <Form.Item
            field="username"
            label="用户名"
            rules={formType === 'sqlite' ? [] : [{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="数据库用户名" />
          </Form.Item>

          <Form.Item field="password" label="密码" extra="留空表示不修改已保存的密码">
            <Input.Password placeholder="数据库密码" />
          </Form.Item>
        </Form>

        <Space style={{ marginTop: 4 }}>
          <Button icon={<IconSync />} loading={testing} onClick={testUnsaved}>
            测试连接
          </Button>
        </Space>
        {testResult ? (
          <Alert
            type={testResult.ok ? 'success' : 'error'}
            content={describeTestResult(testResult)}
            style={{ marginTop: 12 }}
          />
        ) : null}
      </Modal>
    </Card>
  )
}
