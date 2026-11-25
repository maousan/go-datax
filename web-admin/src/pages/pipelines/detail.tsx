import {
  Alert,
  Button,
  Card,
  Descriptions,
  Empty,
  Form,
  Input,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconEye,
  IconPlayArrow,
  IconSave,
  IconSend,
  IconUndo,
} from '@arco-design/web-react/icon'
import { useState } from 'react'
import { useNavigate, useParams } from 'react-router'
import { ApiError } from '../../api/client'
import { useEndpoints } from '../../api/endpoints'
import {
  usePipeline,
  usePipelineRuns,
  usePipelineVersions,
  usePublishPipeline,
  useRestorePipelineVersion,
  useRunPipeline,
  useRunPipelineVersion,
  useUpdatePipeline,
  type Pipeline,
  type PipelineRun,
  type PipelineVersion,
} from '../../api/pipelines'
import RunStatusTag from '../../components/RunStatusTag'
import { formatCount, formatDuration, formatTime } from '../../utils/format'
import styles from './detail.module.less'
import {
  endpointObjectText,
  pipelineChain,
  schemaText,
  schemasMatch,
  triggerLabel,
} from './helpers'
import TransformEditor from './transforms'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

export default function PipelineDetailPage() {
  const params = useParams()
  const pipelineId = Number(params.id)
  if (!Number.isFinite(pipelineId) || pipelineId <= 0) {
    return <Alert type="error" content="无效的 Pipeline 编号" />
  }
  return <PipelineDetailView pipelineId={pipelineId} />
}

function PipelineDetailView({ pipelineId }: { pipelineId: number }) {
  const navigate = useNavigate()
  const pipelineQuery = usePipeline(pipelineId)
  const runMutation = useRunPipeline(pipelineId)
  const [tab, setTab] = useState('overview')
  const [actionError, setActionError] = useState('')
  const pipeline = pipelineQuery.data

  if (pipelineQuery.isLoading) {
    return <Spin style={{ display: 'block', margin: '80px auto' }} />
  }
  if (pipelineQuery.isError || !pipeline) {
    return (
      <Alert
        type="error"
        content={errorMessage(pipelineQuery.error, '读取 Pipeline 失败')}
      />
    )
  }

  const runLatest = async () => {
    setActionError('')
    try {
      const run = await runMutation.mutateAsync()
      navigate(`/pipeline-runs/${run.id}`)
    } catch (err) {
      setActionError(errorMessage(err, '启动运行失败'))
    }
  }

  const heroDetails = [
    { label: '链路', value: pipelineChain(pipeline) },
    {
      label: '最新版本',
      value: pipeline.latestVersion ? `v${pipeline.latestVersion.versionNumber}` : '未发布',
    },
    {
      label: '最近运行',
      value: pipeline.lastRun ? (
        <Space size={6}>
          <RunStatusTag status={pipeline.lastRun.status} />
          <Typography.Text type="secondary">#{pipeline.lastRun.id}</Typography.Text>
        </Space>
      ) : (
        '尚未运行'
      ),
    },
    { label: '更新时间', value: formatTime(pipeline.updatedAt) },
  ]

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <div className={styles.hero}>
        <div className={styles['hero-main']}>
          <div>
            <Typography.Title heading={5} style={{ margin: 0 }}>
              {pipeline.name}
            </Typography.Title>
            <Typography.Text type="secondary">
              {pipeline.description || '未填写说明'}
            </Typography.Text>
          </div>
          <Space>
            {pipeline.latestVersion ? <Tag color="green">已发布</Tag> : <Tag>草稿</Tag>}
            <Button
              type="primary"
              icon={<IconPlayArrow />}
              loading={runMutation.isPending}
              disabled={!pipeline.latestVersion}
              onClick={runLatest}
            >
              运行最新版本
            </Button>
          </Space>
        </div>
        <div className={styles['hero-meta']}>
          {heroDetails.map((item) => (
            <span key={item.label}>
              <span className={styles['meta-label']}>{item.label}</span>
              {item.value}
            </span>
          ))}
        </div>
      </div>

      {actionError ? (
        <Alert type="error" content={actionError} closable onClose={() => setActionError('')} />
      ) : null}

      <Card>
        <Tabs activeTab={tab} onChange={setTab} className={styles.tabs}>
          <Tabs.TabPane key="overview" title="概览">
            <PipelineOverview pipeline={pipeline} />
          </Tabs.TabPane>
          <Tabs.TabPane key="draft" title="草稿">
            <DraftEditor
              key={`${pipeline.id}-${pipeline.updatedAt}`}
              pipeline={pipeline}
              onPublished={() => setTab('versions')}
            />
          </Tabs.TabPane>
          <Tabs.TabPane key="versions" title="版本">
            <VersionsTab pipelineId={pipeline.id} onRestored={() => setTab('draft')} />
          </Tabs.TabPane>
          <Tabs.TabPane key="schedules" title="调度">
            <Empty description="调度将在下一票接入；当前可手动运行最新发布版本。" />
          </Tabs.TabPane>
          <Tabs.TabPane key="runs" title="运行">
            <RunsTab pipelineId={pipeline.id} />
          </Tabs.TabPane>
        </Tabs>
      </Card>
    </Space>
  )
}

