import {
  Card,
  Empty,
  Grid,
  Link,
  Radio,
  Skeleton,
  Space,
  Table,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconCheckCircle,
  IconCloseCircle,
  IconFile,
  IconHistory,
  IconPlayArrow,
  IconPlus,
  IconStorage,
  IconSync,
  IconUpload,
} from '@arco-design/web-react/icon'
import { useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  Cell,
  Legend,
  Pie,
  PieChart,
  ResponsiveContainer,
  Tooltip as ChartTooltip,
  XAxis,
  YAxis,
} from 'recharts'
import { useMe } from '../../api/auth'
import { useHealth } from '../../api/health'
import { useOverview, type Run } from '../../api/runs'
import { useAuth } from '../../auth/AuthContext'
import RunStatusTag from '../../components/RunStatusTag'
import { formatBytes, formatCount, formatDuration, formatTime } from '../../utils/format'
import styles from './style/index.module.less'

const { Row, Col } = Grid

const STATUS_META: Record<Run['status'], { label: string; color: string }> = {
  succeeded: { label: '成功', color: '#00B42A' },
  failed: { label: '失败', color: '#F53F3F' },
  running: { label: '运行中', color: '#165DFF' },
  canceled: { label: '已取消', color: '#86909C' },
}

interface StatisticItemProps {
  icon: ReactNode
  title: string
  count: ReactNode
  unit?: ReactNode
  loading?: boolean
}

function StatisticItem({ icon, title, count, unit, loading }: StatisticItemProps) {
  return (
    <div className={styles.item}>
      <div className={styles.icon}>{icon}</div>
      <div>
        <Skeleton loading={loading} text={{ rows: 2, width: 60 }} animation>
          <div className={styles.title}>{title}</div>
          <div className={styles.count}>
            {count}
            {unit ? <span className={styles.unit}>{unit}</span> : null}
          </div>
        </Skeleton>
      </div>
    </div>
  )
}

const SHORTCUTS = [
  { title: '新建数据源', path: '/data-sources?action=create', icon: <IconPlus /> },
  { title: '新建任务', path: '/sync-jobs?action=create', icon: <IconSync /> },
  { title: '导入任务', path: '/sync-jobs?action=import', icon: <IconUpload /> },
  { title: '运行历史', path: '/runs', icon: <IconHistory /> },
  { title: '数据源管理', path: '/data-sources', icon: <IconStorage /> },
  { title: '同步任务', path: '/sync-jobs', icon: <IconSync /> },
]

const DOC_LINKS = [
  { title: '快速开始', href: 'https://github.com/maousan/go-datax/blob/main/docs/QUICKSTART.md' },
  { title: '用户指南', href: 'https://github.com/maousan/go-datax/blob/main/docs/USER_GUIDE.md' },
  { title: '架构设计', href: 'https://github.com/maousan/go-datax/blob/main/docs/ARCHITECTURE.md' },
  { title: '问题反馈', href: 'https://github.com/maousan/go-datax/issues' },
]

const CHART_MODES = [
  { label: '运行次数', value: 'runs' },
  { label: '读取记录', value: 'records' },
]

const ICON_STYLE = { fontSize: 24 }

