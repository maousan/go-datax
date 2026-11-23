import {
  Card,
  Carousel,
  Divider,
  Empty,
  Grid,
  Link,
  Radio,
  Skeleton,
  Table,
  Tag,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconFile,
  IconHistory,
  IconPlus,
  IconStorage,
  IconSync,
  IconUpload,
} from '@arco-design/web-react/icon'
import dayjs from 'dayjs'
import { useMemo, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router'
import {
  Area,
  AreaChart,
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
import { useOverview, useRuns, type Run } from '../../api/runs'
import { useSyncJobs, type SyncJob } from '../../api/syncJobs'
import { useAuth } from '../../auth/AuthContext'
import carousel1 from '../../assets/dashboard/carousel-1.jpg'
import carousel2 from '../../assets/dashboard/carousel-2.jpg'
import carousel3 from '../../assets/dashboard/carousel-3.jpg'
import carousel4 from '../../assets/dashboard/carousel-4.jpg'
import calendarIcon from '../../assets/dashboard/calendar.svg'
import commentsIcon from '../../assets/dashboard/comments.svg'
import contentIcon from '../../assets/dashboard/content.svg'
import increaseIcon from '../../assets/dashboard/increase.svg'
import { formatTime } from '../../utils/format'
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

const RECENT_VISITS = [
  { title: '数据源', path: '/data-sources', icon: <IconFile /> },
  { title: '同步任务', path: '/sync-jobs', icon: <IconFile /> },
  { title: '运行历史', path: '/runs', icon: <IconFile /> },
]

const CAROUSEL_IMAGES = [carousel1, carousel2, carousel3, carousel4]

const ANNOUNCEMENTS = [
  { type: '活动', color: 'orangered', text: '数据同步管理台正式上线，支持多数据源统一接入' },
  { type: '消息', color: 'cyan', text: '新增 Oracle、SQL Server 数据源与连接测试能力' },
  { type: '通知', color: 'arcoblue', text: '运行日志支持实时回放与断线续传' },
  { type: '通知', color: 'arcoblue', text: '运行历史支持按任务、状态和时间范围筛选' },
]

const DOC_LINKS = [
  { title: '快速开始', href: 'https://github.com/maousan/go-datax/blob/main/docs/QUICKSTART.md' },
  { title: '用户指南', href: 'https://github.com/maousan/go-datax/blob/main/docs/USER_GUIDE.md' },
  { title: '架构设计', href: 'https://github.com/maousan/go-datax/blob/main/docs/ARCHITECTURE.md' },
  { title: '问题反馈', href: 'https://github.com/maousan/go-datax/issues' },
]

const jobColumns: TableColumnProps<SyncJob>[] = [
  { title: '任务名称', dataIndex: 'name', ellipsis: true },
  {
    title: '更新时间',
    dataIndex: 'updatedAt',
    width: 185,
    render: (value: string) => formatTime(value),
  },
]

export default function OverviewPage() {
  const navigate = useNavigate()
  const overview = useOverview()
  const runsQuery = useRuns({ page: 1, pageSize: 100 })
  const jobsQuery = useSyncJobs('')
  const health = useHealth()
  const { token } = useAuth()
  const me = useMe(Boolean(token))
  const [jobFilter, setJobFilter] = useState('all')

  const successRate = ((overview.data?.successRate ?? 0) * 100).toFixed(1)

  const trend = useMemo(() => {
    const items = runsQuery.data?.items ?? []
    return Array.from({ length: 14 }, (_, index) => {
      const date = dayjs().subtract(13 - index, 'day')
      return {
        date: date.format('MM-DD'),
        runs: items.filter((run) => dayjs(run.startedAt).isSame(date, 'day')).length,
      }
    })
  }, [runsQuery.data])

  const statusData = useMemo(() => {
    const items = runsQuery.data?.items ?? []
    return (Object.keys(STATUS_META) as Run['status'][])
      .map((status) => ({
        type: STATUS_META[status].label,
        value: items.filter((run) => run.status === status).length,
        color: STATUS_META[status].color,
      }))
      .filter((item) => item.value > 0)
  }, [runsQuery.data])

  const runTotal = statusData.reduce((sum, item) => sum + item.value, 0)

  const recentJobs = useMemo(() => {
    const items = jobsQuery.data ?? []
    const filtered =
      jobFilter === 'all' ? items : items.filter((job) => job.authoringMode === jobFilter)
    return filtered.slice(0, 5)
  }, [jobsQuery.data, jobFilter])

  const healthLabel = health.isError
    ? { color: 'red', text: '服务未连接' }
    : health.isSuccess
      ? { color: 'green', text: '服务正常' }
      : { color: 'gray', text: '服务连接中' }

  return (
    <div className={styles.wrapper}>
      <div className={styles.left}>
        <Card>
          <Typography.Title heading={5} style={{ marginTop: 0 }}>
            欢迎回来，{me.data?.username ?? 'admin'}
          </Typography.Title>
          <Divider />

          <Row>
            <Col flex={1}>
              <StatisticItem
                icon={<img alt="" src={calendarIcon} width={32} />}
                title="数据源"
                count={overview.data?.dataSourceCount ?? 0}
                unit="个"
                loading={overview.isLoading}
              />
            </Col>
            <Divider type="vertical" className={styles.divider} />
            <Col flex={1}>
              <StatisticItem
                icon={<img alt="" src={contentIcon} width={32} />}
                title="同步任务"
                count={overview.data?.syncJobCount ?? 0}
                unit="个"
                loading={overview.isLoading}
              />
            </Col>
            <Divider type="vertical" className={styles.divider} />
            <Col flex={1}>
              <StatisticItem
                icon={<img alt="" src={commentsIcon} width={32} />}
                title="近 24 小时运行"
                count={overview.data?.totalRuns ?? 0}
                unit="次"
                loading={overview.isLoading}
              />
            </Col>
            <Divider type="vertical" className={styles.divider} />
            <Col flex={1}>
              <StatisticItem
                icon={<img alt="" src={increaseIcon} width={32} />}
                title="运行成功率"
                count={successRate}
                unit="%"
                loading={overview.isLoading}
              />
            </Col>
          </Row>

          <Divider />

          <div className={styles['section-header']}>
            <Typography.Paragraph className={styles['chart-title']} style={{ margin: 0 }}>
              运行数据
              <span className={styles['chart-sub-title']}>（近 14 天）</span>
            </Typography.Paragraph>
            <Link onClick={() => navigate('/runs')}>查看更多</Link>
          </div>
          <ResponsiveContainer width="100%" height={340}>
            <AreaChart data={trend} margin={{ top: 10, right: 10, left: 0, bottom: 0 }}>
              <defs>
                <linearGradient id="runArea" x1="0" y1="0" x2="0" y2="1">
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
              />
              <YAxis
                allowDecimals={false}
                tickLine={false}
                axisLine={false}
                width={32}
                tick={{ fill: '#86909C', fontSize: 12 }}
              />
              <ChartTooltip />
              <Area
                type="monotone"
                dataKey="runs"
                name="运行次数"
                stroke="#165DFF"
                strokeWidth={2}
                fill="url(#runArea)"
              />
            </AreaChart>
          </ResponsiveContainer>
        </Card>

        <Row gutter={16} className={styles['bottom-row']}>
          <Col span={12}>
            <Card>
              <div className={styles['section-header']}>
                <Typography.Title heading={6} style={{ margin: 0 }}>
                  最近同步任务
                </Typography.Title>
                <Link onClick={() => navigate('/sync-jobs')}>查看更多</Link>
              </div>
              <Radio.Group
                type="button"
                value={jobFilter}
                onChange={setJobFilter}
                options={[
                  { label: '全部', value: 'all' },
                  { label: '表单模式', value: 'form' },
                  { label: '原始 JSON', value: 'raw' },
                ]}
                style={{ marginBottom: 16 }}
              />
              <Table<SyncJob>
                rowKey="id"
                tableLayoutFixed
                columns={jobColumns}
                data={recentJobs}
                loading={jobsQuery.isLoading}
                pagination={false}
                noDataElement={<span>还没有同步任务</span>}
              />
            </Card>
          </Col>

          <Col span={12}>
            <Card>
              <Typography.Title heading={6} style={{ marginTop: 0 }}>
                运行状态占比
              </Typography.Title>
              {statusData.length === 0 ? (
                <Empty description="暂无运行记录" />
              ) : (
                <div className={styles.donut}>
                  <ResponsiveContainer width="100%" height={320}>
                    <PieChart>
                      <Pie
                        data={statusData}
                        dataKey="value"
                        nameKey="type"
                        innerRadius={80}
                        outerRadius={110}
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
            <Link onClick={() => navigate('/sync-jobs')}>查看更多</Link>
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
          <Divider />
          <div className={styles['recent-title']}>最近访问</div>
          <div className={styles.shortcuts}>
            {RECENT_VISITS.map((item) => (
              <div
                key={item.title}
                className={styles.shortcut}
                onClick={() => navigate(item.path)}
              >
                <div className={styles['shortcut-icon']}>{item.icon}</div>
                <div className={styles['shortcut-title']}>{item.title}</div>
              </div>
            ))}
          </div>
        </Card>

        <Card className={styles['right-card']} bodyStyle={{ padding: 0 }}>
          <Carousel
            className={styles.carousel}
            indicatorType="slider"
            showArrow="never"
            autoPlay
          >
            {CAROUSEL_IMAGES.map((src) => (
              <div key={src} className={styles['carousel-slide']}>
                <img alt="" src={src} />
              </div>
            ))}
          </Carousel>
        </Card>

        <Card className={styles['right-card']}>
          <div className={styles['section-header']}>
            <Typography.Title heading={6} style={{ margin: 0 }}>
              公告
            </Typography.Title>
            <Link onClick={() => navigate('/runs')}>查看更多</Link>
          </div>
          {ANNOUNCEMENTS.map((item) => (
            <div key={item.text} className={styles['announcement-item']}>
              <Tag color={item.color} size="small">
                {item.type}
              </Tag>
              <span className={styles['announcement-text']}>{item.text}</span>
            </div>
          ))}
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
                {doc.title}
              </Link>
            ))}
          </div>
        </Card>

        <Card className={styles['right-card']}>
          <Typography.Title heading={6} style={{ marginTop: 0 }}>
            服务信息
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
            <span>当前账号</span>
            <span>{me.data?.username ?? '-'}</span>
          </div>
        </Card>
      </div>
    </div>
  )
}