function PipelineOverview({ pipeline }: { pipeline: Pipeline }) {
  const source = pipeline.source
  const sink = pipeline.sink
  const mismatch = Boolean(
    source && sink && !schemasMatch(source.schema, sink.schema),
  )
  const details = [
    {
      label: '源',
      value: source ? (
        <Space direction="vertical" size={0}>
          <span>{source.name}</span>
          <Typography.Text type="secondary">
            {source.connector} · {source.dataSourceName} · {endpointObjectText(source)}
          </Typography.Text>
        </Space>
      ) : (
        '-'
      ),
    },
    {
      label: '目标',
      value: sink ? (
        <Space direction="vertical" size={0}>
          <span>{sink.name}</span>
          <Typography.Text type="secondary">
            {sink.connector} · {sink.dataSourceName} · {endpointObjectText(sink)}
          </Typography.Text>
        </Space>
      ) : (
        '-'
      ),
    },
    {
      label: '最新版本',
      value: pipeline.latestVersion
        ? `v${pipeline.latestVersion.versionNumber} · ${pipeline.latestVersion.summary}`
        : '未发布',
    },
    {
      label: '最近运行',
      value: pipeline.lastRun ? (
        <Space size={6}>
          <RunStatusTag status={pipeline.lastRun.status} />
          <span>#{pipeline.lastRun.id}</span>
          <span>{formatTime(pipeline.lastRun.startedAt)}</span>
        </Space>
      ) : (
        '尚未运行'
      ),
    },
    {
      label: '源字段',
      value: source ? schemaText(source.schema) : '-',
      span: 2,
    },
    {
      label: '目标字段',
      value: sink ? schemaText(sink.schema) : '-',
      span: 2,
    },
    {
      label: '创建时间',
      value: formatTime(pipeline.createdAt),
    },
    {
      label: '更新时间',
      value: formatTime(pipeline.updatedAt),
    },
  ]
  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {mismatch ? (
        <Alert
          type="warning"
          content="当前草稿的源与目标字段不一致；可以保存草稿，但发布会被拒绝。"
        />
      ) : null}
      <Descriptions column={2} data={details} />
    </Space>
  )
}

