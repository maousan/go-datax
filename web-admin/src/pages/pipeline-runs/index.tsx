import {
  Alert,
  Button,
  Card,
  Empty,
  Link,
  Select,
  Space,
  Table,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import { IconRefresh } from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import {
  usePipelineRuns,
  usePipelines,
  type PipelineRun,
} from '../../api/pipelines'
import RunStatusTag from '../../components/RunStatusTag'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatCount, formatDuration, formatTime } from '../../utils/format'
import { triggerLabel } from '../pipelines/helpers'

const STATUS_OPTIONS = [
  { value: 'running', label: '运行中' },
  { value: 'succeeded', label: '成功' },
  { value: 'failed', label: '失败' },
  { value: 'canceled', label: '已取消' },
]

export default function PipelineRunsPage() {
  const navigate = useNavigate()
  const [pipelineId, setPipelineId] = useState<number>()
  const [status, setStatus] = useState<string>()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [tableRef, tableHeight] = useFillHeight(42)
  const pipelinesQuery = usePipelines('')
  const runsQuery = usePipelineRuns({ pipelineId, status, page, pageSize })

  const columns: TableColumnProps<PipelineRun>[] = [
    { title: '编号', dataIndex: 'id', width: 80 },
    {
      title: 'Pipeline',
      dataIndex: 'pipelineName',
      ellipsis: true,
      render: (value: string, run) =>
        run.pipelineId ? (
          <Link onClick={() => navigate(`/pipelines/${run.pipelineId}`)}>{value}</Link>
        ) : (
          value
        ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: PipelineRun['status']) => <RunStatusTag status={value} />,
    },
    {
      title: '版本',
      dataIndex: 'versionNumber',
      width: 90,
      render: (value: number) => (value > 0 ? <Tag>v{value}</Tag> : '-'),
    },
    {
      title: '触发',
      dataIndex: 'trigger',
      width: 100,
      render: (value: string) => triggerLabel(value),
    },
    {
      title: '读取 / 写入',
      width: 140,
      render: (_, run) => `${formatCount(run.readRecords)} / ${formatCount(run.writeRecords)}`,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '耗时',
      width: 100,
      render: (_, run) => formatDuration(run.startedAt, run.finishedAt),
    },
    {
      title: '操作',
      width: 90,
      fixed: 'right',
      render: (_, run) => (
        <Button type="text" size="small" onClick={() => navigate(`/pipeline-runs/${run.id}`)}>
          详情
        </Button>
      ),
    },
  ]

  return (
    <Card className={listStyles.page}>
      <Typography.Title heading={6}>运行记录</Typography.Title>

      <div className={listStyles.toolbar}>
        <Space wrap>
          <Select
            allowClear
            showSearch
            style={{ width: 220 }}
            placeholder="全部 Pipeline"
            value={pipelineId}
            options={(pipelinesQuery.data ?? []).map((pipeline) => ({
              value: pipeline.id,
              label: pipeline.name,
            }))}
            onChange={(value) => {
              setPipelineId(value === undefined ? undefined : Number(value))
              setPage(1)
            }}
          />
          <Select
            allowClear
            style={{ width: 140 }}
            placeholder="全部状态"
            value={status}
            options={STATUS_OPTIONS}
            onChange={(value) => {
              setStatus(value as string | undefined)
              setPage(1)
            }}
          />
          <Button
            icon={<IconRefresh />}
            onClick={() => {
              void runsQuery.refetch()
              void pipelinesQuery.refetch()
            }}
          >
            刷新
          </Button>
        </Space>
      </div>

      {runsQuery.isError ? (
        <Alert
          type="error"
          content={runsQuery.error instanceof Error ? runsQuery.error.message : '读取运行记录失败'}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<PipelineRun>
          rowKey="id"
          tableLayoutFixed
          columns={columns}
          data={runsQuery.data?.items ?? []}
          loading={runsQuery.isLoading}
          scroll={{ x: 1080, y: tableHeight }}
          pagination={{
            current: runsQuery.data?.page ?? page,
            pageSize: runsQuery.data?.pageSize ?? pageSize,
            total: runsQuery.data?.total ?? 0,
            showTotal: true,
            sizeCanChange: true,
            onChange: (nextPage, nextPageSize) => {
              setPage(nextPage)
              setPageSize(nextPageSize)
            },
          }}
          noDataElement={<Empty description="没有匹配的运行记录" />}
        />
      </div>
    </Card>
  )
}
