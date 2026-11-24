import {
  Alert,
  Button,
  Card,
  Empty,
  Skeleton,
  Space,
  Typography,
} from '@arco-design/web-react'
import { IconRefresh } from '@arco-design/web-react/icon'
import { useMemo, type ReactNode } from 'react'
import {
  Area,
  AreaChart,
  CartesianGrid,
  Legend,
  Line,
  LineChart,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useMonitor, type MonitorSample } from '../../api/monitor'
import { formatBytes, formatCount, formatSeconds, formatTime } from '../../utils/format'
import styles from './style/index.module.less'

const COLORS = {
  blue: '#165DFF',
  green: '#00B42A',
  orange: '#FF7D00',
  red: '#F53F3F',
  purple: '#722ED1',
  gray: '#86909C',
}

const API = {
  tick: { fill: '#86909C', fontSize: 12 },
} as const

const AXIS_PROPS = {
  tickLine: false,
  axisLine: false,
  tick: API.tick,
} as const

const CHART_MARGIN = { top: 8, right: 12, left: 0, bottom: 0 }

interface ChartPoint {
  label: string
  cpu: number
  goroutines: number
  peak: number
  running: number
  blocked: number
  waiting: number
  syscall: number
  other: number
  heapAlloc: number
  sys: number
}

function toChartPoint(sample: MonitorSample): ChartPoint {
  const date = new Date(sample.time)
  return {
    label: Number.isNaN(date.getTime())
      ? sample.time
      : date.toLocaleTimeString('zh-CN', { hour12: false }),
    cpu: sample.cpuPercent,
    goroutines: sample.goroutines,
    peak: sample.peakGoroutines,
    running: sample.states.running,
    blocked: sample.states.blocked,
    waiting: sample.states.waiting,
    syscall: sample.states.syscall,
    other: sample.states.other,
    heapAlloc: sample.heapAlloc,
    sys: sample.sys,
  }
}

/** 时间轴只显示到分钟，避免 15 分钟窗口内标签互相重叠。 */
function minuteLabel(value: string): string {
  return value.slice(0, 5)
}

/** 坐标轴上的紧凑字节单位，避免刻度文字过长。 */
function bytesTick(value: number): string {
  if (!Number.isFinite(value) || value < 0) return '-'
  const gb = 1024 ** 3
  const mb = 1024 ** 2
  const kb = 1024
  if (value >= gb) return `${(value / gb).toFixed(1)}G`
  if (value >= mb) return `${(value / mb).toFixed(0)}M`
  if (value >= kb) return `${(value / kb).toFixed(0)}K`
  return `${Math.round(value)}B`
}

function percent(part: number, total: number): string {
  if (!total) return '0.00%'
  return `${((part / total) * 100).toFixed(2)}%`
}

interface MetricCardProps {
  title: string
  stats: { label: string; value: ReactNode }[]
  children: ReactNode
}

function MetricCard({ title, stats, children }: MetricCardProps) {
  return (
    <Card className={styles.card}>
      <Typography.Title heading={6} style={{ margin: 0 }}>
        {title}
      </Typography.Title>
      <div className={styles.stats}>
        {stats.map((stat) => (
          <div key={stat.label} className={styles.stat}>
            <span className={styles['stat-label']}>{stat.label}：</span>
            <span className={styles['stat-value']}>{stat.value}</span>
          </div>
        ))}
      </div>
      <div className={styles.chart}>{children}</div>
    </Card>
  )
}

function ChartEmpty() {
  return (
    <div className={styles.empty}>
      <Empty description="正在采集数据，稍后自动刷新" />
    </div>
  )
}

