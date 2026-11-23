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
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Typography,
} from '@arco-design/web-react'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../../api/client'
import {
  useAccountSettings,
  useNotificationSettings,
  useRuntimeSettings,
  useTestNotification,
  useUpdateAccount,
  useUpdateNotificationSettings,
  useUpdateRuntimeSettings,
  type NotificationConfig,
  type NotificationEvent,
  type NotificationType,
  type RuntimeSettings,
} from '../../api/settings'
import { useAuth } from '../../auth/AuthContext'

const TYPE_OPTIONS: { value: NotificationType; label: string }[] = [
  { value: 'generic', label: '通用 JSON' },
  { value: 'feishu', label: '飞书' },
  { value: 'dingtalk', label: '钉钉' },
  { value: 'wecom', label: '企业微信' },
  { value: 'slack', label: 'Slack' },
]

const EVENT_OPTIONS: { value: NotificationEvent; label: string }[] = [
  { value: 'failed', label: '失败（最终失败）' },
  { value: 'recovered', label: '恢复（重试成功）' },
  { value: 'succeeded', label: '成功' },
  { value: 'canceled', label: '取消' },
]

const MB = 1024 * 1024

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

export default function SettingsPage() {
  return (
    <Card title="系统设置">
      <Tabs defaultActiveTab="notifications">
        <Tabs.TabPane key="notifications" title="通知设置">
          <NotificationSettingsPanel />
        </Tabs.TabPane>
        <Tabs.TabPane key="runtime" title="运行参数">
          <RuntimeSettingsPanel />
        </Tabs.TabPane>
        <Tabs.TabPane key="account" title="账号安全">
          <AccountSettingsPanel />
        </Tabs.TabPane>
      </Tabs>
    </Card>
  )
}

function NotificationSettingsPanel() {
  const query = useNotificationSettings()
  if (query.isLoading) return <Spin />
  if (query.isError || !query.data) {
    return <Alert type="error" content={errorText(query.error, '读取通知设置失败')} />
  }
  return <NotificationSettingsForm config={query.data} />
}

function NotificationSettingsForm({ config }: { config: NotificationConfig }) {
  const [form] = Form.useForm<NotificationConfig>()
  const updateMutation = useUpdateNotificationSettings()
  const testMutation = useTestNotification()
  const [result, setResult] = useState<{ ok: boolean; message: string } | null>(null)

  const normalizedPayload = (values: Partial<NotificationConfig>): NotificationConfig => ({
    enabled: values.enabled ?? false,
    type: values.type ?? 'generic',
    url: values.url?.trim() ?? '',
    events: values.events ?? [],
    timeoutSeconds: values.timeoutSeconds ?? 10,
  })

  const handleSave = async () => {
    let values: NotificationConfig
    try {
      values = await form.validate()
    } catch {
      return
    }
    const payload = normalizedPayload(values)
    if (payload.enabled && !payload.url) {
      setResult({ ok: false, message: '启用通知时必须填写 Webhook 地址' })
      return
    }
    if (payload.enabled && payload.events.length === 0) {
      setResult({ ok: false, message: '启用通知时至少选择一个事件' })
      return
    }

    updateMutation.mutate(payload, {
      onSuccess: () => {
        setResult(null)
        Message.success('通知设置已保存')
      },
      onError: (err) => setResult({ ok: false, message: errorText(err, '保存通知设置失败') }),
    })
  }

  const handleTest = () => {
    const payload = normalizedPayload(form.getFieldsValue())
    if (!payload.url) {
      setResult({ ok: false, message: '请先填写 Webhook 地址' })
      return
    }

    testMutation.mutate(
      { ...payload, enabled: true },
      {
        onSuccess: () => setResult({ ok: true, message: '测试通知已发送，请检查接收端' }),
        onError: (err) => setResult({ ok: false, message: errorText(err, '测试通知发送失败') }),
      },
    )
  }

  return (
    <>
      <Typography.Paragraph type="secondary">
        任务运行结束时按事件发送 Webhook。失败后还会自动重试的任务，只在最终失败或恢复时通知。
      </Typography.Paragraph>

      <Form form={form} layout="vertical" initialValues={config} style={{ maxWidth: 760 }}>
        <Form.Item field="enabled" label="启用通知" triggerPropName="checked">
          <Switch />
        </Form.Item>

        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item field="type" label="通知类型">
              <Select options={TYPE_OPTIONS} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item field="timeoutSeconds" label="请求超时（秒）">
              <InputNumber min={1} max={60} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        <Form.Item
          field="url"
          label="Webhook 地址"
          extra="支持 http/https；通用 JSON 会收到结构化事件，其余类型使用文本消息模板"
        >
          <Input placeholder="https://example.com/webhook" allowClear />
        </Form.Item>

        <Form.Item field="events" label="通知事件">
          <Checkbox.Group options={EVENT_OPTIONS} />
        </Form.Item>

        {result ? (
          <Alert
            type={result.ok ? 'success' : 'error'}
            content={result.message}
            closable
            onClose={() => setResult(null)}
            style={{ marginBottom: 16 }}
          />
        ) : null}

        <Space>
          <Button type="primary" loading={updateMutation.isPending} onClick={handleSave}>
            保存
          </Button>
          <Button loading={testMutation.isPending} onClick={handleTest}>
            发送测试
          </Button>
        </Space>
      </Form>
    </>
  )
}

