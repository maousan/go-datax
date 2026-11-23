import {
  Alert,
  Button,
  Card,
  Descriptions,
  Divider,
  Grid,
  Link,
  Popconfirm,
  Space,
  Spin,
  Statistic,
  Table,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconCopy,
  IconDelete,
  IconEdit,
  IconExport,
  IconLeft,
  IconPlayArrow,
} from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { useDataSources } from '../../api/dataSources'
import { useRuns, type Run } from '../../api/runs'
import {
  exportSyncJob,
  useDeleteSyncJob,
  useDuplicateSyncJob,
  useStartRun,
  useSyncJob,
  useSyncJobStats,
  type AuthoringMode,
  type SyncJob,
} from '../../api/syncJobs'
import JsonEditor from '../../components/JsonEditor'
import RunStatusTag, { RunDetailLink } from '../../components/RunStatusTag'
import { downloadJSON, formatDuration, formatSeconds, formatTime } from '../../utils/format'

const MODE_LABELS: Record<AuthoringMode, string> = {
  form: '表单模式',
  raw: '原始 JSON',
}

const MODE_COLORS: Record<AuthoringMode, string> = {
  form: 'blue',
  raw: 'purple',
}

const TRIGGER_LABELS: Record<string, string> = {
  manual: '手动',
  rerun: '重跑',
  retry: '自动重试',
}

const RUNS_PAGE_SIZE = 10
const STATS_WINDOW_DAYS = 30

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

export default function SyncJobDetailPage() {
  const params = useParams()
  const jobId = Number(params.id)

  if (!Number.isFinite(jobId) || jobId <= 0) {
    return <Alert type="error" content="无效的任务编号" />
  }

  return <SyncJobDetailView key={jobId} jobId={jobId} />
}

