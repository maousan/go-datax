import {
  Alert,
  Button,
  Card,
  Descriptions,
  Grid,
  Link,
  Progress,
  Space,
  Spin,
  Statistic,
  Typography,
} from '@arco-design/web-react'
import { IconDownload, IconLeft, IconRefresh, IconStop } from '@arco-design/web-react/icon'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import {
  downloadRunArtifact,
  streamRunLogs,
  useCancelRun,
  useRerunRun,
  useRun,
  type RunProgress,
} from '../../api/runs'
import RunStatusTag from '../../components/RunStatusTag'
import { formatBytes, formatDuration, formatTime } from '../../utils/format'

const TRIGGER_LABELS: Record<string, string> = {
  manual: '手动',
  rerun: '重跑',
  retry: '自动重试',
}

export default function RunDetailPage() {
  const params = useParams()
  const runId = Number(params.id)

  if (!Number.isFinite(runId) || runId <= 0) {
    return <Alert type="error" content="无效的运行编号" />
  }

  return <RunDetailView key={runId} runId={runId} />
}

function RunDetailView({ runId }: { runId: number }) {
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const runQuery = useRun(runId)
  const cancelMutation = useCancelRun()
  const rerunMutation = useRerunRun()

  const [lines, setLines] = useState<string[]>([])
  const [liveProgress, setLiveProgress] = useState<RunProgress | null>(null)
  const [streamError, setStreamError] = useState('')
  const [actionError, setActionError] = useState('')
  const [follow, setFollow] = useState(true)
  const logRef = useRef<HTMLPreElement>(null)

  useEffect(() => {
    const controller = new AbortController()
    let offset = 0
    let ended = false

    const connect = async () => {
      while (!controller.signal.aborted && !ended) {
        try {
          await streamRunLogs(
            runId,
            offset,
            (event) => {
              if (event.type === 'log' && event.line !== undefined) {
                offset = event.seq ?? offset
                setLines((previous) => [...previous, event.line as string])
              }
              if (event.type === 'progress' && event.progress) {
                setLiveProgress(event.progress)
              }
              if (event.type === 'status') {
                void queryClient.invalidateQueries({ queryKey: ['run', runId] })
              }
              if (event.type === 'end') {
                ended = true
                void queryClient.invalidateQueries({ queryKey: ['run', runId] })
              }
            },
            controller.signal,
          )
          setStreamError('')
        } catch (err: unknown) {
          if (controller.signal.aborted) return
          setStreamError(err instanceof Error ? err.message : '日志连接中断，正在重连')
        }

        if (!ended && !controller.signal.aborted) {
          await new Promise((resolve) => setTimeout(resolve, 1000))
        }
      }
    }

    void connect()
    return () => controller.abort()
  }, [runId, queryClient])

  useEffect(() => {
    const element = logRef.current
    if (element && follow) {
      element.scrollTop = element.scrollHeight
    }
  }, [lines, follow])

  const run = runQuery.data
  const progress = liveProgress ?? run?.progress

  const handleDownload = async (kind: 'log' | 'snapshot') => {
    setActionError('')
    try {
      await downloadRunArtifact(runId, kind)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : '下载失败')
    }
  }

  const handleRerun = async () => {
    setActionError('')
    try {
      const next = await rerunMutation.mutateAsync(runId)
      navigate(`/runs/${next.id}`)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : '重跑失败')
    }
  }

  const details = run
    ? [
        {
          label: '任务',
          value: run.syncJobId ? (
            <Link onClick={() => navigate(`/sync-jobs/${run.syncJobId}`)}>
              {run.syncJobName}
            </Link>
          ) : (
            run.syncJobName
          ),
        },
        { label: '开始时间', value: formatTime(run.startedAt) },
        { label: '结束时间', value: formatTime(run.finishedAt) },
        { label: '耗时', value: formatDuration(run.startedAt, run.finishedAt) },
        { label: '退出码', value: run.exitCode === null ? '-' : String(run.exitCode) },
        { label: '触发方式', value: TRIGGER_LABELS[run.trigger] ?? run.trigger },
        ...(run.retryAttempt > 0
          ? [{ label: '重试次数', value: `第 ${run.retryAttempt} 次重试` }]
          : []),
        { label: '错误摘要', value: run.errorSummary || '-', span: 3 },
      ]
    : []

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={
          <Space>
            <Button type="text" icon={<IconLeft />} onClick={() => navigate('/runs')}>
              返回
            </Button>
            <Typography.Text>运行 #{runId}</Typography.Text>
            {run ? <RunStatusTag status={run.status} /> : null}
          </Space>
        }
        extra={
          <Space>
            {run?.status === 'running' ? (
              <Button
                type="primary"
                status="danger"
                icon={<IconStop />}
                loading={cancelMutation.isPending}
                onClick={() => cancelMutation.mutate(runId)}
              >
                停止
              </Button>
            ) : null}
            {run && run.status !== 'running' ? (
              <Button
                icon={<IconRefresh />}
                loading={rerunMutation.isPending}
                onClick={handleRerun}
              >
                重跑
              </Button>
            ) : null}
            <Button icon={<IconDownload />} onClick={() => handleDownload('log')}>
              下载日志
            </Button>
            <Button icon={<IconDownload />} onClick={() => handleDownload('snapshot')}>
              下载快照
            </Button>
          </Space>
        }
      >
        {runQuery.isLoading ? (
          <Spin />
        ) : run ? (
          <Descriptions column={3} data={details} />
        ) : (
          <Alert type="error" content="读取运行记录失败" />
        )}
      </Card>

      {actionError ? <Alert type="error" content={actionError} closable /> : null}

      {progress ? (
        <Card
          title="同步进度"
          extra={<Typography.Text type="secondary">数据来自引擎进度上报</Typography.Text>}
        >
          <Progress percent={Math.min(100, Math.max(0, progress.percent))} strokeWidth={10} />
          <Grid.Row gutter={16} style={{ marginTop: 16 }}>
            <Grid.Col span={6}>
              <Statistic title="已读记录" value={progress.readRecords} />
            </Grid.Col>
            <Grid.Col span={6}>
              <Statistic title="已读流量" value={formatBytes(progress.readBytes)} />
            </Grid.Col>
            <Grid.Col span={6}>
              <Statistic
                title="读取速度"
                value={`${formatBytes(progress.speedBytesPerSec)}/s · ${progress.speedRecordsPerSec} 条/s`}
              />
            </Grid.Col>
            <Grid.Col span={6}>
              <Statistic title="错误记录" value={progress.errorRecords} />
            </Grid.Col>
          </Grid.Row>
        </Card>
      ) : run?.status === 'running' ? (
        <Card title="同步进度">
          <Typography.Text type="secondary">
            等待引擎上报进度（默认每 30 秒一次）
          </Typography.Text>
        </Card>
      ) : null}

      <Card title="运行日志">
        {streamError ? (
          <Alert type="warning" content={streamError} style={{ marginBottom: 12 }} />
        ) : null}
        <pre
          ref={logRef}
          className="run-log"
          onScroll={(event) => {
            const element = event.currentTarget
            setFollow(element.scrollHeight - element.scrollTop - element.clientHeight < 40)
          }}
        >
          {lines.length > 0 ? lines.join('\n') : '等待日志输出...'}
        </pre>
        {!follow ? (
          <Button
            size="small"
            style={{ marginTop: 8 }}
            onClick={() => {
              setFollow(true)
              const element = logRef.current
              if (element) element.scrollTop = element.scrollHeight
            }}
          >
            继续跟随
          </Button>
        ) : null}
      </Card>
    </Space>
  )
}
