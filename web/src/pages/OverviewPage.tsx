import { Card, Col, Row, Statistic, Table } from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { useOverview, type Run } from '../api/runs'
import RunDetailLink from '../components/RunDetailLink'
import RunStatusTag from '../components/RunStatusTag'
import { formatDuration, formatTime } from '../utils/format'

const columns: ColumnsType<Run> = [
  { title: '编号', dataIndex: 'id', key: 'id', width: 80 },
  { title: '任务', dataIndex: 'syncJobName', key: 'syncJobName' },
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
    title: '操作',
    key: 'actions',
    width: 110,
    render: (_, run) => <RunDetailLink runId={run.id} />,
  },
]

export default function OverviewPage() {
  const overview = useOverview()

  return (
    <Row gutter={[16, 16]}>
      <Col span={6}>
        <Card>
          <Statistic title="数据源" value={overview.data?.dataSourceCount ?? 0} />
        </Card>
      </Col>
      <Col span={6}>
        <Card>
          <Statistic title="同步任务" value={overview.data?.syncJobCount ?? 0} />
        </Card>
      </Col>
      <Col span={6}>
        <Card>
          <Statistic title="近 24 小时运行" value={overview.data?.totalRuns ?? 0} />
        </Card>
      </Col>
      <Col span={6}>
        <Card>
          <Statistic
            title="成功率"
            value={((overview.data?.successRate ?? 0) * 100).toFixed(1)}
            suffix="%"
          />
        </Card>
      </Col>

      <Col span={24}>
        <Card title="最近运行">
          <Table<Run>
            rowKey="id"
            columns={columns}
            dataSource={overview.data?.recentRuns ?? []}
            loading={overview.isLoading}
            pagination={false}
            locale={{ emptyText: '还没有运行记录' }}
          />
        </Card>
      </Col>
    </Row>
  )
}
