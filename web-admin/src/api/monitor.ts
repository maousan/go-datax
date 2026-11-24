import { useQuery } from '@tanstack/react-query'
import { apiGet } from './client'

export interface GoroutineStates {
  running: number
  blocked: number
  waiting: number
  syscall: number
  other: number
}

export interface MonitorSample {
  time: string
  cpuPercent: number
  cores: number
  goroutines: number
  peakGoroutines: number
  states: GoroutineStates
  heapAlloc: number
  heapSys: number
  sys: number
  gcCount: number
  gcPauseTotalMs: number
}

export interface MonitorSnapshot {
  intervalSeconds: number
  retentionSeconds: number
  startedAt: string
  uptimeSeconds: number
  goVersion: string
  current: MonitorSample
  samples: MonitorSample[]
}

/** 轮询进程运行时指标，间隔与服务端采样间隔保持一致。 */
export function useMonitor() {
  return useQuery({
    queryKey: ['monitor'],
    queryFn: () => apiGet<MonitorSnapshot>('/api/v1/monitor'),
    refetchInterval: 10_000,
    staleTime: 0,
  })
}
