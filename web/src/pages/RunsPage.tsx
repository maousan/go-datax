import { Card, Input, Select, Space, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useState } from 'react'
import { Link } from 'react-router'
import { useRuns, type Run, type RunFilters } from '../api/runs'
import { useSyncJobs } from '../api/syncJobs'
import RunStatusTag from '../components/RunStatusTag'
import { formatDuration, formatTime } from '../utils/format'

const STATUS_OPTIONS = [
  { value: 'running', label: '运行中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
]

export default function RunsPage() {
  const [filters, setFilters] = useState<RunFilters>({ page: 1, pageSize: 20 })
  const [fromDate, setFromDate] = useState('')
  const [toDate, setToDate] = useState('')
  const runsQuery = useRuns(filters)
  const jobsQuery = useSyncJobs('')

  const jobOptions = (jobsQuery.data ?? []).map((job) => ({
    value: job.id,
    label: job.name,
  }))

  const columns: ColumnsType<Run> = [
    { title: '编号', dataIndex: 'id', key: 'id', width: 80 },
    { title: '任务', dataIndex: 'syncJobName', key: 'syncJobName', ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status',
      key: 'status',
      width: 110,
      render: (status: Run['status']) => <RunStatusTag status={status} />,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      key: 'startedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '耗时',
      key: 'duration',
      width: 110,
      render: (_, run) => formatDuration(run.startedAt, run.finishedAt),
    },
    {
      title: '退出码',
      dataIndex: 'exitCode',
      key: 'exitCode',
      width: 90,
      render: (value: number | null) => (value === null ? '-' : value),
    },
    {
      title: '操作',
      key: 'actions',
      width: 90,
      render: (_, run) => <Link to={`/runs/${run.id}`}>详情</Link>,
    },
  ]

  return (
    <Card title="运行历史">
      <Space style={{ marginBottom: 16 }} wrap>
        <Select
          allowClear
          placeholder="按状态筛选"
          style={{ width: 160 }}
          options={STATUS_OPTIONS}
          value={filters.status}
          onChange={(value) => setFilters({ ...filters, status: value, page: 1 })}
        />
        <Select
          allowClear
          showSearch
          optionFilterProp="label"
          placeholder="按任务筛选"
          style={{ width: 220 }}
          options={jobOptions}
          value={filters.jobId}
          onChange={(value) => setFilters({ ...filters, jobId: value, page: 1 })}
        />
        <Input
          type="date"
          aria-label="开始日期"
          value={fromDate}
          onChange={(event) => {
            const value = event.target.value
            setFromDate(value)
            setFilters({
              ...filters,
              from: value ? new Date(`${value}T00:00:00`).toISOString() : undefined,
              page: 1,
            })
          }}
        />
        <Input
          type="date"
          aria-label="结束日期"
          value={toDate}
          onChange={(event) => {
            const value = event.target.value
            setToDate(value)
            setFilters({
              ...filters,
              to: value ? new Date(`${value}T23:59:59`).toISOString() : undefined,
              page: 1,
            })
          }}
        />
      </Space>

      <Table<Run>
        rowKey="id"
        tableLayout="fixed"
        columns={columns}
        dataSource={runsQuery.data?.items ?? []}
        loading={runsQuery.isLoading}
        pagination={{
          current: runsQuery.data?.page ?? 1,
          pageSize: runsQuery.data?.pageSize ?? 20,
          total: runsQuery.data?.total ?? 0,
          showSizeChanger: true,
          onChange: (page, pageSize) => setFilters({ ...filters, page, pageSize }),
        }}
        locale={{ emptyText: '还没有运行记录' }}
      />
    </Card>
  )
}
