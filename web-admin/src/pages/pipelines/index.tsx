import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Link,
  Message,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import { IconEye, IconPlus, IconSearch } from '@arco-design/web-react/icon'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../../api/client'
import { useEndpoints } from '../../api/endpoints'
import {
  useCreatePipeline,
  usePipelines,
  type Pipeline,
} from '../../api/pipelines'
import RunStatusTag from '../../components/RunStatusTag'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'
import { pipelineChain, schemasMatch } from './helpers'
import styles from './index.module.less'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

export default function PipelinesPage() {
  const navigate = useNavigate()
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const [createOpen, setCreateOpen] = useState(false)
  const [tableRef, tableHeight] = useFillHeight(42)
  const searchTimer = useRef<number | undefined>(undefined)
  const pipelinesQuery = usePipelines(query)

  useEffect(
    () => () => {
      if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    },
    [],
  )

  const applySearch = () => {
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    setQuery(searchInput.trim())
  }

  const handleSearchChange = (value: string) => {
    setSearchInput(value)
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    if (!value.trim()) {
      setQuery('')
      return
    }
    searchTimer.current = window.setTimeout(() => setQuery(value.trim()), 300)
  }

  const columns: TableColumnProps<Pipeline>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 190,
      ellipsis: true,
      render: (_, pipeline) => (
        <Link onClick={() => navigate(`/pipelines/${pipeline.id}`)}>{pipeline.name}</Link>
      ),
    },
    {
      title: '链路',
      width: 240,
      ellipsis: true,
      render: (_, pipeline) => (
        <Space size={6}>
          <Tag color="arcoblue">{pipeline.source?.connector ?? '-'}</Tag>
          <span>{pipelineChain(pipeline)}</span>
        </Space>
      ),
    },
    {
      title: '最新版本',
      width: 110,
      render: (_, pipeline) =>
        pipeline.latestVersion ? (
          <Tag color="green">v{pipeline.latestVersion.versionNumber}</Tag>
        ) : (
          <Tag>未发布</Tag>
        ),
    },
    {
      title: '最近运行',
      width: 120,
      render: (_, pipeline) =>
        pipeline.lastRun ? <RunStatusTag status={pipeline.lastRun.status} /> : '-',
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 175,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 90,
      fixed: 'right',
      render: (_, pipeline) => (
        <Tooltip content="详情">
          <Button
            type="text"
            size="small"
            aria-label={`查看 Pipeline ${pipeline.name}`}
            icon={<IconEye />}
            onClick={() => navigate(`/pipelines/${pipeline.id}`)}
          />
        </Tooltip>
      ),
    },
  ]

  return (
    <Card className={listStyles.page}>
      <Typography.Title heading={6}>Pipelines</Typography.Title>

      <div className={listStyles.toolbar}>
        <Input
          allowClear
          aria-label="Pipeline 名称"
          className={listStyles['search-input']}
          prefix={<IconSearch />}
          placeholder="请输入 Pipeline 名称"
          value={searchInput}
          onChange={handleSearchChange}
          onPressEnter={applySearch}
        />
        <div className={listStyles.actions}>
          <Button type="primary" icon={<IconPlus />} onClick={() => setCreateOpen(true)}>
            新建 Pipeline
          </Button>
        </div>
      </div>

      {pipelinesQuery.isError ? (
        <Alert
          type="error"
          content={errorMessage(pipelinesQuery.error, '读取 Pipeline 失败')}
          style={{ marginBottom: 12 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<Pipeline>
          rowKey="id"
          tableLayoutFixed
          scroll={{ x: 925, y: tableHeight }}
          columns={columns}
          data={pipelinesQuery.data ?? []}
          loading={pipelinesQuery.isLoading}
          pagination={false}
          noDataElement={
            <Empty description={query ? '没有匹配的 Pipeline' : '还没有 Pipeline'} />
          }
        />
      </div>

      {createOpen ? (
        <CreatePipelineModal
          onClose={() => setCreateOpen(false)}
          onCreated={(pipeline) => {
            setCreateOpen(false)
            navigate(`/pipelines/${pipeline.id}`)
          }}
        />
      ) : null}
    </Card>
  )
}

function CreatePipelineModal({
  onClose,
  onCreated,
}: {
  onClose: () => void
  onCreated: (pipeline: Pipeline) => void
}) {
  const createMutation = useCreatePipeline()
  const sourcesQuery = useEndpoints('source', '')
  const sinksQuery = useEndpoints('sink', '')
  const [name, setName] = useState('')
  const [description, setDescription] = useState('')
  const [sourceId, setSourceId] = useState<number>()
  const [sinkId, setSinkId] = useState<number>()
  const [formError, setFormError] = useState('')

  const sources = sourcesQuery.data ?? []
  const sinks = sinksQuery.data ?? []
  const source = sources.find((item) => item.id === sourceId)
  const sink = sinks.find((item) => item.id === sinkId)
  const mismatch = Boolean(source && sink && !schemasMatch(source.schema, sink.schema))

  const save = async () => {
    if (!name.trim()) {
      setFormError('请输入名称')
      return
    }
    if (!sourceId) {
      setFormError('请选择源')
      return
    }
    if (!sinkId) {
      setFormError('请选择目标')
      return
    }
    if (!source || !sink) {
      setFormError('源或目标不存在')
      return
    }
    setFormError('')
    try {
      const pipeline = await createMutation.mutateAsync({
        name: name.trim(),
        description: description.trim(),
        sourceId,
        sinkId,
      })
      Message.success('Pipeline 已创建')
      onCreated(pipeline)
    } catch (err) {
      setFormError(errorMessage(err, '创建 Pipeline 失败'))
    }
  }

  return (
    <Modal
      visible
      title="新建 Pipeline"
      onCancel={onClose}
      unmountOnExit
      maskClosable={false}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" loading={createMutation.isPending} onClick={save}>
            创建
          </Button>
        </Space>
      }
      style={{ width: 'min(620px, calc(100vw - 48px))', top: 60 }}
    >
      {formError ? (
        <Alert
          type="error"
          content={formError}
          closable
          onClose={() => setFormError('')}
          style={{ marginBottom: 12 }}
        />
      ) : null}
      {mismatch ? (
        <Alert
          type="warning"
          content="源与目标的字段不一致；可以保存草稿，但发布会被拒绝。"
          style={{ marginBottom: 12 }}
        />
      ) : null}
      <Form layout="vertical">
        <Form.Item label="名称" required>
          <Input
            value={name}
            maxLength={100}
            placeholder="例如：订单同步"
            onChange={setName}
          />
        </Form.Item>
        <Form.Item label="说明">
          <Input.TextArea
            value={description}
            maxLength={500}
            rows={2}
            placeholder="可选"
            onChange={setDescription}
          />
        </Form.Item>
        <Form.Item
          label="源"
          required
          extra={sources.length === 0 ? '还没有源，请先在「源与目标」创建。' : undefined}
        >
          <Select
            showSearch
            allowClear
            value={sourceId}
            placeholder="选择源"
            options={sources.map((item) => ({
              value: item.id,
              label: `${item.name}（${item.connector}）`,
            }))}
            onChange={(value) => setSourceId(value === undefined ? undefined : Number(value))}
          />
        </Form.Item>
        <Form.Item
          label="目标"
          required
          extra={sinks.length === 0 ? '还没有目标，请先在「源与目标」创建。' : undefined}
        >
          <Select
            showSearch
            allowClear
            value={sinkId}
            placeholder="选择目标"
            options={sinks.map((item) => ({
              value: item.id,
              label: `${item.name}（${item.connector}）`,
            }))}
            onChange={(value) => setSinkId(value === undefined ? undefined : Number(value))}
          />
        </Form.Item>
        {source && sink ? (
          <div className={styles.chain}>
            <Typography.Text type="secondary">链路预览</Typography.Text>
            <Typography.Text>
              {source.name} → {sink.name}
            </Typography.Text>
            <Typography.Text type="secondary">
              {source.schema.map((column) => column.name).join(', ')}
            </Typography.Text>
          </div>
        ) : null}
      </Form>
    </Modal>
  )
}
