import { Button, Checkbox, Form, Input, Space } from '@arco-design/web-react'
import type { FormInstance } from '@arco-design/web-react/es/Form'
import { IconLock, IconUser } from '@arco-design/web-react/icon'
import { useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import styles from './style/index.module.less'

interface LoginFormValues {
  username: string
  password: string
}

const REMEMBER_KEY = 'go-datax:last-username'

export default function LoginForm({ from }: { from: string }) {
  const { login } = useAuth()
  const navigate = useNavigate()
  const formRef = useRef<FormInstance<LoginFormValues>>(null)
  const remembered = window.localStorage.getItem(REMEMBER_KEY) ?? ''
  const [rememberPassword, setRememberPassword] = useState(Boolean(remembered))
  const [errorMessage, setErrorMessage] = useState('')
  const [loading, setLoading] = useState(false)

  useEffect(() => {
    if (remembered) {
      formRef.current?.setFieldsValue({ username: remembered })
    }
  }, [remembered])

  const loginWithForm = async () => {
    let values: LoginFormValues
    try {
      values = await formRef.current!.validate()
    } catch {
      return
    }

    setErrorMessage('')
    setLoading(true)
    try {
      await login(values.username, values.password)
      if (rememberPassword) {
        window.localStorage.setItem(REMEMBER_KEY, values.username)
      } else {
        window.localStorage.removeItem(REMEMBER_KEY)
      }
      navigate(from, { replace: true })
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) {
        setErrorMessage('用户名或密码错误')
      } else {
        setErrorMessage('登录出错，请稍后重试')
      }
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className={styles['login-form-wrapper']}>
      <div className={styles['login-form-title']}>登录 go-datax 管理台</div>
      <div className={styles['login-form-sub-title']}>请输入账号信息以继续</div>
      <div className={styles['login-form-error-msg']}>{errorMessage}</div>
      <Form
        className={styles['login-form']}
        layout="vertical"
        ref={formRef}
        initialValues={{ username: remembered, password: '' }}
      >
        <Form.Item
          field="username"
          rules={[{ required: true, message: '用户名不能为空' }]}
        >
          <Input
            prefix={<IconUser />}
            placeholder="用户名"
            autoComplete="username"
            onPressEnter={loginWithForm}
          />
        </Form.Item>
        <Form.Item field="password" rules={[{ required: true, message: '密码不能为空' }]}>
          <Input.Password
            prefix={<IconLock />}
            placeholder="密码"
            autoComplete="current-password"
            onPressEnter={loginWithForm}
          />
        </Form.Item>
        <Space size={16} direction="vertical" style={{ width: '100%' }}>
          <div className={styles['login-form-password-actions']}>
            <Checkbox checked={rememberPassword} onChange={setRememberPassword}>
              记住用户名
            </Checkbox>
          </div>
          <Button type="primary" long onClick={loginWithForm} loading={loading}>
            登录
          </Button>
        </Space>
      </Form>
    </div>
  )
}
