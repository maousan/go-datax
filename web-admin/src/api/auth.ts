import { useQuery } from '@tanstack/react-query'
import { apiGet, apiPost } from './client'

export interface LoginResponse {
  token: string
  expiresAt: string
}

export interface MeResponse {
  username: string
  expiresAt: string
}

export function login(username: string, password: string): Promise<LoginResponse> {
  return apiPost<LoginResponse>(
    '/api/v1/auth/login',
    { username, password },
    { authenticated: false },
  )
}

export function useMe(enabled: boolean) {
  return useQuery({
    queryKey: ['auth', 'me'],
    queryFn: () => apiGet<MeResponse>('/api/v1/auth/me'),
    enabled,
    retry: false,
  })
}
