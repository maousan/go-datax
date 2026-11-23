import { Alert, Button, Card, Form, Input, Typography } from 'antd'
import { useState } from 'react'
import { Navigate, useLocation } from 'react-router'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'

interface LoginFormValues {
  username: string
  password: string
}

export default function LoginPage() {
  const { token, login } = useAuth()
  const location = useLocation()
  const [error, setError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const from = (location.state as { from?: string } | null)?.from ?? '/'

  if (token) {
    return <Navigate to={from} replace />
  }

  const handleFinish = async (values: LoginFormValues) => {
    setSubmitting(true)
    setError('')
    try {
      await login(values.username, values.password)
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setError('用户名或密码错误')
      } else {
        setError('无法连接服务，请稍后重试')
      }
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div
      style={{
        alignItems: 'center',
        background: '#f5f5f5',
        display: 'flex',
        justifyContent: 'center',
        minHeight: '100vh',
        padding: 24,
      }}
    >
      <Card style={{ width: 380 }}>
        <Typography.Title level={3} style={{ marginTop: 0, textAlign: 'center' }}>
          go-datax 管理台
        </Typography.Title>
        <Typography.Paragraph type="secondary" style={{ textAlign: 'center' }}>
          请登录后管理数据同步
        </Typography.Paragraph>

        <Form<LoginFormValues> layout="vertical" requiredMark={false} onFinish={handleFinish}>
          <Form.Item
            name="username"
            label="用户名"
            rules={[{ required: true, message: '请输入用户名' }]}
          >
            <Input placeholder="用户名" autoComplete="username" />
          </Form.Item>
          <Form.Item name="password" label="密码" rules={[{ required: true, message: '请输入密码' }]}>
            <Input.Password placeholder="密码" autoComplete="current-password" />
          </Form.Item>

          {error ? (
            <Alert type="error" message={error} showIcon style={{ marginBottom: 16 }} />
          ) : null}

          <Button type="primary" htmlType="submit" block loading={submitting}>
            登录
          </Button>
        </Form>
      </Card>
    </div>
  )
}
