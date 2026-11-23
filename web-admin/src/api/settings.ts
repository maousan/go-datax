import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPost, apiPut } from './client'

export type NotificationType = 'generic' | 'feishu' | 'dingtalk' | 'wecom' | 'slack'
export type NotificationEvent = 'failed' | 'recovered' | 'succeeded' | 'canceled'

export interface NotificationConfig {
  enabled: boolean
  type: NotificationType
  url: string
  events: NotificationEvent[]
  timeoutSeconds: number
}

const NOTIFICATION_KEY = ['settings', 'notifications'] as const
const ACCOUNT_KEY = ['settings', 'account'] as const
const RUNTIME_KEY = ['settings', 'runtime'] as const

export interface AccountSettings {
  username: string
}

export interface UpdateAccountInput {
  username?: string
  currentPassword: string
  newPassword?: string
}

export interface RuntimeSettings {
  maxConcurrent: number
  maxRuns: number
  maxAgeDays: number
  maxLogBytes: number
}

export function useNotificationSettings() {
  return useQuery({
    queryKey: NOTIFICATION_KEY,
    queryFn: () => apiGet<NotificationConfig>('/api/v1/settings/notifications'),
  })
}

export function useUpdateNotificationSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: NotificationConfig) =>
      apiPut<NotificationConfig>('/api/v1/settings/notifications', input),
    onSuccess: (data) => {
      queryClient.setQueryData(NOTIFICATION_KEY, data)
    },
  })
}

export function useTestNotification() {
  return useMutation({
    mutationFn: (input: NotificationConfig) =>
      apiPost<{ ok: boolean }>('/api/v1/settings/notifications/test', input),
  })
}

export function useAccountSettings() {
  return useQuery({
    queryKey: ACCOUNT_KEY,
    queryFn: () => apiGet<AccountSettings>('/api/v1/settings/account'),
  })
}

export function useUpdateAccount() {
  return useMutation({
    mutationFn: (input: UpdateAccountInput) =>
      apiPut<{ ok: boolean; relogin: boolean }>('/api/v1/settings/account', input),
  })
}

export function useRuntimeSettings() {
  return useQuery({
    queryKey: RUNTIME_KEY,
    queryFn: () => apiGet<RuntimeSettings>('/api/v1/settings/runtime'),
  })
}

export function useUpdateRuntimeSettings() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (input: RuntimeSettings) =>
      apiPut<RuntimeSettings>('/api/v1/settings/runtime', input),
    onSuccess: (data) => {
      queryClient.setQueryData(RUNTIME_KEY, data)
    },
  })
}
