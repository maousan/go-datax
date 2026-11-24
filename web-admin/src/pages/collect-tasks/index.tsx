import {
  Alert,
  Button,
  Card,
  Form,
  Grid,
  Input,
  InputNumber,
  Link,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconPlayArrow,
  IconPlus,
  IconRefresh,
  IconSearch,
} from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import {
  useCollectTasks,
  useCreateCollectTask,
  useDeleteCollectTask,
  useRunCollectTask,
  useUpdateCollectTask,
  type BlockingPolicy,
  type CollectTask,
  type CollectTaskInput,
} from '../../api/collectTasks'
import { useSyncJobs } from '../../api/syncJobs'
import RunStatusTag from '../../components/RunStatusTag'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'

const BLOCKING_LABELS: Record<BlockingPolicy, string> = {
  discard: '丢弃调度',
  overwrite: '覆盖调度',
}

const BLOCKING_COLORS: Record<BlockingPolicy, string> = {
  discard: 'gray',
  overwrite: 'orange',
}

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

export default function CollectTasksPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const query = searchParams.get('q') ?? ''
  const presetJobId = Number(searchParams.get('syncJobId') ?? 0)
  const [modalOpen, setModalOpen] = useState(searchParams.get('action') === 'create')
  const [editing, setEditing] = useState<CollectTask | null>(null)
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const [pageError, setPageError] = useState('')

  const [form] = Form.useForm<CollectTaskInput>()
  const [searchForm] = Form.useForm<{ name?: string }>()
  const tasksQuery = useCollectTasks({ query })
  const jobsQuery = useSyncJobs('')
  const createMutation = useCreateCollectTask()
  const updateMutation = useUpdateCollectTask()
  const deleteMutation = useDeleteCollectTask()
  const runMutation = useRunCollectTask()

  const jobOptions = (jobsQuery.data ?? []).map((job) => ({
    value: job.id,
    label: job.name,
  }))

  const handleSearch = () => {
    const name = (searchForm.getFieldValue('name') ?? '').trim()
    navigate(name ? `/collect-tasks?q=${encodeURIComponent(name)}` : '/collect-tasks')
  }

  const handleReset = () => {
    searchForm.resetFields()
    navigate('/collect-tasks')
  }

  const openCreate = () => {
    setEditing(null)
    setFormError('')
    form.resetFields()
    if (presetJobId > 0) {
      form.setFieldsValue({ syncJobId: presetJobId })
    }
    setModalOpen(true)
  }

  const openEdit = (task: CollectTask) => {
    setEditing(task)
    setFormError('')
    form.setFieldsValue({
      name: task.name,
      syncJobId: task.syncJobId,
      cronExpr: task.cronExpr,
      enabled: task.enabled,
      blockingPolicy: task.blockingPolicy,
      retryMax: task.retryMax,
      retryIntervalSeconds: task.retryIntervalSeconds,
      timeoutSeconds: task.timeoutSeconds,
    })
    setModalOpen(true)
  }

  const clearAction = () => {
    if (!searchParams.has('action') && !searchParams.has('syncJobId')) return
    const next = new URLSearchParams(searchParams)
    next.delete('action')
    next.delete('syncJobId')
    setSearchParams(next, { replace: true })
  }

  const closeModal = () => {
    setModalOpen(false)
    setFormError('')
    form.resetFields()
    clearAction()
  }

  const submit = async () => {
    let values: CollectTaskInput
    try {
      values = await form.validate()
    } catch {
      return
    }

    setSaving(true)
    setFormError('')
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, input: values })
      } else {
        await createMutation.mutateAsync(values)
      }
      closeModal()
    } catch (err) {
      setFormError(
        err instanceof ApiError && err.code === 'duplicate_name'
          ? '采集任务名称已存在'
          : errorMessage(err, '保存采集任务失败'),
      )
    } finally {
      setSaving(false)
    }
  }

  const handleRun = async (task: CollectTask) => {
    setPageError('')
    try {
      const run = await runMutation.mutateAsync(task.id)
      navigate(`/runs/${run.id}`)
    } catch (err) {
      setPageError(errorMessage(err, '启动采集任务失败'))
    }
  }

  const handleDelete = async (task: CollectTask) => {
    setPageError('')
    try {
      await deleteMutation.mutateAsync(task.id)
    } catch (err) {
      setPageError(errorMessage(err, '删除采集任务失败'))
    }
  }

  const columns: TableColumnProps<CollectTask>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 150,
      ellipsis: true,
    },
    {
      title: 'datax任务定义',
      dataIndex: 'syncJobName',
      width: 150,
      ellipsis: true,
      render: (value: string, task) => (
        <Link onClick={() => navigate(`/sync-jobs/${task.syncJobId}`)}>{value}</Link>
      ),
    },
    {
      title: 'Cron',
      dataIndex: 'cronExpr',
      width: 140,
      ellipsis: true,
      render: (value: string) => (
        <Typography.Text style={{ fontFamily: 'Consolas, monospace', fontSize: 12 }}>
          {value}
        </Typography.Text>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 80,
      render: (enabled: boolean) =>
        enabled ? <Tag color="green">启用</Tag> : <Tag>停用</Tag>,
    },
    {
      title: '阻塞策略',
      dataIndex: 'blockingPolicy',
      width: 100,
      render: (value: BlockingPolicy) => (
        <Tag color={BLOCKING_COLORS[value]}>{BLOCKING_LABELS[value]}</Tag>
      ),
    },
    {
      title: '重试 / 超时',
      width: 130,
      render: (_, task) => (
        <div>
          <div>
            {task.retryMax > 0 ? `${task.retryMax} 次 / ${task.retryIntervalSeconds}s` : '不重试'}
          </div>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            {task.timeoutSeconds > 0 ? `超时 ${task.timeoutSeconds}s` : '不超时'}
          </Typography.Text>
        </div>
      ),
    },
    {
      title: '下次运行',
      dataIndex: 'nextRunAt',
      width: 160,
      render: (value: string | null, task) =>
        task.enabled ? formatTime(value) : '-',
    },
    {
      title: '最近运行',
      width: 180,
      render: (_, task) =>
        task.lastRun ? (
          <Space size={4}>
            <RunStatusTag status={task.lastRun.status} />
            <Link onClick={() => navigate(`/runs/${task.lastRun?.id}`)}>
              {formatTime(task.lastRun.startedAt)}
            </Link>
          </Space>
        ) : (
          <Typography.Text type="secondary">未运行</Typography.Text>
        ),
    },
    {
      title: '操作',
      width: 150,
      fixed: 'right',
      render: (_, task) => (
        <Space size={2}>
          <Tooltip content="立即运行">
            <Button
              type="text"
              size="small"
              icon={<IconPlayArrow />}
              loading={runMutation.isPending}
              disabled={task.lastRun?.status === 'running'}
              onClick={() => handleRun(task)}
            />
          </Tooltip>
          <Tooltip content="编辑">
            <Button
              type="text"
              size="small"
              icon={<IconEdit />}
              onClick={() => openEdit(task)}
            />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该采集任务？运行历史会保留。"
              okText="确定"
              cancelText="取消"
              onOk={() => handleDelete(task)}
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
      <Typography.Title heading={6}>采集任务</Typography.Title>

      <div className={listStyles['search-form-wrapper']}>
        <Form
          key={query}
          form={searchForm}
          initialValues={{ name: query }}
          className={listStyles['search-form']}
          labelAlign="left"
          labelCol={{ span: 6 }}
          wrapperCol={{ span: 18 }}
        >
          <Grid.Row gutter={24}>
            <Grid.Col span={8}>
              <Form.Item label="名称" field="name">
                <Input
                  placeholder="按名称搜索"
                  allowClear
                  onPressEnter={handleSearch}
                />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>
        </Form>

        <div className={listStyles['right-button']}>
          <Button type="primary" icon={<IconSearch />} onClick={handleSearch}>
            查询
          </Button>
          <Button icon={<IconRefresh />} onClick={handleReset}>
            重置
          </Button>
          <Button type="primary" icon={<IconPlus />} onClick={openCreate}>
            新建采集任务
          </Button>
        </div>
      </div>

      {pageError ? <Alert type="error" content={pageError} closable onClose={() => setPageError('')} style={{ marginBottom: 16 }} /> : null}

      <Table<CollectTask>
        rowKey="id"
        tableLayoutFixed
        scroll={{ x: 1240 }}
        columns={columns}
        data={tasksQuery.data ?? []}
        loading={tasksQuery.isLoading}
        pagination={false}
        noDataElement={<span>还没有采集任务</span>}
      />

      <Modal
        title={editing ? '编辑采集任务' : '新建采集任务'}
        visible={modalOpen}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
        onOk={submit}
        onCancel={closeModal}
        unmountOnExit
        style={{ width: 680 }}
      >
        {formError ? (
          <Alert type="error" content={formError} style={{ marginBottom: 16 }} />
        ) : null}

        <Form
          form={form}
          layout="vertical"
          initialValues={{
            enabled: true,
            blockingPolicy: 'discard',
            retryMax: 0,
            retryIntervalSeconds: 60,
            timeoutSeconds: 0,
            syncJobId: presetJobId > 0 ? presetJobId : undefined,
          }}
        >
          <Grid.Row gutter={16}>
            <Grid.Col span={12}>
              <Form.Item
                field="name"
                label="名称"
                rules={[{ required: true, message: '请输入名称' }]}
              >
                <Input placeholder="例如：订单每 5 分钟采集" maxLength={100} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={12}>
              <Form.Item field="enabled" label="启用" triggerPropName="checked">
                <Switch />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>

          <Form.Item
            field="syncJobId"
            label="datax任务定义"
            rules={[{ required: true, message: '请选择 datax任务定义' }]}
          >
            <Select
              showSearch
              options={jobOptions}
              placeholder="选择一个 datax任务定义"
            />
          </Form.Item>

          <Form.Item
            field="cronExpr"
            label="Cron 表达式"
            extra="Quartz 6/7 段（秒 分 时 日 月 周 [年]），例如 0 0/5 * * * ?；暂不支持 L/W/#"
            rules={[{ required: true, message: '请输入 cron 表达式' }]}
          >
            <Input placeholder="0 0/5 * * * ?" maxLength={100} />
          </Form.Item>

          <Form.Item field="blockingPolicy" label="阻塞处理">
            <Select
              options={[
                { value: 'discard', label: '丢弃调度：上一次未结束则跳过本次' },
                { value: 'overwrite', label: '覆盖调度：取消上一次运行后重新启动' },
              ]}
            />
          </Form.Item>

          <Grid.Row gutter={16}>
            <Grid.Col span={8}>
              <Form.Item field="retryMax" label="失败重试次数" extra="0 表示不重试">
                <InputNumber min={0} max={10} style={{ width: '100%' }} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={8}>
              <Form.Item field="retryIntervalSeconds" label="重试间隔（秒）">
                <InputNumber min={0} max={86400} style={{ width: '100%' }} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={8}>
              <Form.Item field="timeoutSeconds" label="超时时间（秒）" extra="0 表示不超时">
                <InputNumber min={0} max={86400} style={{ width: '100%' }} />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>
        </Form>
      </Modal>
    </Card>
  )
}
