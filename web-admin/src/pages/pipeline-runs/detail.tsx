import {
  Alert,
  Button,
  Card,
  Descriptions,
  Space,
  Spin,
  Statistic,
  Typography,
} from '@arco-design/web-react'
import { IconDownload, IconLeft } from '@arco-design/web-react/icon'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import {
  downloadPipelineRunLog,
  streamPipelineRunEvents,
  usePipelineRun,
  type PipelineRunEvent,
} from '../../api/pipelines'
import RunStatusTag from '../../components/RunStatusTag'
import { formatCount, formatDuration, formatTime } from '../../utils/format'
import styles from './detail.module.less'

export default function PipelineRunDetailPage() {
  const params = useParams()
  const runId = Number(params.id)
  if (!Number.isFinite(runId) || runId <= 0) {
    return <Alert type="error" content="无效的运行编号" />
  }
  return <PipelineRunDetailView key={runId} runId={runId} />
}

function PipelineRunDetailView({ runId }: { runId: number }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const runQuery = usePipelineRun(runId)
  const [lines, setLines] = useState<string[]>([])
  const [live, setLive] = useState<PipelineRunEvent>()
  const [streamError, setStreamError] = useState('')
  const [actionError, setActionError] = useState('')
  const [follow, setFollow] = useState(true)
  const logRef = useRef<HTMLPreElement>(null)
  const run = runQuery.data

  useEffect(() => {
    const controller = new AbortController()
    let ended = false
    let offset = 0
    const connect = async () => {
      while (!controller.signal.aborted && !ended) {
        try {
          await streamPipelineRunEvents(
            runId,
            offset,
            (event) => {
              offset += 1
              if (event.type === 'log' && event.message) {
                setLines((previous) => [...previous, event.message as string])
              }
              if (event.type === 'progress' || event.type === 'done') {
                setLive(event)
              }
              if (event.type === 'done') {
                ended = true
                void queryClient.invalidateQueries({ queryKey: ['pipeline-run', runId] })
                void queryClient.invalidateQueries({ queryKey: ['pipeline-runs'] })
                void queryClient.invalidateQueries({ queryKey: ['pipeline'] })
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

  const handleDownload = async () => {
    setActionError('')
    try {
      await downloadPipelineRunLog(runId)
    } catch (err) {
      setActionError(err instanceof Error ? err.message : '下载日志失败')
    }
  }

  const status = live?.status ?? run?.status
  const readRecords = live?.readRecords ?? run?.readRecords ?? 0
  const writeRecords = live?.writeRecords ?? run?.writeRecords ?? 0
  const errorSummary = live?.error || run?.errorSummary || ''
  const details = run
    ? [
        {
          label: 'Pipeline',
          value: run.pipelineId ? (
            <Button
              type="text"
              size="small"
              style={{ padding: 0 }}
              onClick={() => navigate(`/pipelines/${run.pipelineId}`)}
            >
              {run.pipelineName}
            </Button>
          ) : (
            run.pipelineName
          ),
        },
        { label: '版本', value: run.versionNumber > 0 ? `v${run.versionNumber}` : '-' },
        { label: '触发方式', value: run.trigger === 'manual' ? '手动' : run.trigger },
        { label: '开始时间', value: formatTime(run.startedAt) },
        { label: '结束时间', value: formatTime(run.finishedAt) },
        { label: '耗时', value: formatDuration(run.startedAt, run.finishedAt) },
        { label: '错误摘要', value: errorSummary || '-', span: 3 },
      ]
    : []

  if (runQuery.isLoading) {
    return <Spin style={{ display: 'block', margin: '80px auto' }} />
  }
  if (runQuery.isError || !run) {
    return <Alert type="error" content="读取运行记录失败" />
  }

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={
          <Space>
            <Button
              type="text"
              icon={<IconLeft />}
              onClick={() =>
                navigate(run.pipelineId ? `/pipelines/${run.pipelineId}` : '/pipelines')
              }
            >
              返回
            </Button>
            <Typography.Text>Pipeline 运行 #{runId}</Typography.Text>
            {status ? <RunStatusTag status={status} /> : null}
          </Space>
        }
        extra={
          <Button icon={<IconDownload />} onClick={handleDownload}>
            下载日志
          </Button>
        }
      >
        <Descriptions column={3} data={details} />
      </Card>

      {streamError ? <Alert type="warning" content={streamError} /> : null}
      {actionError ? <Alert type="error" content={actionError} closable onClose={() => setActionError('')} /> : null}

      <Card title="读取 / 写入">
        <Space size={48}>
          <Statistic title="读取行数" value={formatCount(readRecords)} />
          <Statistic title="写入行数" value={formatCount(writeRecords)} />
        </Space>
        {errorSummary ? (
          <Alert type="error" content={errorSummary} style={{ marginTop: 12 }} />
        ) : null}
      </Card>

      <Card title="运行日志">
        <pre
          ref={logRef}
          className={styles.log}
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