function RuntimeSettingsPanel() {
  const query = useRuntimeSettings()
  if (query.isLoading) return <Spin />
  if (query.isError || !query.data) {
    return <Alert type="error" content={errorText(query.error, '读取运行参数失败')} />
  }
  return <RuntimeSettingsForm settings={query.data} />
}

interface RuntimeFormValues {
  maxConcurrent: number
  maxRuns: number
  maxAgeDays: number
  maxLogBytesMB: number
}

function RuntimeSettingsForm({ settings }: { settings: RuntimeSettings }) {
  const [form] = Form.useForm<RuntimeFormValues>()
  const updateMutation = useUpdateRuntimeSettings()
  const [error, setError] = useState('')

  const handleSave = async () => {
    let values: RuntimeFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }

    updateMutation.mutate(
      {
        maxConcurrent: values.maxConcurrent,
        maxRuns: values.maxRuns,
        maxAgeDays: values.maxAgeDays,
        maxLogBytes: Math.round(values.maxLogBytesMB * MB),
      },
      {
        onSuccess: () => {
          setError('')
          Message.success('运行参数已保存')
        },
        onError: (err) => setError(errorText(err, '保存运行参数失败')),
      },
    )
  }

  return (
    <>
      <Typography.Paragraph type="secondary">
        保存后立即生效。运行中的任务继续使用启动时的日志上限；调整并发上限不会中断已在运行的任务。
      </Typography.Paragraph>

      <Form
        form={form}
        layout="vertical"
        initialValues={{
          maxConcurrent: settings.maxConcurrent,
          maxRuns: settings.maxRuns,
          maxAgeDays: settings.maxAgeDays,
          maxLogBytesMB: Math.round(settings.maxLogBytes / MB),
        }}
        style={{ maxWidth: 760 }}
      >
        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item field="maxConcurrent" label="并发上限" extra="同时运行的任务数，1–16">
              <InputNumber min={1} max={16} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item field="maxRuns" label="保留运行数" extra="最多保留多少条历史，1–100000">
              <InputNumber min={1} max={100000} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item field="maxAgeDays" label="保留天数" extra="超过天数的运行会被清理，1–3650">
              <InputNumber min={1} max={3650} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item field="maxLogBytesMB" label="单次日志上限（MB）" extra="1–1024">
              <InputNumber min={1} max={1024} style={{ width: '100%' }} />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        {error ? (
          <Alert
            type="error"
            content={error}
            closable
            onClose={() => setError('')}
            style={{ marginBottom: 16 }}
          />
        ) : null}

        <Button type="primary" loading={updateMutation.isPending} onClick={handleSave}>
          保存
        </Button>
      </Form>
    </>
  )
}

function AccountSettingsPanel() {
  const query = useAccountSettings()
  if (query.isLoading) return <Spin />
  if (query.isError || !query.data) {
    return <Alert type="error" content={errorText(query.error, '读取账号信息失败')} />
  }
  return <AccountSettingsForm username={query.data.username} />
}

interface AccountFormValues {
  username: string
  currentPassword: string
  newPassword?: string
  confirmPassword?: string
}

function AccountSettingsForm({ username }: { username: string }) {
  const [form] = Form.useForm<AccountFormValues>()
  const updateMutation = useUpdateAccount()
  const { logout } = useAuth()
  const navigate = useNavigate()
  const [error, setError] = useState('')

  const handleSave = async () => {
    let values: AccountFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }

    if (values.newPassword && values.newPassword.length < 8) {
      setError('新密码至少 8 个字符')
      return
    }
    if (values.newPassword && values.newPassword !== values.confirmPassword) {
      setError('两次输入的新密码不一致')
      return
    }
    if (!values.newPassword && values.confirmPassword) {
      setError('请同时填写新密码')
      return
    }
    if (values.username.trim() === username && !values.newPassword) {
      setError('没有需要修改的内容')
      return
    }

    updateMutation.mutate(
      {
        username: values.username.trim(),
        currentPassword: values.currentPassword,
        newPassword: values.newPassword || undefined,
      },
      {
        onSuccess: () => {
          Message.success('账号已更新，请重新登录')
          logout()
          navigate('/login', { replace: true })
        },
        onError: (err) => setError(errorText(err, '更新账号失败')),
      },
    )
  }

  return (
    <>
      <Typography.Paragraph type="secondary">
        修改用户名或密码后，所有已登录会话会立即失效，需要重新登录。
      </Typography.Paragraph>

      <Form
        form={form}
        layout="vertical"
        initialValues={{ username }}
        style={{ maxWidth: 760 }}
      >
        <Form.Item
          field="username"
          label="用户名"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input maxLength={100} />
        </Form.Item>

        <Form.Item
          field="currentPassword"
          label="当前密码"
          rules={[{ required: true, message: '请输入当前密码' }]}
        >
          <Input.Password />
        </Form.Item>

        <Grid.Row gutter={16}>
          <Grid.Col span={12}>
            <Form.Item field="newPassword" label="新密码" extra="至少 8 个字符；留空表示不修改">
              <Input.Password />
            </Form.Item>
          </Grid.Col>
          <Grid.Col span={12}>
            <Form.Item field="confirmPassword" label="确认新密码">
              <Input.Password />
            </Form.Item>
          </Grid.Col>
        </Grid.Row>

        {error ? (
          <Alert
            type="error"
            content={error}
            closable
            onClose={() => setError('')}
            style={{ marginBottom: 16 }}
          />
        ) : null}

        <Button
          type="primary"
          status="warning"
          loading={updateMutation.isPending}
          onClick={handleSave}
        >
          保存并重新登录
        </Button>
      </Form>
    </>
  )
}
