import {
  Button,
  Card,
  Form,
  Grid,
  Input,
  InputNumber,
  Message,
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
import { IconDelete, IconEdit, IconPlus, IconRefresh, IconSearch } from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../../api/client'
import { usePipelines, usePipelineVersions } from '../../api/pipelines'
import {
  useCreatePipelineSchedule,
  useDeletePipelineSchedule,
  usePipelineSchedules,
  useUpdatePipelineSchedule,
  type PipelineBlockingPolicy,
  type PipelineSchedule,
  type PipelineScheduleInput,
} from '../../api/schedules'
import RunStatusTag from '../../components/RunStatusTag'
import { formatTime } from '../../utils/format'

const BLOCKING_LABELS: Record<PipelineBlockingPolicy, string> = {
  discard: '丢弃',
  overwrite: '覆盖',
  queue: '排队',
}

const BLOCKING_COLORS: Record<PipelineBlockingPolicy, string> = {
  discard: 'gray',
  overwrite: 'orange',
  queue: 'arcoblue',
}

const DEFAULT_TIMEZONE =
  Intl.DateTimeFormat().resolvedOptions().timeZone || 'Asia/Shanghai'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

function scheduleInput(schedule: PipelineSchedule, enabled = schedule.enabled): PipelineScheduleInput {
  return {
    name: schedule.name,
    cronExpr: schedule.cronExpr,
    timezone: schedule.timezone,
    enabled,
    blockingPolicy: schedule.blockingPolicy,
    retryMax: schedule.retryMax,
    retryIntervalSeconds: schedule.retryIntervalSeconds,
    timeoutSeconds: schedule.timeoutSeconds,
    versionId: schedule.pinnedVersionId ?? 0,
  }
}

export function SchedulePanel({
  pipelineId,
  embedded = false,
}: {
  pipelineId?: number
  embedded?: boolean
}) {
  const navigate = useNavigate()
  const [query, setQuery] = useState('')
  const [modalOpen, setModalOpen] = useState(false)
  const [editing, setEditing] = useState<PipelineSchedule>()
  const [pageError, setPageError] = useState('')
  const schedulesQuery = usePipelineSchedules(pipelineId, query)
  const pipelinesQuery = usePipelines('')
  const updateMutation = useUpdatePipelineSchedule(pipelineId)
  const deleteMutation = useDeletePipelineSchedule(pipelineId)
  const pipelines = pipelinesQuery.data ?? []
  const pipelineNames = new Map(pipelines.map((pipeline) => [pipeline.id, pipeline.name]))

  const toggle = async (schedule: PipelineSchedule, enabled: boolean) => {
    setPageError('')
    try {
      await updateMutation.mutateAsync({ id: schedule.id, input: scheduleInput(schedule, enabled) })
      Message.success(enabled ? '调度已启用' : '调度已停用')
    } catch (err) {
      setPageError(errorMessage(err, '更新调度失败'))
    }
  }

  const remove = async (schedule: PipelineSchedule) => {
    setPageError('')
    try {
      await deleteMutation.mutateAsync(schedule.id)
      Message.success('调度已删除')
    } catch (err) {
      setPageError(errorMessage(err, '删除调度失败'))
    }
  }

  const columns: TableColumnProps<PipelineSchedule>[] = [
    { title: '名称', dataIndex: 'name', width: 160, ellipsis: true },
    ...(pipelineId
      ? []
      : [
          {
            title: 'Pipeline',
            dataIndex: 'pipelineName',
            width: 170,
            ellipsis: true,
            render: (value: string, schedule: PipelineSchedule) => (
              <Button
                type="text"
                size="small"
                style={{ padding: 0 }}
                onClick={() => navigate(`/pipelines/${schedule.pipelineId}`)}
              >
                {value}
              </Button>
            ),
          },
        ]),
    {
      title: 'Cron / 时区',
      width: 220,
      render: (_, schedule) => (
        <div>
          <Typography.Text style={{ fontFamily: 'Consolas, monospace', fontSize: 12 }}>
            {schedule.cronExpr}
          </Typography.Text>
          <div>
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              {schedule.timezone}
            </Typography.Text>
          </div>
        </div>
      ),
    },
    {
      title: '启用',
      dataIndex: 'enabled',
      width: 80,
      render: (enabled: boolean, schedule) => (
        <Switch checked={enabled} size="small" onChange={(value) => void toggle(schedule, value)} />
      ),
    },
    {
      title: '阻塞',
      dataIndex: 'blockingPolicy',
      width: 90,
      render: (policy: PipelineBlockingPolicy) => (
        <Tag color={BLOCKING_COLORS[policy]}>{BLOCKING_LABELS[policy] ?? policy}</Tag>
      ),
    },
    {
      title: '重试 / 超时',
      width: 130,
      render: (_, schedule) => (
        <Tooltip
          content={`重试间隔 ${schedule.retryIntervalSeconds} 秒；0 表示不重试/不超时`}
        >
          <span>
            {schedule.retryMax} 次
            {schedule.timeoutSeconds > 0 ? ` / ${schedule.timeoutSeconds}s` : ' / 不限时'}
          </span>
        </Tooltip>
      ),
    },
    {
      title: '版本',
      width: 90,
      render: (_, schedule) =>
        schedule.pinnedVersionId
          ? `v${schedule.pinnedVersionNumber}`
          : '最新',
    },
    {
      title: '下次触发',
      width: 175,
      render: (_, schedule) =>
        schedule.enabled ? formatTime(schedule.nextRunAt) : <Tag>已停用</Tag>,
    },
    {
      title: '最近运行',
      width: 130,
      render: (_, schedule) =>
        schedule.lastRun ? (
          <Button
            type="text"
            size="small"
            style={{ padding: 0 }}
            onClick={() => navigate(`/pipeline-runs/${schedule.lastRun?.id}`)}
          >
            <Space size={4}>
              <RunStatusTag status={schedule.lastRun.status} />
              #{schedule.lastRun.id}
            </Space>
          </Button>
        ) : (
          '-'
        ),
    },
    {
      title: '操作',
      width: 110,
      render: (_, schedule) => (
        <Space size={2}>
          <Tooltip content="编辑">
            <Button
              type="text"
              size="small"
              icon={<IconEdit />}
              aria-label={`编辑调度 ${schedule.name}`}
              onClick={() => {
                setEditing(schedule)
                setModalOpen(true)
              }}
            />
          </Tooltip>
          <Popconfirm title="删除这个调度？运行历史会保留。" onOk={() => void remove(schedule)}>
            <Tooltip content="删除">
              <Button
                type="text"
                size="small"
                status="danger"
                icon={<IconDelete />}
                aria-label={`删除调度 ${schedule.name}`}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ]

  const content = (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {pageError ? (
        <Typography.Text type="error" style={{ display: 'block' }}>
          {pageError}
        </Typography.Text>
      ) : null}
      <Space>
        {!pipelineId ? (
          <Input
            allowClear
            prefix={<IconSearch />}
            value={query}
            placeholder="搜索调度名称"
            style={{ width: 220 }}
            onChange={setQuery}
          />
        ) : null}
        <Button
          type="primary"
          icon={<IconPlus />}
          onClick={() => {
            setEditing(undefined)
            setModalOpen(true)
          }}
        >
          新建调度
        </Button>
        <Button icon={<IconRefresh />} onClick={() => void schedulesQuery.refetch()}>
          刷新
        </Button>
      </Space>
      <Table<PipelineSchedule>
        rowKey="id"
        tableLayoutFixed
        columns={columns}
        data={schedulesQuery.data ?? []}
        loading={schedulesQuery.isLoading}
        scroll={{ x: pipelineId ? 1180 : 1350 }}
        pagination={false}
        noDataElement={<div style={{ padding: 24 }}>还没有调度</div>}
      />
    </Space>
  )

  return (
    <>
      {embedded ? content : <Card title="Pipeline 调度">{content}</Card>}
      {modalOpen ? (
        <ScheduleModal
          pipelineId={pipelineId}
          schedule={editing}
          pipelines={pipelines.map((pipeline) => ({
            value: pipeline.id,
            label: pipeline.name,
          }))}
          pipelineNames={pipelineNames}
          onClose={() => setModalOpen(false)}
          onSaved={() => {
            setModalOpen(false)
            void schedulesQuery.refetch()
          }}
        />
      ) : null}
    </>
  )
}

function ScheduleModal({
  pipelineId,
  schedule,
  pipelines,
  pipelineNames,
  onClose,
  onSaved,
}: {
  pipelineId?: number
  schedule?: PipelineSchedule
  pipelines: { value: number; label: string }[]
  pipelineNames: Map<number, string>
  onClose: () => void
  onSaved: () => void
}) {
  const [form] = Form.useForm<PipelineScheduleInput>()
  const [selectedPipelineId, setSelectedPipelineId] = useState(
    schedule?.pipelineId ?? pipelineId ?? 0,
  )
  const [formError, setFormError] = useState('')
  const [saving, setSaving] = useState(false)
  const versionsQuery = usePipelineVersions(selectedPipelineId)
  const createMutation = useCreatePipelineSchedule(selectedPipelineId)
  const updateMutation = useUpdatePipelineSchedule(schedule?.pipelineId ?? pipelineId)
  const versions = versionsQuery.data ?? []
  const versionOptions = [
    { value: 0, label: '最新发布版本' },
    ...versions.map((version) => ({
      value: version.id,
      label: `v${version.versionNumber}`,
    })),
  ]

  const submit = async () => {
    let values: PipelineScheduleInput
    try {
      values = await form.validate()
    } catch {
      return
    }
    if (!schedule && !selectedPipelineId) {
      setFormError('请选择 Pipeline')
      return
    }
    setSaving(true)
    setFormError('')
    try {
      const input: PipelineScheduleInput = {
        ...values,
        timezone: values.timezone.trim(),
        versionId: values.versionId ?? 0,
      }
      if (schedule) {
        await updateMutation.mutateAsync({ id: schedule.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      Message.success(schedule ? '调度已保存' : '调度已创建')
      onSaved()
    } catch (err) {
      setFormError(errorMessage(err, '保存调度失败'))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Modal
      visible
      title={schedule ? '编辑调度' : '新建调度'}
      style={{ width: 'min(760px, calc(100vw - 48px))', top: 40 }}
      maskClosable={false}
      unmountOnExit
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={saving} onClick={() => void submit()}>
            保存
          </Button>
        </Space>
      }
    >
      {formError ? (
        <Typography.Text type="error" style={{ display: 'block', marginBottom: 12 }}>
          {formError}
        </Typography.Text>
      ) : null}
      <Form
        form={form}
        layout="vertical"
        initialValues={{
          name: schedule?.name ?? '',
          cronExpr: schedule?.cronExpr ?? '0 0/5 * * * ?',
          timezone: schedule?.timezone ?? DEFAULT_TIMEZONE,
          enabled: schedule?.enabled ?? true,
          blockingPolicy: schedule?.blockingPolicy ?? 'discard',
          retryMax: schedule?.retryMax ?? 0,
          retryIntervalSeconds: schedule?.retryIntervalSeconds ?? 0,
          timeoutSeconds: schedule?.timeoutSeconds ?? 0,
          versionId: schedule?.pinnedVersionId ?? 0,
        }}
      >
        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item
              field="name"
              label="名称"
              rules={[{ required: true, message: '请输入名称' }]}
            >
              <Input placeholder="例如：订单每小时同步" maxLength={100} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item label="Pipeline">
              <Select
                disabled={Boolean(schedule) || Boolean(pipelineId)}
                showSearch
                value={selectedPipelineId || undefined}
                options={
                  pipelineId
                    ? [{ value: pipelineId, label: pipelineNames.get(pipelineId) ?? `#${pipelineId}` }]
                    : pipelines
                }
                placeholder="选择 Pipeline"
                onChange={(value) => {
                  setSelectedPipelineId(Number(value))
                  form.setFieldValue('versionId', 0)
                }}
              />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        <Grid.Row gutter={16}>
          <Grid.Col span={16}>
            <Form.Item
              field="cronExpr"
              label="Cron 表达式"
              extra="Quartz 6/7 段（秒 分 时 日 月 周 [年]），例如 0 0/5 * * * ?"
              rules={[{ required: true, message: '请输入 cron 表达式' }]}
            >
              <Input placeholder="0 0/5 * * * ?" maxLength={100} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={8}>
            <Form.Item
              field="timezone"
              label="时区"
              rules={[{ required: true, message: '请输入时区' }]}
            >
              <Input placeholder="Asia/Shanghai" maxLength={64} />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        <Grid.Row gutter={16}>
          <Grid.Col span={8}>
            <Form.Item field="enabled" label="启用" triggerPropName="checked">
              <Switch />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={8}>
            <Form.Item field="blockingPolicy" label="阻塞策略">
              <Select
                options={[
                  { value: 'discard', label: '丢弃：上次未结束则跳过' },
                  { value: 'overwrite', label: '覆盖：取消上次后启动' },
                  { value: 'queue', label: '排队：上次结束后启动' },
                ]}
              />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={8}>
            <Form.Item field="versionId" label="执行版本">
              <Select options={versionOptions} placeholder="最新发布版本" />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

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
  )
}

export default function SchedulesPage() {
  return <SchedulePanel />
}