export default function MonitorPage() {
  const monitor = useMonitor()
  const snapshot = monitor.data
  const samples = useMemo(() => snapshot?.samples ?? [], [snapshot])
  const current = snapshot?.current

  const chartData = useMemo(() => samples.map(toChartPoint), [samples])

  if (monitor.isLoading && !snapshot) {
    return (
      <Card>
        <Skeleton animation text={{ rows: 12 }} />
      </Card>
    )
  }

  if (!snapshot || !current) {
    return (
      <Alert
        type="error"
        content="读取服务监控数据失败，请稍后重试"
        action={
          <Button size="small" onClick={() => monitor.refetch()}>
            重试
          </Button>
        }
      />
    )
  }

  const hasChart = chartData.length >= 2
  const lastUpdated = samples.length > 0 ? formatTime(samples[samples.length - 1].time) : '-'

  return (
    <div className={styles.page}>
      <div className={styles.toolbar}>
        <Typography.Text type="secondary">
          每 {snapshot.intervalSeconds} 秒采样一次 · 保留最近{' '}
          {Math.max(1, Math.round(snapshot.retentionSeconds / 60))} 分钟 · {snapshot.goVersion} ·
          已运行 {formatSeconds(snapshot.uptimeSeconds)}
        </Typography.Text>
        <Space size={12}>
          <Typography.Text type="secondary">更新于 {lastUpdated}</Typography.Text>
          <Button
            size="small"
            icon={<IconRefresh />}
            loading={monitor.isFetching}
            onClick={() => monitor.refetch()}
          >
            刷新
          </Button>
        </Space>
      </div>

      <div className={styles.grid}>
        <MetricCard
          title="处理器监控"
          stats={[
            { label: '当前使用率', value: `${current.cpuPercent.toFixed(2)}%` },
            { label: '核心数', value: current.cores },
          ]}
        >
          {hasChart ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={CHART_MARGIN}>
                <defs>
                  <linearGradient id="monitorCpuArea" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={COLORS.blue} stopOpacity={0.28} />
                    <stop offset="95%" stopColor={COLORS.blue} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis dataKey="label" {...AXIS_PROPS} tickFormatter={minuteLabel} minTickGap={48} />
                <YAxis
                  {...AXIS_PROPS}
                  width={44}
                  domain={[0, 100]}
                  ticks={[0, 20, 40, 60, 80, 100]}
                  tickFormatter={(value: number) => `${value}%`}
                />
                <ChartTooltip
                  labelFormatter={(label) => `时间：${label}`}
                  formatter={(value) => [`${Number(value).toFixed(2)}%`, 'CPU 使用率']}
                />
                <Area
                  type="monotone"
                  dataKey="cpu"
                  name="CPU 使用率"
                  stroke={COLORS.blue}
                  strokeWidth={2}
                  fill="url(#monitorCpuArea)"
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <ChartEmpty />
          )}
        </MetricCard>

        <MetricCard
          title="协程"
          stats={[
            { label: '当前协程数', value: formatCount(current.goroutines) },
            { label: '峰值协程数', value: formatCount(current.peakGoroutines) },
            { label: 'GC 次数', value: formatCount(current.gcCount) },
          ]}
        >
          {hasChart ? (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={CHART_MARGIN}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis dataKey="label" {...AXIS_PROPS} tickFormatter={minuteLabel} minTickGap={48} />
                <YAxis {...AXIS_PROPS} width={44} allowDecimals={false} />
                <ChartTooltip
                  labelFormatter={(label) => `时间：${label}`}
                  formatter={(value, name) => [formatCount(Number(value)), String(name)]}
                />
                <Legend verticalAlign="top" height={28} iconType="circle" iconSize={8} />
                <Line
                  type="monotone"
                  dataKey="goroutines"
                  name="当前协程"
                  stroke={COLORS.blue}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="peak"
                  name="峰值协程"
                  stroke={COLORS.purple}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <ChartEmpty />
          )}
        </MetricCard>

        <MetricCard
          title="协程状态"
          stats={[
            { label: '运行中', value: formatCount(current.states.running) },
            { label: '阻塞中', value: formatCount(current.states.blocked) },
            { label: '等待中', value: formatCount(current.states.waiting) },
            { label: '系统调用', value: formatCount(current.states.syscall) },
            { label: '其他', value: formatCount(current.states.other) },
          ]}
        >
          {hasChart ? (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={CHART_MARGIN}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis dataKey="label" {...AXIS_PROPS} tickFormatter={minuteLabel} minTickGap={48} />
                <YAxis {...AXIS_PROPS} width={44} allowDecimals={false} />
                <ChartTooltip
                  labelFormatter={(label) => `时间：${label}`}
                  formatter={(value, name) => [formatCount(Number(value)), String(name)]}
                />
                <Legend verticalAlign="top" height={28} iconType="circle" iconSize={8} />
                <Line
                  type="monotone"
                  dataKey="running"
                  name="运行中"
                  stroke={COLORS.green}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="blocked"
                  name="阻塞中"
                  stroke={COLORS.red}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="waiting"
                  name="等待中"
                  stroke={COLORS.orange}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="syscall"
                  name="系统调用"
                  stroke={COLORS.purple}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="other"
                  name="其他"
                  stroke={COLORS.gray}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          ) : (
            <ChartEmpty />
          )}
        </MetricCard>

        <MetricCard
          title="内存"
          stats={[
            { label: '内存总量', value: formatBytes(current.sys) },
            { label: '内存已使用', value: formatBytes(current.heapAlloc) },
            { label: '内存使用率', value: percent(current.heapAlloc, current.sys) },
          ]}
        >
          {hasChart ? (
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={CHART_MARGIN}>
                <defs>
                  <linearGradient id="monitorHeapArea" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor={COLORS.blue} stopOpacity={0.28} />
                    <stop offset="95%" stopColor={COLORS.blue} stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis dataKey="label" {...AXIS_PROPS} tickFormatter={minuteLabel} minTickGap={48} />
                <YAxis {...AXIS_PROPS} width={56} tickFormatter={bytesTick} />
                <ChartTooltip
                  labelFormatter={(label) => `时间：${label}`}
                  formatter={(value, name) => [formatBytes(Number(value)), String(name)]}
                />
                <Legend verticalAlign="top" height={28} iconType="circle" iconSize={8} />
                <Area
                  type="monotone"
                  dataKey="sys"
                  name="内存总量"
                  stroke={COLORS.purple}
                  strokeWidth={2}
                  fill="none"
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
                <Area
                  type="monotone"
                  dataKey="heapAlloc"
                  name="已使用"
                  stroke={COLORS.blue}
                  strokeWidth={2}
                  fill="url(#monitorHeapArea)"
                  dot={false}
                  activeDot={{ r: 3 }}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          ) : (
            <ChartEmpty />
          )}
        </MetricCard>
      </div>
    </div>
  )
}
