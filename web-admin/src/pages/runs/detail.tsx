import {
  Alert,
  Button,
  Card,
  Descriptions,
  Space,
  Spin,
  Typography,
} from '@arco-design/web-react'
import { IconDownload, IconLeft, IconStop } from '@arco-design/web-react/icon'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import {
  downloadRunArtifact,
  streamRunLogs,
  useCancelRun,
  useRun,
} from '../../api/runs'
import RunStatusTag from '../../components/RunStatusTag'
import { formatDuration, formatTime } from '../../utils/format'

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

  const [lines, setLines] = useState<string[]>([])
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

  const handleDownload = async (kind: 'log' | 'snapshot') => {
    setActionError('')
    try {
      await downloadRunArtifact(runId, kind)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : '下载失败')
    }
  }

  const details = run
    ? [
        { label: '任务', value: run.syncJobName },
        { label: '开始时间', value: formatTime(run.startedAt) },
        { label: '结束时间', value: formatTime(run.finishedAt) },
        { label: '耗时', value: formatDuration(run.startedAt, run.finishedAt) },
        { label: '退出码', value: run.exitCode === null ? '-' : String(run.exitCode) },
        { label: '触发方式', value: run.trigger },
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