export default function OverviewPage() {
  const navigate = useNavigate()
  const overview = useOverview()
  const health = useHealth()
  const { token } = useAuth()
  const me = useMe(Boolean(token))
  const [chartMode, setChartMode] = useState<'runs' | 'records'>('runs')

  const data = overview.data
  const successRate = ((data?.successRate ?? 0) * 100).toFixed(1)
  const dailyStats = data?.dailyStats ?? []
  const failedJobs = data?.failedJobs ?? []
  const recentRuns = data?.recentRuns ?? []

  const statusData = useMemo(() => {
    if (!data) return []
    return (Object.keys(STATUS_META) as Run['status'][])
      .map((status) => ({
        type: STATUS_META[status].label,
        value: data.statusCounts?.[status] ?? 0,
        color: STATUS_META[status].color,
      }))
      .filter((item) => item.value > 0)
  }, [data])

  const runTotal = statusData.reduce((sum, item) => sum + item.value, 0)

  const healthLabel = health.isError
    ? { color: 'red', text: '服务未连接' }
    : health.isSuccess
      ? { color: 'green', text: '服务正常' }
      : { color: 'gray', text: '服务连接中' }

  const recentRunColumns: TableColumnProps<Run>[] = [
    {
      title: '编号',
      dataIndex: 'id',
      width: 80,
      render: (id: number) => <Link onClick={() => navigate(`/runs/${id}`)}>#{id}</Link>,
    },
    {
      title: '任务',
      dataIndex: 'syncJobName',
      ellipsis: true,
      render: (name: string, run) =>
        run.syncJobId ? (
          <Link onClick={() => navigate(`/sync-jobs/${run.syncJobId}`)}>{name}</Link>
        ) : (
          name
        ),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 100,
      render: (status: Run['status']) => <RunStatusTag status={status} />,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 170,
      render: (value: string) => formatTime(value),
    },
    {
      title: '耗时',
      width: 100,
      render: (_, run) => formatDuration(run.startedAt, run.finishedAt),
    },
  ]

  return (
    <div className={styles.wrapper}>
      <div className={styles.left}>
        <Card>
          <Typography.Title heading={5} style={{ marginTop: 0 }}>
            欢迎回来，{me.data?.username ?? 'admin'}
          </Typography.Title>

          <Row gutter={16} style={{ marginTop: 20, marginBottom: 16 }}>
            <Col span={8}>
              <StatisticItem
                icon={<IconStorage style={{ ...ICON_STYLE, color: 'rgb(var(--primary-6))' }} />}
                title="数据源"
                count={data?.dataSourceCount ?? 0}
                unit="个"
                loading={overview.isLoading}
              />
            </Col>
            <Col span={8}>
              <StatisticItem
                icon={<IconSync style={{ ...ICON_STYLE, color: 'rgb(var(--primary-6))' }} />}
                title="同步任务"
                count={data?.syncJobCount ?? 0}
                unit="个"
                loading={overview.isLoading}
              />
            </Col>
            <Col span={8}>
              <StatisticItem
                icon={<IconPlayArrow style={{ ...ICON_STYLE, color: STATUS_META.running.color }} />}
                title="运行中"
                count={data?.runningRuns ?? 0}
                unit="个"
                loading={overview.isLoading}
              />
            </Col>
          </Row>

          <Row gutter={16}>
            <Col span={8}>
              <StatisticItem
                icon={<IconHistory style={{ ...ICON_STYLE, color: 'rgb(var(--primary-6))' }} />}
                title={`近 ${data?.windowHours ?? 24} 小时运行`}
                count={data?.totalRuns ?? 0}
                unit="次"
                loading={overview.isLoading}
              />
            </Col>
            <Col span={8}>
              <StatisticItem
                icon={
                  <IconCheckCircle style={{ ...ICON_STYLE, color: STATUS_META.succeeded.color }} />
                }
                title="运行成功率"
                count={successRate}
                unit="%"
                loading={overview.isLoading}
              />
            </Col>
            <Col span={8}>
              <StatisticItem
                icon={<IconCloseCircle style={{ ...ICON_STYLE, color: STATUS_META.failed.color }} />}
                title="失败运行"
                count={data?.failedRuns ?? 0}
                unit="次"
                loading={overview.isLoading}
              />
            </Col>
          </Row>

          <div className={styles['section-header']} style={{ marginTop: 24 }}>
            <Typography.Paragraph className={styles['chart-title']} style={{ margin: 0 }}>
              运行趋势
              <span className={styles['chart-sub-title']}>（近 14 天）</span>
            </Typography.Paragraph>
            <Space size={12}>
              <Typography.Text type="secondary">
                近 {data?.windowHours ?? 24} 小时：{formatCount(data?.readRecords)} 条 ·{' '}
                {formatBytes(data?.readBytes)}
              </Typography.Text>
              <Radio.Group
                type="button"
                size="small"
                value={chartMode}
                onChange={setChartMode}
                options={CHART_MODES}
              />
            </Space>
          </div>

          <ResponsiveContainer width="100%" height={320}>
            {chartMode === 'runs' ? (
              <BarChart data={dailyStats} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis
                  dataKey="date"
                  tickLine={false}
                  axisLine={false}
                  tick={{ fill: '#86909C', fontSize: 12 }}
                  tickFormatter={(value: string) => value.slice(5)}
                />
                <YAxis
                  allowDecimals={false}
                  tickLine={false}
                  axisLine={false}
                  width={32}
                  tick={{ fill: '#86909C', fontSize: 12 }}
                />
                <ChartTooltip labelFormatter={(value) => `日期：${value}`} />
                <Legend />
                <Bar
                  dataKey="succeeded"
                  name="成功"
                  stackId="runs"
                  fill={STATUS_META.succeeded.color}
                />
                <Bar dataKey="failed" name="失败" stackId="runs" fill={STATUS_META.failed.color} />
                <Bar
                  dataKey="canceled"
                  name="已取消"
                  stackId="runs"
                  fill={STATUS_META.canceled.color}
                  radius={[4, 4, 0, 0]}
                />
              </BarChart>
            ) : (
              <AreaChart data={dailyStats} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
                <defs>
                  <linearGradient id="recordsArea" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#165DFF" stopOpacity={0.28} />
                    <stop offset="95%" stopColor="#165DFF" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#E5E6EB" />
                <XAxis
                  dataKey="date"
                  tickLine={false}
                  axisLine={false}
                  tick={{ fill: '#86909C', fontSize: 12 }}
                  tickFormatter={(value: string) => value.slice(5)}
                />
                <YAxis
                  allowDecimals={false}
                  tickLine={false}
                  axisLine={false}
                  width={70}
                  tick={{ fill: '#86909C', fontSize: 12 }}
                  tickFormatter={(value: number) => formatCount(value)}
                />
                <ChartTooltip
                  labelFormatter={(value) => `日期：${value}`}
                  formatter={(value) => [formatCount(Number(value)), '读取记录']}
                />
                <Area
                  type="monotone"
                  dataKey="readRecords"
                  name="读取记录"
                  stroke="#165DFF"
                  strokeWidth={2}
                  fill="url(#recordsArea)"
                />
              </AreaChart>
            )}
          </ResponsiveContainer>
        </Card>

        <Row gutter={16} className={styles['bottom-row']}>
          <Col span={14}>
            <Card>
              <div className={styles['section-header']}>
                <Typography.Title heading={6} style={{ margin: 0 }}>
                  最近运行
                </Typography.Title>
                <Link onClick={() => navigate('/runs')}>查看更多</Link>
              </div>
              <Table<Run>
                rowKey="id"
                tableLayoutFixed
                size="small"
                columns={recentRunColumns}
                data={recentRuns}
                loading={overview.isLoading}
                pagination={false}
                noDataElement={<span>还没有运行记录</span>}
              />
            </Card>
          </Col>
          <Col span={10}>
            <Card>
              <div className={styles['section-header']}>
                <Typography.Title heading={6} style={{ margin: 0 }}>
                  运行状态占比
                </Typography.Title>
                <Typography.Text type="secondary">
                  近 {data?.windowHours ?? 24} 小时
                </Typography.Text>
              </div>
              {statusData.length === 0 ? (
                <Empty description="暂无运行记录" />
              ) : (
                <div className={styles.donut}>
                  <ResponsiveContainer width="100%" height={280}>
                    <PieChart>
                      <Pie
                        data={statusData}
                        dataKey="value"
                        nameKey="type"
                        innerRadius={70}
                        outerRadius={98}
                        paddingAngle={2}
                        label={(props: { percent?: number }) =>
                          `${((props.percent ?? 0) * 100).toFixed(0)}%`
                        }
                        labelLine={false}
                      >
                        {statusData.map((item) => (
                          <Cell key={item.type} fill={item.color} />
                        ))}
                      </Pie>
                      <ChartTooltip />
                      <Legend verticalAlign="bottom" height={24} />
                    </PieChart>
                  </ResponsiveContainer>
                  <div className={styles['donut-center']}>
                    <span>运行总数</span>
                    <strong>{runTotal}</strong>
                  </div>
                </div>
              )}
            </Card>
          </Col>
        </Row>
      </div>

      <div className={styles.right}>
        <Card>
          <div className={styles['section-header']}>
            <Typography.Title heading={6} style={{ margin: 0 }}>
              快捷入口
            </Typography.Title>
          </div>
          <div className={styles.shortcuts}>
            {SHORTCUTS.map((shortcut) => (
              <div
                key={shortcut.title}
                className={styles.shortcut}
                onClick={() => navigate(shortcut.path)}
              >
                <div className={styles['shortcut-icon']}>{shortcut.icon}</div>
                <div className={styles['shortcut-title']}>{shortcut.title}</div>
              </div>
            ))}
          </div>
        </Card>

        <Card className={styles['right-card']}>
          <div className={styles['section-header']}>
            <Typography.Title heading={6} style={{ margin: 0 }}>
              失败 Top
            </Typography.Title>
            <Typography.Text type="secondary">近 7 天</Typography.Text>
          </div>
          {failedJobs.length === 0 ? (
            <Empty description="近 7 天没有失败" />
          ) : (
            failedJobs.map((job) => (
              <div key={job.jobId} className={styles['service-line']}>
                <Link
                  onClick={() => navigate(`/sync-jobs/${job.jobId}`)}
                  style={{
                    maxWidth: 160,
                    overflow: 'hidden',
                    whiteSpace: 'nowrap',
                    textOverflow: 'ellipsis',
                  }}
                >
                  {job.jobName}
                </Link>
                <Tag color="red">{job.failures} 次</Tag>
              </div>
            ))
          )}
        </Card>

        <Card className={styles['right-card']}>
          <Typography.Title heading={6} style={{ marginTop: 0 }}>
            系统信息
          </Typography.Title>
          <div className={styles['service-line']}>
            <span>服务状态</span>
            <Tag color={healthLabel.color as 'green' | 'red' | 'gray'}>{healthLabel.text}</Tag>
          </div>
          <div className={styles['service-line']}>
            <span>服务版本</span>
            <span>{health.data?.version ?? '-'}</span>
          </div>
          <div className={styles['service-line']}>
            <span>DataX 版本</span>
            <span>{health.data?.dataxVersion ?? '-'}</span>
          </div>
          <div className={styles['service-line']}>
            <span>当前账号</span>
            <span>{me.data?.username ?? '-'}</span>
          </div>
        </Card>

        <Card className={styles['right-card']}>
          <div className={styles['section-header']}>
            <Typography.Title heading={6} style={{ margin: 0 }}>
              文档中心
            </Typography.Title>
          </div>
          <div className={styles.docs}>
            {DOC_LINKS.map((doc) => (
              <Link key={doc.title} className={styles['docs-link']} href={doc.href} target="_blank">
                <IconFile style={{ marginRight: 4 }} />
                {doc.title}
              </Link>
            ))}
          </div>
        </Card>
      </div>
    </div>
  )
}
