import { ArrowLeftOutlined, DownloadOutlined, StopOutlined } from '@ant-design/icons'
import { Alert, Button, Card, Descriptions, Space, Spin, Typography } from 'antd'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useRef, useState } from 'react'
import { Link, useParams } from 'react-router'
import {
  downloadRunArtifact,
  streamRunLogs,
  useCancelRun,
  useRun,
} from '../api/runs'
import RunStatusTag from '../components/RunStatusTag'
import { formatDuration, formatTime } from '../utils/format'

export default function RunDetailPage() {
  const params = useParams()
  const runId = Number(params.id)

  if (!Number.isFinite(runId) || runId <= 0) {
    return <Alert type="error" message="无效的运行编号" showIcon />
  }

  return <RunDetailView key={runId} runId={runId} />
}

function RunDetailView({ runId }: { runId: number }) {
  const queryClient = useQueryClient()
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

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card
        title={
          <Space>
            <Link to="/runs">
              <ArrowLeftOutlined /> 返回
            </Link>
            <Typography.Text>运行 #{runId}</Typography.Text>
            {run ? <RunStatusTag status={run.status} /> : null}
          </Space>
        }
        extra={
          <Space>
            {run?.status === 'running' ? (
              <Button
                danger
                icon={<StopOutlined />}
                loading={cancelMutation.isPending}
                onClick={() => cancelMutation.mutate(runId)}
              >
                停止
              </Button>
            ) : null}
            <Button icon={<DownloadOutlined />} onClick={() => handleDownload('log')}>
              下载日志
            </Button>
            <Button icon={<DownloadOutlined />} onClick={() => handleDownload('snapshot')}>
              下载快照
            </Button>
          </Space>
        }
      >
        {runQuery.isLoading ? (
          <Spin />
        ) : run ? (
          <Descriptions column={3} size="small">
            <Descriptions.Item label="任务">{run.syncJobName}</Descriptions.Item>
            <Descriptions.Item label="开始时间">{formatTime(run.startedAt)}</Descriptions.Item>
            <Descriptions.Item label="结束时间">{formatTime(run.finishedAt)}</Descriptions.Item>
            <Descriptions.Item label="耗时">
              {formatDuration(run.startedAt, run.finishedAt)}
            </Descriptions.Item>
            <Descriptions.Item label="退出码">
              {run.exitCode === null ? '-' : run.exitCode}
            </Descriptions.Item>
            <Descriptions.Item label="触发方式">{run.trigger}</Descriptions.Item>
            <Descriptions.Item label="错误摘要" span={3}>
              {run.errorSummary || '-'}
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Alert type="error" message="读取运行记录失败" showIcon />
        )}
      </Card>

      {actionError ? <Alert type="error" message={actionError} showIcon closable /> : null}

      <Card title="运行日志">
        {streamError ? (
          <Alert type="warning" message={streamError} showIcon style={{ marginBottom: 12 }} />
        ) : null}
        <pre
          ref={logRef}
          onScroll={(event) => {
            const element = event.currentTarget
            const nearBottom =
              element.scrollHeight - element.scrollTop - element.clientHeight < 40
            setFollow(nearBottom)
          }}
          style={{
            background: '#0b1021',
            borderRadius: 6,
            color: '#d7e0ff',
            fontFamily: 'Consolas, "Courier New", monospace',
            fontSize: 12,
            height: 420,
            margin: 0,
            overflow: 'auto',
            padding: 12,
            whiteSpace: 'pre-wrap',
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
