import { Alert, Form, Input, Message, Modal } from '@arco-design/web-react'
import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { useNavigate } from 'react-router'
import { ApiError } from '../../api/client'
import { useUpdateAccount } from '../../api/settings'
import { useAuth } from '../../auth/AuthContext'

interface PasswordFormValues {
  currentPassword: string
  newPassword: string
  confirmPassword: string
}

export default function ChangePasswordModal({
  visible,
  onClose,
}: {
  visible: boolean
  onClose: () => void
}) {
  const [form] = Form.useForm<PasswordFormValues>()
  const [error, setError] = useState('')
  const updateMutation = useUpdateAccount()
  const { logout } = useAuth()
  const queryClient = useQueryClient()
  const navigate = useNavigate()

  const handleClose = () => {
    if (updateMutation.isPending) return
    form.resetFields()
    setError('')
    onClose()
  }

  const handleOk = async () => {
    let values: PasswordFormValues
    try {
      values = await form.validate()
    } catch {
      return
    }
    if (values.newPassword !== values.confirmPassword) {
      setError('两次输入的新密码不一致')
      return
    }

    setError('')
    updateMutation.mutate(
      {
        currentPassword: values.currentPassword,
        newPassword: values.newPassword,
      },
      {
        onSuccess: () => {
          Message.success('密码已修改，请重新登录')
          logout()
          queryClient.clear()
          navigate('/login', { replace: true })
        },
        onError: (err) => {
          setError(err instanceof ApiError ? err.message : '修改密码失败')
        },
      },
    )
  }

  return (
    <Modal
      title="修改密码"
      visible={visible}
      okText="确认修改"
      cancelText="取消"
      confirmLoading={updateMutation.isPending}
      onOk={handleOk}
      onCancel={handleClose}
      maskClosable={false}
      unmountOnExit
      style={{ width: 460 }}
    >
      <Alert
        type="info"
        content="修改成功后所有已登录会话会立即失效，需要重新登录。"
        style={{ marginBottom: 16 }}
      />
      {error ? (
        <Alert
          type="error"
          content={error}
          closable
          onClose={() => setError('')}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <Form form={form} layout="vertical">
        <Form.Item
          field="currentPassword"
          label="当前密码"
          rules={[{ required: true, message: '请输入当前密码' }]}
        >
          <Input.Password placeholder="请输入当前密码" />
        </Form.Item>

        <Form.Item
          field="newPassword"
          label="新密码"
          rules={[
            { required: true, message: '请输入新密码' },
            {
              validator: (value: string | undefined, callback: (message?: string) => void) => {
                if (value && value.length < 8) {
                  callback('新密码至少 8 个字符')
                  return
                }
                callback()
              },
            },
          ]}
        >
          <Input.Password placeholder="至少 8 个字符" />
        </Form.Item>

        <Form.Item
          field="confirmPassword"
          label="确认新密码"
          dependencies={['newPassword']}
          rules={[
            { required: true, message: '请再次输入新密码' },
            {
              validator: (value: string | undefined, callback: (message?: string) => void) => {
                if (value && value !== form.getFieldValue('newPassword')) {
                  callback('两次输入的新密码不一致')
                  return
                }
                callback()
              },
            },
          ]}
        >
          <Input.Password placeholder="请再次输入新密码" />
        </Form.Item>
      </Form>
    </Modal>
  )
}