function SyncJobDetailView({ jobId }: { jobId: number }) {
  const navigate = useNavigate()
  const jobQuery = useSyncJob(jobId)
  const statsQuery = useSyncJobStats(jobId, STATS_WINDOW_DAYS)
  const dataSourcesQuery = useDataSources('')
  const [page, setPage] = useState(1)
  const runsQuery = useRuns({ jobId, page, pageSize: RUNS_PAGE_SIZE })
  const startRunMutation = useStartRun()
  const duplicateMutation = useDuplicateSyncJob()
  const deleteMutation = useDeleteSyncJob()
  const [actionError, setActionError] = useState('')

  const job = jobQuery.data
  const stats = statsQuery.data
  const lastFailedRun = stats?.lastFailedRun
  const running = (stats?.running ?? 0) > 0 || job?.lastRun?.status === 'running'

  const dataSourceNames = new Map(
    (dataSourcesQuery.data ?? []).map((item) => [item.id, `${item.name}（${item.type}）`]),
  )
  const dataSourceName = (id: number) => dataSourceNames.get(id) ?? `#${id}`

  const handleRun = async () => {
    setActionError('')
    try {
      const run = await startRunMutation.mutateAsync(jobId)
      navigate(`/runs/${run.id}`)
    } catch (err) {
      setActionError(errorText(err, '启动运行失败'))
    }
  }

  const handleDuplicate = async () => {
    setActionError('')
    try {
      const copy = await duplicateMutation.mutateAsync(jobId)
      navigate(`/sync-jobs/${copy.id}`)
    } catch (err) {
      setActionError(errorText(err, '复制任务失败'))
    }
  }

  const handleExport = async () => {
    setActionError('')
    try {
      downloadJSON(`${job?.name ?? 'sync-job'}.json`, await exportSyncJob(jobId))
    } catch (err) {
      setActionError(errorText(err, '导出失败'))
    }
  }

  const handleDelete = async () => {
    setActionError('')
    try {
      await deleteMutation.mutateAsync(jobId)
      navigate('/sync-jobs')
    } catch (err) {
      setActionError(errorText(err, '删除任务失败'))
    }
  }

  const runColumns: TableColumnProps<Run>[] = [
    {
      title: '编号',
      dataIndex: 'id',
      width: 90,
      render: (id: number) => (
        <Link onClick={() => navigate(`/runs/${id}`)}>#{id}</Link>
      ),
    },
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
      title: '触发方式',
      dataIndex: 'trigger',
      width: 130,
      render: (value: string, run) => (
        <Tag>
          {TRIGGER_LABELS[value] ?? value}
          {run.retryAttempt > 0 ? ` #${run.retryAttempt}` : ''}
        </Tag>
      ),
    },
    {
      title: '退出码',
      dataIndex: 'exitCode',
      width: 90,
      render: (value: number | null) => (value === null ? '-' : value),
    },
    {
      title: '操作',
      width: 100,
      render: (_, run) => <RunDetailLink runId={run.id} />,
    },
  ]

  if (jobQuery.isError) {
    return <Alert type="error" content={errorText(jobQuery.error, '读取任务失败')} />
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={
          <Space>
            <Button type="text" icon={<IconLeft />} onClick={() => navigate('/sync-jobs')}>
              返回
            </Button>
            <Typography.Text>{job?.name ?? `任务 #${jobId}`}</Typography.Text>
            {job ? <Tag color={MODE_COLORS[job.authoringMode]}>{MODE_LABELS[job.authoringMode]}</Tag> : null}
          </Space>
        }
        extra={
          <Space>
            <Button
              type="primary"
              icon={<IconPlayArrow />}
              loading={startRunMutation.isPending}
              disabled={!job || running}
              onClick={handleRun}
            >
              {running ? '运行中' : '运行'}
            </Button>
            <Button
              icon={<IconEdit />}
              disabled={!job}
              onClick={() => navigate(`/sync-jobs?edit=${jobId}`)}
            >
              编辑
            </Button>
            <Button
              icon={<IconCopy />}
              loading={duplicateMutation.isPending}
              disabled={!job}
              onClick={handleDuplicate}
            >
              复制
            </Button>
            <Popconfirm
              content="导出文件包含数据库连接凭据，确认导出？"
              okText="确定"
              cancelText="取消"
              onOk={handleExport}
            >
              <Button icon={<IconExport />} disabled={!job}>
                导出
              </Button>
            </Popconfirm>
            <Popconfirm
              content="确认删除该任务？运行历史会保留。"
              okText="确定"
              cancelText="取消"
              onOk={handleDelete}
            >
              <Button
                status="danger"
                icon={<IconDelete />}
                loading={deleteMutation.isPending}
                disabled={!job}
              >
                删除
              </Button>
            </Popconfirm>
          </Space>
        }
      >
        {jobQuery.isLoading ? (
          <Spin />
        ) : job ? (
          <Descriptions
            column={4}
            data={[
              { label: '任务编号', value: `#${job.id}` },
              { label: '编写模式', value: MODE_LABELS[job.authoringMode] },
              { label: '创建时间', value: formatTime(job.createdAt) },
              { label: '更新时间', value: formatTime(job.updatedAt) },
              { label: '配置摘要', value: job.summary, span: 4 },
            ]}
          />
        ) : (
          <Alert type="error" content="任务不存在或已被删除" />
        )}
      </Card>

      {actionError ? <Alert type="error" content={actionError} closable /> : null}

      <Card
        title="运行统计"
        extra={
          <Typography.Text type="secondary">
            近 {stats?.windowDays ?? STATS_WINDOW_DAYS} 天
          </Typography.Text>
        }
      >
        <Grid.Row gutter={16}>
          <Grid.Col span={6}>
            <Statistic title="运行次数" value={stats ? stats.total : '-'} />
          </Grid.Col>
          <Grid.Col span={6}>
            <Statistic
              title="成功率"
              value={
                stats && stats.total > 0
                  ? `${(stats.successRate * 100).toFixed(1)}%`
                  : '-'
              }
            />
          </Grid.Col>
          <Grid.Col span={6}>
            <Statistic title="失败次数" value={stats ? stats.failed : '-'} />
          </Grid.Col>
          <Grid.Col span={6}>
            <Statistic
              title="平均耗时"
              value={stats ? formatSeconds(stats.avgDurationSeconds) : '-'}
            />
          </Grid.Col>
        </Grid.Row>

        {lastFailedRun ? (
          <Alert
            type="error"
            style={{ marginTop: 16 }}
            content={
              <Space>
                <span>
                  最近失败：{formatTime(lastFailedRun.startedAt)}
                  {lastFailedRun.errorSummary ? ` · ${lastFailedRun.errorSummary}` : ''}
                </span>
                <Link onClick={() => navigate(`/runs/${lastFailedRun.id}`)}>查看日志</Link>
              </Space>
            }
          />
        ) : null}
      </Card>

      <Card
        title="任务配置"
        extra={
          job ? (
            <Typography.Text type="secondary">
              {job.retryMax > 0
                ? `失败重试 ${job.retryMax} 次，间隔 ${job.retryIntervalSeconds}s`
                : '失败不自动重试'}
            </Typography.Text>
          ) : null
        }
      >
        {jobQuery.isLoading ? (
          <Spin />
        ) : job ? (
          <SyncJobDefinition job={job} dataSourceName={dataSourceName} />
        ) : null}
      </Card>

      <Card title="运行历史">
        <Table<Run>
          rowKey="id"
          tableLayoutFixed
          columns={runColumns}
          data={runsQuery.data?.items ?? []}
          loading={runsQuery.isLoading}
          pagination={{
            current: runsQuery.data?.page ?? page,
            pageSize: runsQuery.data?.pageSize ?? RUNS_PAGE_SIZE,
            total: runsQuery.data?.total ?? 0,
            showTotal: true,
            onChange: (nextPage) => setPage(nextPage),
          }}
          noDataElement={<span>还没有运行记录，点击右上角「运行」发起第一次同步</span>}
        />
      </Card>
    </Space>
  )
}

