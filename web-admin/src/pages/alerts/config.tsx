import {
  Alert,
  Button,
  Card,
  Checkbox,
  Form,
  Grid,
  Input,
  InputNumber,
  Message,
  Modal,
  Popconfirm,
  Select,
  Space,
  Spin,
  Switch,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconPlus,
  IconSend,
} from '@arco-design/web-react/icon'
import { useState } from 'react'
import {
  useAlertConfig,
  useTestAlertChannel,
  useUpdateAlertConfig,
  type AlertChannel,
  type AlertChannelType,
  type AlertConfig,
  type AlertEvent,
} from '../../api/alerts'
import { ApiError } from '../../api/client'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import styles from './config.module.less'

const TYPE_OPTIONS: { value: AlertChannelType; label: string }[] = [
  { value: 'email', label: '邮箱' },
  { value: 'dingtalk', label: '钉钉' },
  { value: 'feishu', label: '飞书' },
  { value: 'wecom', label: '企业微信' },
  { value: 'slack', label: 'Slack' },
  { value: 'webhook', label: '通用 Webhook' },
]

const TYPE_META: Record<AlertChannelType, { label: string; color: string }> = {
  email: { label: '邮箱', color: 'orange' },
  dingtalk: { label: '钉钉', color: 'arcoblue' },
  feishu: { label: '飞书', color: 'blue' },
  wecom: { label: '企业微信', color: 'cyan' },
  slack: { label: 'Slack', color: 'purple' },
  webhook: { label: '通用 Webhook', color: 'gray' },
}

const EVENT_OPTIONS: { value: AlertEvent; label: string }[] = [
  { value: 'failed', label: '失败（最终失败）' },
  { value: 'recovered', label: '恢复（重试成功）' },
  { value: 'succeeded', label: '成功' },
  { value: 'canceled', label: '取消' },
]

const EVENT_LABELS: Record<AlertEvent, string> = {
  failed: '失败',
  recovered: '恢复',
  succeeded: '成功',
  canceled: '取消',
}

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

function newChannelID(): string {
  return `ch_${Date.now().toString(36)}${Math.random().toString(36).slice(2, 10)}`
}

function blankChannel(): AlertChannel {
  return {
    id: newChannelID(),
    name: '',
    type: 'dingtalk',
    enabled: true,
    events: ['failed', 'recovered'],
    useTLS: true,
    smtpPort: 465,
    to: [],
  }
}

interface GlobalFormValues {
  enabled: boolean
  timeoutSeconds: number
  retentionDays: number
}

export default function AlertConfigPage() {
  const query = useAlertConfig()
  if (query.isLoading) {
    return (
      <Card>
        <Spin />
      </Card>
    )
  }
  if (query.isError || !query.data) {
    return (
      <Card>
        <Alert type="error" content={errorText(query.error, '读取告警配置失败')} />
      </Card>
    )
  }
  return <AlertConfigEditor config={query.data} />
}

