import {
  Alert,
  Button,
  Card,
  DatePicker,
  Form,
  Grid,
  Input,
  Link,
  Message,
  Popconfirm,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import { IconDelete, IconRefresh, IconSearch } from '@arco-design/web-react/icon'
import type { Dayjs } from 'dayjs'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import {
  useAlertLogs,
  useClearAlertLogs,
  type AlertChannelType,
  type AlertLog,
  type AlertLogEvent,
  type AlertLogFilters,
  type AlertLogStatus,
} from '../../api/alerts'
import { ApiError } from '../../api/client'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'

const EVENT_META: Record<AlertLogEvent, { label: string; color: string }> = {
  failed: { label: '失败', color: 'red' },
  recovered: { label: '恢复', color: 'green' },
  succeeded: { label: '成功', color: 'arcoblue' },
  canceled: { label: '取消', color: 'gray' },
  test: { label: '测试', color: 'purple' },
}

const TYPE_META: Record<AlertChannelType, { label: string; color: string }> = {
  email: { label: '邮箱', color: 'orange' },
  dingtalk: { label: '钉钉', color: 'arcoblue' },
  feishu: { label: '飞书', color: 'blue' },
  wecom: { label: '企业微信', color: 'cyan' },
  slack: { label: 'Slack', color: 'purple' },
  webhook: { label: '通用 Webhook', color: 'gray' },
}

const STATUS_META: Record<AlertLogStatus, { label: string; color: string }> = {
  success: { label: '成功', color: 'green' },
  failed: { label: '失败', color: 'red' },
}

const EVENT_OPTIONS = (Object.keys(EVENT_META) as AlertLogEvent[]).map((value) => ({
  value,
  label: EVENT_META[value].label,
}))

const TYPE_OPTIONS = (Object.keys(TYPE_META) as AlertChannelType[]).map((value) => ({
  value,
  label: TYPE_META[value].label,
}))

const STATUS_OPTIONS = (Object.keys(STATUS_META) as AlertLogStatus[]).map((value) => ({
  value,
  label: STATUS_META[value].label,
}))

interface AlertLogSearchValues {
  event?: AlertLogEvent
  status?: AlertLogStatus
  channelType?: AlertChannelType
  query?: string
  range?: [Dayjs, Dayjs]
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

export default function AlertLogsPage() {
  const navigate = useNavigate()
  const [filters, setFilters] = useState<AlertLogFilters>({ page: 1, pageSize: 20 })
  const [searchForm] = Form.useForm<AlertLogSearchValues>()
  const logsQuery = useAlertLogs(filters)
  const clearMutation = useClearAlertLogs()
  const [pageError, setPageError] = useState('')
  const [tableRef, tableHeight] = useFillHeight(140)

  const handleSearch = () => {
    const values = searchForm.getFieldsValue()
    setFilters({
      page: 1,
      pageSize: filters.pageSize,
      event: values.event,
      status: values.status,
      channelType: values.channelType,
      query: values.query?.trim() || undefined,
      from: values.range?.[0]?.startOf('day').toISOString(),
      to: values.range?.[1]?.endOf('day').toISOString(),
    })
  }

  const handleReset = () => {
    searchForm.resetFields()
    setFilters({ page: 1, pageSize: filters.pageSize })
  }

  const handleClear = async () => {
    setPageError('')
    try {
      const result = await clearMutation.mutateAsync()
      Message.success(`已清空 ${result.deleted} 条告警日志`)
    } catch (err) {
      setPageError(errorText(err, '清空告警日志失败'))
    }
  }

  const columns: TableColumnProps<AlertLog>[] = [
    {
      title: '发送时间',
      dataIndex: 'createdAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '事件',
      dataIndex: 'event',
      width: 90,
      render: (event: AlertLogEvent) => (
        <Tag color={EVENT_META[event]?.color}>{EVENT_META[event]?.label ?? event}</Tag>
      ),
    },
    {
      title: '任务',
      width: 220,
      ellipsis: true,
      render: (_, log) => {
        const name = log.syncJobName || log.collectTaskName
        if (!name) return '-'
        return log.runId > 0 ? (
          <Link onClick={() => navigate(`/runs/${log.runId}`)}>{name}</Link>
        ) : (
          name
        )
      },
    },
    {
      title: '渠道',
      width: 200,
      render: (_, log) => (
        <Space size={4}>
          <span>{log.channelName || '-'}</span>
          <Tag color={TYPE_META[log.channelType]?.color}>
            {TYPE_META[log.channelType]?.label ?? log.channelType}
          </Tag>
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 90,
      render: (status: AlertLogStatus) => (
        <Tag color={STATUS_META[status]?.color}>{STATUS_META[status]?.label ?? status}</Tag>
      ),
    },
    {
      title: '耗时',
      dataIndex: 'durationMs',
      width: 90,
      render: (value: number) => `${value}ms`,
    },
    {
      title: '错误',
      dataIndex: 'error',
      ellipsis: true,
      render: (value: string) =>
        value ? (
          <Tooltip content={value}>
            <span>{value}</span>
          </Tooltip>
        ) : (
          '-'
        ),
    },
  ]

  return (
    <Card className={listStyles.page}>
      <Typography.Title heading={6}>告警日志</Typography.Title>

      <div className={listStyles['search-form-wrapper']}>
        <Form
          form={searchForm}
          className={listStyles['search-form']}
          labelAlign="right"
          labelCol={{ span: 8 }}
          wrapperCol={{ span: 16 }}
        >
          <Grid.Row gutter={24}>
            <Grid.Col span={6}>
              <Form.Item label="事件" field="event">
                <Select allowClear placeholder="全部事件" options={EVENT_OPTIONS} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={6}>
              <Form.Item label="状态" field="status">
                <Select allowClear placeholder="全部状态" options={STATUS_OPTIONS} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={6}>
              <Form.Item label="渠道" field="channelType">
                <Select allowClear placeholder="全部渠道" options={TYPE_OPTIONS} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={6}>
              <Form.Item label="关键字" field="query">
                <Input
                  placeholder="渠道或任务名称"
                  allowClear
                  onPressEnter={handleSearch}
                />
              </Form.Item>
            </Grid.Col>
          </Grid.Row>
          <Grid.Row gutter={24}>
            <Grid.Col span={12}>
              <Form.Item label="发送时间" field="range">
                <DatePicker.RangePicker
                  allowClear
                  style={{ width: '100%' }}
                  placeholder={['开始日期', '结束日期']}
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
          <Popconfirm
            content="确认清空所有告警日志？此操作不可恢复。"
            okText="确定"
            cancelText="取消"
            onOk={handleClear}
          >
            <Button
              status="danger"
              icon={<IconDelete />}
              loading={clearMutation.isPending}
            >
              清空日志
            </Button>
          </Popconfirm>
        </div>
      </div>

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          onClose={() => setPageError('')}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<AlertLog>
          rowKey="id"
          tableLayoutFixed
          columns={columns}
          data={logsQuery.data?.items ?? []}
          loading={logsQuery.isLoading}
          scroll={{ y: tableHeight }}
          pagination={{
            current: logsQuery.data?.page ?? 1,
            pageSize: logsQuery.data?.pageSize ?? 20,
            total: logsQuery.data?.total ?? 0,
            showTotal: true,
            sizeCanChange: true,
            onChange: (page, pageSize) => setFilters({ ...filters, page, pageSize }),
          }}
          noDataElement={<span>还没有告警记录</span>}
        />
      </div>
    </Card>
  )
}
