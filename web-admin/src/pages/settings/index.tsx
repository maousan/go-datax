import {
  Alert,
  Button,
  Card,
  Form,
  Grid,
  InputNumber,
  Message,
  Spin,
  Typography,
} from '@arco-design/web-react'
import { useState } from 'react'
import { ApiError } from '../../api/client'
import {
  useRuntimeSettings,
  useUpdateRuntimeSettings,
  type RuntimeSettings,
} from '../../api/settings'

const MB = 1024 * 1024

function errorText(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback
}

export default function SettingsPage() {
  return (
    <Card title="系统设置">
      <RuntimeSettingsPanel />
    </Card>
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
