import {
  Button,
  Card,
  DatePicker,
  Form,
  Grid,
  Select,
  Table,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import { IconRefresh, IconSearch } from '@arco-design/web-react/icon'
import type { Dayjs } from 'dayjs'
import { useState } from 'react'
import { useRuns, type Run, type RunFilters } from '../../api/runs'
import { useSyncJobs } from '../../api/syncJobs'
import RunStatusTag, { RunDetailLink } from '../../components/RunStatusTag'
import listStyles from '../../style/list.module.less'
import { formatDuration, formatTime } from '../../utils/format'

const STATUS_OPTIONS = [
  { value: 'running', label: '运行中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
]

interface RunSearchValues {
  status?: Run['status']
  jobId?: number
  range?: [Dayjs, Dayjs]
}

export default function RunsPage() {
  const [filters, setFilters] = useState<RunFilters>({ page: 1, pageSize: 20 })
  const [searchForm] = Form.useForm<RunSearchValues>()
  const runsQuery = useRuns(filters)
  const jobsQuery = useSyncJobs('')

  const jobOptions = (jobsQuery.data ?? []).map((job) => ({
    value: job.id,
    label: job.name,
  }))

  const handleSearch = () => {
    const values = searchForm.getFieldsValue()
    setFilters({
      page: 1,
      pageSize: filters.pageSize,
      status: values.status,
      jobId: values.jobId,
      from: values.range?.[0]?.startOf('day').toISOString(),
      to: values.range?.[1]?.endOf('day').toISOString(),
    })
  }

  const handleReset = () => {
    searchForm.resetFields()
    setFilters({ page: 1, pageSize: filters.pageSize })
  }

  const columns: TableColumnProps<Run>[] = [
    { title: '编号', dataIndex: 'id', width: 80 },
    { title: '任务', dataIndex: 'syncJobName', ellipsis: true },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: Run['status']) => <RunStatusTag status={status} />,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '耗时',
      width: 110,
      render: (_, run) => formatDuration(run.startedAt, run.finishedAt),
    },
    {
      title: '退出码',
      dataIndex: 'exitCode',
      width: 90,
      render: (value: number | null) => (value === null ? '-' : value),
    },
    {
      title: '操作',
      width: 110,
      render: (_, run) => <RunDetailLink runId={run.id} />,
    },
  ]

  return (
    <Card>
      <Typography.Title heading={6}>运行历史</Typography.Title>

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
              <Form.Item label="状态" field="status">
                <Select allowClear placeholder="全部状态" options={STATUS_OPTIONS} />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={8}>
              <Form.Item label="任务" field="jobId">
                <Select
                  allowClear
                  showSearch
                  placeholder="全部任务"
                  options={jobOptions}
                />
              </Form.Item>
            </Grid.Col>
            <Grid.Col span={8}>
              <Form.Item label="开始时间" field="range">
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
        </div>
      </div>

      <Table<Run>
        rowKey="id"
        tableLayoutFixed
        columns={columns}
        data={runsQuery.data?.items ?? []}
        loading={runsQuery.isLoading}
        pagination={{
          current: runsQuery.data?.page ?? 1,
          pageSize: runsQuery.data?.pageSize ?? 20,
          total: runsQuery.data?.total ?? 0,
          showTotal: true,
          sizeCanChange: true,
          onChange: (page, pageSize) => setFilters({ ...filters, page, pageSize }),
        }}
        noDataElement={<span>还没有运行记录</span>}
      />
    </Card>
  )
}