function AlertConfigEditor({ config }: { config: AlertConfig }) {
  const [form] = Form.useForm<GlobalFormValues>()
  const updateMutation = useUpdateAlertConfig()
  const testMutation = useTestAlertChannel()
  const [channels, setChannels] = useState<AlertChannel[]>(config.channels)
  const [pageError, setPageError] = useState('')
  const [testingId, setTestingId] = useState('')
  const [modalState, setModalState] = useState<{
    channel: AlertChannel
    isNew: boolean
  } | null>(null)
  const [tableRef, tableHeight] = useFillHeight(40)

  const handleSave = async () => {
    let values: GlobalFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }

    setPageError('')
    try {
      const saved = await updateMutation.mutateAsync({
        enabled: values.enabled,
        timeoutSeconds: values.timeoutSeconds,
        retentionDays: values.retentionDays,
        channels,
      })
      setChannels(saved.channels)
      Message.success('告警配置已保存')
    } catch (err) {
      setPageError(errorText(err, '保存告警配置失败'))
    }
  }

  const currentTimeout = (): number => {
    const value = form.getFieldValue('timeoutSeconds')
    return typeof value === 'number' && value > 0 ? value : config.timeoutSeconds
  }

  const handleTest = (channel: AlertChannel) => {
    setTestingId(channel.id)
    testMutation.mutate(
      {
        channel,
        timeoutSeconds: currentTimeout(),
      },
      {
        onSuccess: () => Message.success(`测试告警已发送到「${channel.name}」`),
        onError: (err) => Message.error(errorText(err, '测试告警发送失败')),
        onSettled: () => setTestingId(''),
      },
    )
  }

  const toggleChannel = (id: string, enabled: boolean) => {
    setChannels((previous) =>
      previous.map((channel) => (channel.id === id ? { ...channel, enabled } : channel)),
    )
  }

  const removeChannel = (id: string) => {
    setChannels((previous) => previous.filter((channel) => channel.id !== id))
  }

  const handleChannelSaved = (channel: AlertChannel) => {
    setChannels((previous) => {
      const exists = previous.some((item) => item.id === channel.id)
      return exists
        ? previous.map((item) => (item.id === channel.id ? channel : item))
        : [...previous, channel]
    })
    setModalState(null)
  }

  const columns: TableColumnProps<AlertChannel>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 180,
      ellipsis: true,
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 130,
      render: (type: AlertChannelType) => (
        <Tag color={TYPE_META[type]?.color}>{TYPE_META[type]?.label ?? type}</Tag>
      ),
    },
    {
      title: '告警事件',
      dataIndex: 'events',
      render: (events: AlertEvent[]) => (
        <Space size={4}>
          {events.map((event) => (
            <Tag key={event}>{EVENT_LABELS[event] ?? event}</Tag>
          ))}
        </Space>
      ),
    },
    {
      title: '状态',
      dataIndex: 'enabled',
      width: 90,
      render: (enabled: boolean, channel) => (
        <Switch
          size="small"
          checked={enabled}
          onChange={(checked) => toggleChannel(channel.id, checked)}
        />
      ),
    },
    {
      title: '操作',
      width: 170,
      fixed: 'right',
      render: (_, channel) => (
        <Space size={2}>
          <Tooltip content="发送测试告警">
            <Button
              type="text"
              size="small"
              icon={<IconSend />}
              loading={testMutation.isPending && testingId === channel.id}
              onClick={() => handleTest(channel)}
            />
          </Tooltip>
          <Tooltip content="编辑">
            <Button
              type="text"
              size="small"
              icon={<IconEdit />}
              onClick={() => setModalState({ channel, isNew: false })}
            />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该告警渠道？历史告警日志会保留。"
              okText="确定"
              cancelText="取消"
              onOk={() => removeChannel(channel.id)}
            >
              <Button type="text" status="danger" size="small" icon={<IconDelete />} />
            </Popconfirm>
          </Tooltip>
        </Space>
      ),
    },
  ]

  return (
    <Card className={listStyles.page}>
      <Form
        form={form}
        layout="vertical"
        initialValues={{
          enabled: config.enabled,
          timeoutSeconds: config.timeoutSeconds,
          retentionDays: config.retentionDays,
        }}
      >
        <div className={listStyles['button-group']}>
          <div className={styles['title-row']}>
            <Typography.Title heading={6} style={{ marginBottom: 0 }}>
              告警配置
            </Typography.Title>
            <div className={styles['global-switch']}>
              <Typography.Text type="secondary">启用告警</Typography.Text>
              <Form.Item field="enabled" triggerPropName="checked" noStyle>
                <Switch aria-label="启用告警" />
              </Form.Item>
            </div>
          </div>
          <Space>
            <Button icon={<IconPlus />} onClick={() => setModalState({ channel: blankChannel(), isNew: true })}>
              新建渠道
            </Button>
            <Button
              type="primary"
              loading={updateMutation.isPending}
              onClick={handleSave}
            >
              保存配置
            </Button>
          </Space>
        </div>

        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item
              field="timeoutSeconds"
              label="请求超时（秒）"
              extra="SMTP 与 Webhook 共用，1–60"
            >
              <InputNumber min={1} max={60} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item
              field="retentionDays"
              label="日志保留天数"
              extra="超过天数的告警日志会自动清理，1–3650"
            >
              <InputNumber min={1} max={3650} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>
      </Form>

      <Typography.Paragraph type="secondary" style={{ marginTop: -8 }}>
        任务运行结束后按事件发送到已启用的渠道；失败后还会自动重试的任务，只在最终失败或恢复时告警。
        渠道与全局开关修改后需要点击「保存配置」才会生效，渠道列表中的「发送测试」不受开关影响。
      </Typography.Paragraph>

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          onClose={() => setPageError('')}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<AlertChannel>
          rowKey="id"
          tableLayoutFixed
          columns={columns}
          data={channels}
          pagination={false}
          scroll={{ x: 900, y: tableHeight }}
          noDataElement={<span>还没有告警渠道</span>}
        />
      </div>

      {modalState ? (
        <ChannelModal
          key={modalState.channel.id}
          channel={modalState.channel}
          isNew={modalState.isNew}
          timeoutSeconds={currentTimeout()}
          onClose={() => setModalState(null)}
          onSave={handleChannelSaved}
        />
      ) : null}
    </Card>
  )
}

