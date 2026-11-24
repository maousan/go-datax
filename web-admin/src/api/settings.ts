import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { apiGet, apiPut } from './client'

const RUNTIME_KEY = ['settings', 'runtime'] as const

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