function DraftEditor({
  pipeline,
  onPublished,
}: {
  pipeline: Pipeline
  onPublished: () => void
}) {
  const sourcesQuery = useEndpoints('source', '')
  const sinksQuery = useEndpoints('sink', '')
  const updateMutation = useUpdatePipeline(pipeline.id)
  const publishMutation = usePublishPipeline(pipeline.id)
  const [name, setName] = useState(pipeline.name)
  const [description, setDescription] = useState(pipeline.description)
  const [sourceId, setSourceId] = useState<number>(pipeline.sourceId)
  const [sinkId, setSinkId] = useState<number>(pipeline.sinkId)
  const [transforms, setTransforms] = useState(pipeline.transforms ?? [])
  const [error, setError] = useState('')
  const sources = sourcesQuery.data ?? []
  const sinks = sinksQuery.data ?? []
  const selectedSource = sources.find((item) => item.id === sourceId)
  const selectedSink = sinks.find((item) => item.id === sinkId)
  const restoredFrom = pipeline.draftVersionNumber > 0
  const source =
    restoredFrom && sourceId === pipeline.sourceId
      ? pipeline.source ?? selectedSource
      : selectedSource
  const sink =
    restoredFrom && sinkId === pipeline.sinkId
      ? pipeline.sink ?? selectedSink
      : selectedSink
  const directMismatch = Boolean(
    source && sink && !schemasMatch(source.schema, sink.schema),
  )
  const hasTransforms = transforms.length > 0
  const dirty =
    name.trim() !== pipeline.name ||
    description.trim() !== pipeline.description ||
    sourceId !== pipeline.sourceId ||
    sinkId !== pipeline.sinkId ||
    JSON.stringify(transforms) !== JSON.stringify(pipeline.transforms ?? [])

  const saveDraft = async () => {
    if (!name.trim()) {
      setError('请输入名称')
      return
    }
    if (!sourceId || !sinkId) {
      setError('请选择源和目标')
      return
    }
    setError('')
    await updateMutation.mutateAsync({
      name: name.trim(),
      description: description.trim(),
      sourceId,
      sinkId,
      transforms,
    })
    Message.success('草稿已保存')
  }

  const publish = async () => {
    if (!source || !sink) {
      setError('请选择源和目标')
      return
    }
    if (!schemasMatch(source.schema, sink.schema) && !hasTransforms) {
      setError('源与目标的字段不一致，请添加 Transform 或调整端点字段')
      return
    }
    setError('')
    try {
      if (dirty) {
        await updateMutation.mutateAsync({
          name: name.trim(),
          description: description.trim(),
          sourceId,
          sinkId,
          transforms,
        })
      }
      await publishMutation.mutateAsync()
      Message.success('已发布新版本')
      onPublished()
    } catch (err) {
      setError(errorMessage(err, '发布失败'))
    }
  }

  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {error ? <Alert type="error" content={error} closable onClose={() => setError('')} /> : null}
      {restoredFrom ? (
        <Alert
          type="info"
          content={`草稿由 v${pipeline.draftVersionNumber} 快照重建；直接发布会使用快照中的源/目标定义。修改源或目标后保存会清除快照。`}
        />
      ) : null}
      {!hasTransforms && directMismatch ? (
        <Alert
          type="warning"
          content="源与目标的字段不一致；添加 Transform 或调整端点字段后再发布。"
        />
      ) : null}
      <Form layout="vertical" className={styles.form}>
        <Form.Item label="名称" required>
          <Input value={name} maxLength={100} onChange={setName} />
        </Form.Item>
        <Form.Item label="说明">
          <Input.TextArea
            value={description}
            maxLength={500}
            rows={2}
            onChange={setDescription}
          />
        </Form.Item>
        <Form.Item label="源" required extra="发布时会快照源的数据源、字段和选项。">
          <Select
            showSearch
            value={sourceId}
            options={sources.map((item) => ({
              value: item.id,
              label: `${item.name}（${item.connector}）`,
            }))}
            onChange={(value) => setSourceId(Number(value))}
          />
        </Form.Item>
        <Form.Item label="目标" required extra="发布时会快照目标的数据源、字段和选项。">
          <Select
            showSearch
            value={sinkId}
            options={sinks.map((item) => ({
              value: item.id,
              label: `${item.name}（${item.connector}）`,
            }))}
            onChange={(value) => setSinkId(Number(value))}
          />
        </Form.Item>
        <Form.Item label="字段预览">
          <div className={styles['schema-grid']}>
            <div>
              <Typography.Text type="secondary">源</Typography.Text>
              <div>{source ? schemaText(source.schema) : '-'}</div>
            </div>
            <div>
              <Typography.Text type="secondary">目标</Typography.Text>
              <div>{sink ? schemaText(sink.schema) : '-'}</div>
            </div>
          </div>
        </Form.Item>
      </Form>

      <TransformEditor
        sourceId={sourceId}
        sourceSchema={source?.schema ?? []}
        sinkSchema={sink?.schema ?? []}
        steps={transforms}
        initialPreview={pipeline.transformPreview}
        onChange={setTransforms}
      />
      <Space>
        <Button
          icon={<IconSave />}
          loading={updateMutation.isPending}
          onClick={() => void saveDraft().catch((err) => setError(errorMessage(err, '保存草稿失败')))}
        >
          保存草稿
        </Button>
        <Popconfirm
          title="发布后生成不可变版本，确认发布当前草稿？"
          okText="发布"
          cancelText="取消"
          onOk={() => void publish()}
        >
          <Button
            type="primary"
            icon={<IconSend />}
            loading={publishMutation.isPending}
            disabled={!source || !sink || (!hasTransforms && directMismatch)}
          >
            发布版本
          </Button>
        </Popconfirm>
      </Space>
    </Space>
  )
}

