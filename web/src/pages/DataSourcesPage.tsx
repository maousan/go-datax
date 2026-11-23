import { PlusOutlined } from '@ant-design/icons'
import {
  Alert,
  Button,
  Card,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Table,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useState } from 'react'
import { ApiError } from '../api/client'
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
} from '../api/dataSources'

const DATA_SOURCE_TYPES: Array<{ value: DataSourceType; label: string; placeholder: string }> = [
  { value: 'mysql', label: 'MySQL', placeholder: 'jdbc:mysql://host:3306/db' },
  { value: 'postgres', label: 'PostgreSQL', placeholder: 'jdbc:postgresql://host:5432/db' },
  { value: 'sqlite', label: 'SQLite', placeholder: 'jdbc:sqlite:/path/to/database.db' },
  { value: 'mssql', label: 'SQL Server', placeholder: 'jdbc:sqlserver://host:1433;DatabaseName=db' },
  { value: 'oracle', label: 'Oracle', placeholder: 'jdbc:oracle:thin:@//host:1521/service' },
]

const TYPE_OPTIONS = DATA_SOURCE_TYPES.map(({ value, label }) => ({ value, label }))

const TYPE_LABELS = Object.fromEntries(
  DATA_SOURCE_TYPES.map(({ value, label }) => [value, label]),
) as Record<DataSourceType, string>

const JDBC_PLACEHOLDERS = Object.fromEntries(
  DATA_SOURCE_TYPES.map(({ value, placeholder }) => [value, placeholder]),
) as Record<DataSourceType, string>

function formatTime(value: string): string {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

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
  const [query, setQuery] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<DataSource | null>(null)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null)
  const [pageTestResult, setPageTestResult] = useState<ConnectionTestResult | null>(null)
  const [pageError, setPageError] = useState('')

  const [form] = Form.useForm<DataSourceInput>()
  const selectedType = Form.useWatch('type', form) ?? 'mysql'
  const listQuery = useDataSources(query)
  const createMutation = useCreateDataSource()
  const updateMutation = useUpdateDataSource()
  const deleteMutation = useDeleteDataSource()

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    setTestResult(null)
    form.resetFields()
    form.setFieldsValue({ type: 'mysql' })
    setModalOpen(true)
  }

  const openEdit = (item: DataSource) => {
    setEditing(item)
    setFormError('')
    setTestResult(null)
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
  }

  const submit = async () => {
    let values: DataSourceInput
    try {
      values = await form.validateFields()
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
      values = await form.validateFields()
    } catch {
      return
    }

    setTesting(true)
    setTestResult(null)
    try {
      const body = editing
        ? { id: editing.id, ...values, properties: editing.properties }
        : { ...values, properties: {} }
      setTestResult(await testDataSource(body))
    } catch (err) {
      setTestResult(testFailure(err))
    } finally {
      setTesting(false)
    }
  }

  const testSaved = async (item: DataSource) => {
    setPageTestResult(null)
    try {
      setPageTestResult(await testDataSource({ id: item.id }))
    } catch (err) {
      setPageTestResult(testFailure(err))
    }
  }

  const columns: ColumnsType<DataSource> = [
    { title: '名称', dataIndex: 'name', key: 'name', width: 180, ellipsis: true },
    {
      title: '类型',
      dataIndex: 'type',
      key: 'type',
      width: 120,
      render: (value: DataSourceType) => TYPE_LABELS[value] ?? value,
    },
    { title: '连接地址', dataIndex: 'jdbcUrl', key: 'jdbcUrl', ellipsis: true },
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
      width: 210,
      render: (_, item) => (
        <Space>
          <Button type="link" onClick={() => testSaved(item)}>
            测试
          </Button>
          <Button type="link" onClick={() => openEdit(item)}>
            编辑
          </Button>
          <Popconfirm
            title="确认删除该数据源？"
            okText="确定"
            cancelText="取消"
            onConfirm={async () => {
              setPageError('')
              try {
                await deleteMutation.mutateAsync(item.id)
              } catch (err) {
                setPageError(err instanceof ApiError ? err.message : '删除失败，请稍后重试')
              }
            }}
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
      title="数据源"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>
          新建数据源
        </Button>
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

      {listQuery.isError ? (
        <Alert type="error" message="读取数据源失败，请稍后重试" showIcon style={{ marginBottom: 16 }} />
      ) : null}

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

      {pageTestResult ? (
        <Alert
          closable
          showIcon
          type={pageTestResult.ok ? 'success' : 'error'}
          message={describeTestResult(pageTestResult)}
          style={{ marginBottom: 16 }}
          onClose={() => setPageTestResult(null)}
        />
      ) : null}

      <Table<DataSource>
        rowKey="id"
        tableLayout="fixed"
        columns={columns}
        dataSource={listQuery.data ?? []}
        loading={listQuery.isLoading}
        pagination={false}
        locale={{ emptyText: '还没有数据源' }}
      />

      <Modal
        title={editing ? '编辑数据源' : '新建数据源'}
        open={modalOpen}
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

        <Form form={form} layout="vertical" initialValues={{ type: 'mysql' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true, message: '请输入名称' }]}>
            <Input placeholder="例如：prod-mysql" />
          </Form.Item>

          <Form.Item name="type" label="类型" rules={[{ required: true, message: '请选择类型' }]}>
            <Select options={TYPE_OPTIONS} />
          </Form.Item>

          <Form.Item
            name="jdbcUrl"
            label="连接地址"
            rules={[{ required: true, message: '请输入连接地址' }]}
          >
            <Input placeholder={JDBC_PLACEHOLDERS[selectedType]} />
          </Form.Item>

          <Form.Item noStyle shouldUpdate={(prev, current) => prev.type !== current.type}>
            {({ getFieldValue }) => (
              <Form.Item
                name="username"
                label="用户名"
                rules={
                  getFieldValue('type') === 'sqlite'
                    ? []
                    : [{ required: true, message: '请输入用户名' }]
                }
              >
                <Input placeholder="数据库用户名" />
              </Form.Item>
            )}
          </Form.Item>

          <Form.Item name="password" label="密码" extra="留空表示不修改已保存的密码">
            <Input.Password placeholder="数据库密码" />
          </Form.Item>
        </Form>

        <Space direction="vertical" style={{ width: '100%' }}>
          <Button onClick={testUnsaved} loading={testing}>
            测试连接
          </Button>
          {testResult ? (
            <Alert
              showIcon
              type={testResult.ok ? 'success' : 'error'}
              message={describeTestResult(testResult)}
            />
          ) : null}
        </Space>
      </Modal>
    </Card>
  )
}