function SyncJobDefinition({
  job,
  dataSourceName,
}: {
  job: SyncJob
  dataSourceName: (id: number) => string
}) {
  if (job.authoringMode === 'raw') {
    return (
      <JsonEditor
        readOnly
        height="420px"
        value={JSON.stringify(job.raw ?? {}, null, 2)}
      />
    )
  }

  if (!job.form) {
    return <Alert type="warning" content="任务配置缺失，请编辑后重新保存" />
  }

  const { reader, writer, setting } = job.form

  return (
    <>
      <Grid.Row gutter={16}>
        <Grid.Col span={12}>
          <Card title="读取端">
            <Descriptions
              column={1}
              data={[
                { label: '数据源', value: dataSourceName(reader.dataSourceId) },
                { label: '表', value: reader.table || '-' },
                { label: '列', value: reader.column.join(', ') || '-' },
                { label: 'WHERE', value: reader.where || '-' },
                { label: '分片字段', value: reader.splitPk || '-' },
              ]}
            />
          </Card>
        </Grid.Col>
        <Grid.Col span={12}>
          <Card title="写入端">
            <Descriptions
              column={1}
              data={[
                { label: '数据源', value: dataSourceName(writer.dataSourceId) },
                { label: '表', value: writer.table || '-' },
                { label: '列', value: writer.column.join(', ') || '-' },
                { label: '写入模式', value: writer.writeMode || 'insert' },
              ]}
            />
          </Card>
        </Grid.Col>
      </Grid.Row>

      <Divider />

      <Descriptions
        column={3}
        data={[
          { label: '并发通道数', value: setting.channel },
          { label: '错误记录上限', value: setting.errorLimit.record },
          {
            label: '错误率上限',
            value: `${(setting.errorLimit.percentage * 100).toFixed(2)}%`,
          },
        ]}
      />
    </>
  )
}
