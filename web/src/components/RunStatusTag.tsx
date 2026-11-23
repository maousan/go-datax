import { Tag } from 'antd'
import type { RunStatus } from '../api/runs'

const LABELS: Record<RunStatus, string> = {
  running: '运行中',
  succeeded: '成功',
  failed: '失败',
  canceled: '已取消',
}

const COLORS: Record<RunStatus, string> = {
  running: 'processing',
  succeeded: 'success',
  failed: 'error',
  canceled: 'default',
}

export default function RunStatusTag({ status }: { status: RunStatus }) {
  return <Tag color={COLORS[status]}>{LABELS[status] ?? status}</Tag>
}