interface ChannelFormValues {
  name: string
  type: AlertChannelType
  events: AlertEvent[]
  url?: string
  secret?: string
  smtpHost?: string
  smtpPort?: number
  smtpUsername?: string
  smtpPassword?: string
  from?: string
  to?: string[]
  useTLS?: boolean
}

function ChannelModal({
  channel,
  isNew,
  timeoutSeconds,
  onClose,
  onSave,
}: {
  channel: AlertChannel
  isNew: boolean
  timeoutSeconds: number
  onClose: () => void
  onSave: (channel: AlertChannel) => void
}) {
  const [form] = Form.useForm<ChannelFormValues>()
  const testMutation = useTestAlertChannel()
  const [type, setType] = useState<AlertChannelType>(channel.type)
  const [testResult, setTestResult] = useState<{ ok: boolean; message: string } | null>(null)

  const initialValues: ChannelFormValues = {
    name: channel.name,
    type: channel.type,
    events: channel.events,
    url: channel.url ?? '',
    secret: channel.secret ?? '',
    smtpHost: channel.smtpHost ?? '',
    smtpPort: channel.smtpPort ?? 465,
    smtpUsername: channel.smtpUsername ?? '',
    smtpPassword: channel.smtpPassword ?? '',
    from: channel.from ?? '',
    to: channel.to ?? [],
    useTLS: channel.useTLS ?? true,
  }

  const buildChannel = (values: ChannelFormValues): AlertChannel => {
    const result: AlertChannel = {
      id: channel.id,
      name: values.name.trim(),
      type: values.type,
      enabled: channel.enabled,
      events: values.events ?? [],
    }
    if (values.type === 'email') {
      result.smtpHost = values.smtpHost?.trim()
      result.smtpPort = values.smtpPort
      result.smtpUsername = values.smtpUsername?.trim()
      result.smtpPassword = values.smtpPassword
      result.from = values.from?.trim()
      result.to = values.to ?? []
      result.useTLS = values.useTLS
    } else {
      result.url = values.url?.trim()
      if (values.type === 'dingtalk' || values.type === 'feishu') {
        result.secret = values.secret
      }
    }
    return result
  }

  const handleSave = async () => {
    let values: ChannelFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }
    onSave(buildChannel(values))
  }

  const handleTest = async () => {
    let values: ChannelFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }

    const target = buildChannel(values)
    setTestResult(null)
    testMutation.mutate(
      { channel: target, timeoutSeconds },
      {
        onSuccess: () => setTestResult({ ok: true, message: '测试告警已发送，请检查接收端' }),
        onError: (err) =>
          setTestResult({ ok: false, message: errorText(err, '测试告警发送失败') }),
      },
    )
  }

  return (
    <Modal
      title={isNew ? '新建告警渠道' : '编辑告警渠道'}
      visible
      unmountOnExit
      style={{ width: 680 }}
      onCancel={onClose}
      footer={
        <Space>
          <Button onClick={onClose}>取消</Button>
          <Button loading={testMutation.isPending} onClick={handleTest}>
            发送测试
          </Button>
          <Button type="primary" onClick={handleSave}>
            确定
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical" initialValues={initialValues}>
        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item
              field="name"
              label="渠道名称"
              rules={[{ required: true, message: '请输入渠道名称' }]}
            >
              <Input maxLength={100} placeholder="例如：运维告警群" allowClear />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item field="type" label="渠道类型">
              <Select
                options={TYPE_OPTIONS}
                onChange={(value: AlertChannelType) => setType(value)}
              />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        <Form.Item
          field="events"
          label="告警事件"
          rules={[{ required: true, message: '至少选择一个告警事件' }]}
        >
          <Checkbox.Group options={EVENT_OPTIONS} />
        </Form.Item>

        {type === 'email' ? (
          <>
            <Grid.Row gutter={16}>
              <Grid.Col span={12}>
                <Form.Item
                  field="smtpHost"
                  label="SMTP 服务器"
                  rules={[{ required: true, message: '请输入 SMTP 服务器' }]}
                >
                  <Input placeholder="smtp.example.com" allowClear />
                </Form.Item>
              </Grid.Col>
              <Grid.Col span={6}>
                <Form.Item
                  field="smtpPort"
                  label="端口"
                  rules={[{ required: true, message: '请输入端口' }]}
                >
                  <InputNumber min={1} max={65535} style={{ width: '100%' }} />
                </Form.Item>
              </Grid.Col>
              <Grid.Col span={6}>
                <Form.Item
                  field="useTLS"
                  label="SSL/TLS"
                  triggerPropName="checked"
                  extra="465 端口通常开启"
                >
                  <Switch />
                </Form.Item>
              </Grid.Col>
            </Grid.Row>

            <Grid.Row gutter={16}>
              <Grid.Col span={12}>
                <Form.Item field="smtpUsername" label="SMTP 用户名">
                  <Input autoComplete="off" allowClear />
                </Form.Item>
              </Grid.Col>
              <Grid.Col span={12}>
                <Form.Item
                  field="smtpPassword"
                  label="SMTP 密码"
                  extra="留空或保持 ****** 表示不修改"
                >
                  <Input.Password autoComplete="new-password" />
                </Form.Item>
              </Grid.Col>
            </Grid.Row>

            <Grid.Row gutter={16}>
              <Grid.Col span={12}>
                <Form.Item
                  field="from"
                  label="发件人"
                  extra="留空时使用 SMTP 用户名"
                >
                  <Input placeholder="alert@example.com" allowClear />
                </Form.Item>
              </Grid.Col>
              <Grid.Col span={12}>
                <Form.Item
                  field="to"
                  label="收件人"
                  rules={[{ required: true, message: '至少填写一个收件人' }]}
                >
                  <Select
                    mode="multiple"
                    allowCreate
                    placeholder="输入邮箱后回车"
                  />
                </Form.Item>
              </Grid.Col>
            </Grid.Row>
          </>
        ) : (
          <>
            <Form.Item
              field="url"
              label={type === 'webhook' ? 'Webhook 地址' : '机器人 Webhook 地址'}
              extra="支持 http/https；企业微信请使用机器人 Webhook 完整地址"
              rules={[
                { required: true, message: '请输入 Webhook 地址' },
                {
                  match: /^https?:\/\/.+/,
                  message: '必须是 http 或 https URL',
                },
              ]}
            >
              <Input placeholder="https://example.com/webhook" allowClear />
            </Form.Item>

            {type === 'dingtalk' || type === 'feishu' ? (
              <Form.Item
                field="secret"
                label="签名密钥"
                extra="机器人开启「加签」时填写；留空或保持 ****** 表示不修改"
              >
                <Input.Password autoComplete="new-password" placeholder="可选" />
              </Form.Item>
            ) : null}
          </>
        )}
      </Form>

      {testResult ? (
        <Alert
          type={testResult.ok ? 'success' : 'error'}
          content={testResult.message}
          closable
          onClose={() => setTestResult(null)}
          style={{ marginTop: 16 }}
        />
      ) : null}
    </Modal>
  )
}
