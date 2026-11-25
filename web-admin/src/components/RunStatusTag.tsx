import { Button, Tag } from '@arco-design/web-react'
import { IconEye } from '@arco-design/web-react/icon'
import { useNavigate } from 'react-router'
import type { RunStatus } from '../api/runs'

const LABELS: Record<RunStatus, string> = {
  queued: '排队中',
  running: '运行中',
  succeeded: '成功',
  failed: '失败',
  canceled: '已取消',
}

const COLORS: Record<RunStatus, string> = {
  queued: 'orange',
  running: 'blue',
  succeeded: 'green',
  failed: 'red',
  canceled: 'gray',
}

export default function RunStatusTag({ status }: { status: RunStatus }) {
  return <Tag color={COLORS[status]}>{LABELS[status] ?? status}</Tag>
}

export function RunDetailLink({ runId }: { runId: number }) {
  const navigate = useNavigate()
  return (
    <Button type="text" size="small" icon={<IconEye />} onClick={() => navigate(`/runs/${runId}`)}>
      详情
    </Button>
  )
}