function VersionsTab({
  pipelineId,
  onRestored,
}: {
  pipelineId: number
  onRestored: () => void
}) {
  const navigate = useNavigate()
  const versionsQuery = usePipelineVersions(pipelineId)
  const runVersion = useRunPipelineVersion()
  const restoreVersion = useRestorePipelineVersion(pipelineId)
  const [snapshot, setSnapshot] = useState<PipelineVersion>()
  const [actionError, setActionError] = useState('')

  const run = async (version: PipelineVersion) => {
    setActionError('')
    try {
      const run = await runVersion.mutateAsync(version.id)
      navigate(`/pipeline-runs/${run.id}`)
    } catch (err) {
      setActionError(errorMessage(err, '启动版本运行失败'))
    }
  }

  const restoreDraft = async (version: PipelineVersion) => {
    setActionError('')
    try {
      await restoreVersion.mutateAsync(version.id)
      Message.success(`已用 v${version.versionNumber} 重建草稿`)
      onRestored()
    } catch (err) {
      setActionError(errorMessage(err, '重建草稿失败'))
    }
  }

  const columns: TableColumnProps<PipelineVersion>[] = [
    {
      title: '版本',
      dataIndex: 'versionNumber',
      width: 90,
      render: (value: number) => <Tag color="green">v{value}</Tag>,
    },
    { title: '链路', dataIndex: 'summary', ellipsis: true },
    {
      title: '发布时间',
      dataIndex: 'createdAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 220,
      render: (_, version) => (
        <Space size={2}>
          <Tooltip content="运行此版本">
            <Button
              type="text"
              size="small"
              aria-label={`运行版本 ${version.versionNumber}`}
              icon={<IconPlayArrow />}
              loading={runVersion.isPending && runVersion.variables === version.id}
              onClick={() => void run(version)}
            />
          </Tooltip>
          <Tooltip content="用此版本重建草稿">
            <Popconfirm
              content="将草稿的源与目标切换到该版本，名称和说明保留。确认重建？"
              okText="重建"
              cancelText="取消"
              onOk={() => void restoreDraft(version)}
            >
              <Button
                type="text"
                size="small"
                aria-label={`从版本 ${version.versionNumber} 重建草稿`}
                icon={<IconUndo />}
                loading={
                  restoreVersion.isPending && restoreVersion.variables === version.id
                }
              />
            </Popconfirm>
          </Tooltip>
          <Tooltip content="查看快照">
            <Button
              type="text"
              size="small"
              aria-label={`查看版本 ${version.versionNumber} 快照`}
              icon={<IconEye />}
              onClick={() => setSnapshot(version)}
            />
          </Tooltip>
        </Space>
      ),
    },
  ]
  return (
    <Space direction="vertical" size={12} style={{ width: '100%' }}>
      {actionError ? (
        <Alert
          type="error"
          content={actionError}
          closable
          onClose={() => setActionError('')}
        />
      ) : null}
      <Table<PipelineVersion>
        rowKey="id"
        tableLayoutFixed
        columns={columns}
        data={versionsQuery.data ?? []}
        loading={versionsQuery.isLoading}
        pagination={false}
        scroll={{ x: 820 }}
        noDataElement={<Empty description="还没有发布版本" />}
      />
      {snapshot ? <SnapshotModal version={snapshot} onClose={() => setSnapshot(undefined)} /> : null}
    </Space>
  )
}

function SnapshotModal({
  version,
  onClose,
}: {
  version: PipelineVersion
  onClose: () => void
}) {
  return (
    <Modal
      visible
      title={`版本 v${version.versionNumber} 快照`}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
      style={{ width: 'min(820px, calc(100vw - 48px))', top: 40 }}
    >
      <pre className={styles.snapshot}>
        {JSON.stringify(
          {
            transformLibraryVersion: version.transformLibraryVersion,
            source: version.source,
            transforms: version.transforms,
            sink: version.sink,
          },
          null,
          2,
        )}
      </pre>
    </Modal>
  )
}

function RunsTab({ pipelineId }: { pipelineId: number }) {
  const navigate = useNavigate()
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const runsQuery = usePipelineRuns({ pipelineId, page, pageSize })
  const columns: TableColumnProps<PipelineRun>[] = [
    { title: '编号', dataIndex: 'id', width: 80 },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (status: PipelineRun['status']) => <RunStatusTag status={status} />,
    },
    {
      title: '版本',
      dataIndex: 'versionNumber',
      width: 90,
      render: (value: number) => (value > 0 ? `v${value}` : '-'),
    },
    {
      title: '触发',
      dataIndex: 'trigger',
      width: 100,
      render: (value: string) => triggerLabel(value),
    },
    {
      title: '读取 / 写入',
      width: 140,
      render: (_, run) => `${formatCount(run.readRecords)} / ${formatCount(run.writeRecords)}`,
    },
    {
      title: '开始时间',
      dataIndex: 'startedAt',
      width: 180,
      render: (value: string) => formatTime(value),
    },
    {
      title: '耗时',
      width: 100,
      render: (_, run) => formatDuration(run.startedAt, run.finishedAt),
    },
    {
      title: '操作',
      width: 90,
      render: (_, run) => (
        <Button type="text" size="small" onClick={() => navigate(`/pipeline-runs/${run.id}`)}>
          详情
        </Button>
      ),
    },
  ]
  return (
    <Table<PipelineRun>
      rowKey="id"
      tableLayoutFixed
      columns={columns}
      data={runsQuery.data?.items ?? []}
      loading={runsQuery.isLoading}
      scroll={{ x: 1050 }}
      pagination={{
        current: runsQuery.data?.page ?? page,
        pageSize: runsQuery.data?.pageSize ?? pageSize,
        total: runsQuery.data?.total ?? 0,
        showTotal: true,
        sizeCanChange: true,
        onChange: (nextPage, nextPageSize) => {
          setPage(nextPage)
          setPageSize(nextPageSize)
        },
      }}
      noDataElement={<Empty description="还没有运行记录" />}
    />
  )
}
