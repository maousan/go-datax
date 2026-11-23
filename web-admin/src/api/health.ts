import { useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export interface Health {
  status: string
  version: string
  dataxVersion?: string
}

export function useHealth() {
  return useQuery({
    queryKey: ['health'],
    queryFn: () => apiGet<Health>('/api/v1/health'),
    retry: false,
    staleTime: 30_000,
  })
}
