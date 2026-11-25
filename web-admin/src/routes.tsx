import {
  IconDashboard,
  IconHistory,
  IconNotification,
  IconSettings,
  IconStorage,
  IconSwap,
} from '@arco-design/web-react/icon'
import type { ComponentType } from 'react'

export interface MenuRoute {
  key: string
  name: string
  icon?: ComponentType<{ className?: string }>
  path?: string
  children?: MenuRoute[]
}

export const menuRoutes: MenuRoute[] = [
  {
    key: 'dashboard',
    name: '仪表盘',
    icon: IconDashboard,
    children: [{ key: 'workplace', name: '工作台', path: '/' }],
  },
  {
    key: 'data',
    name: '数据管理',
    icon: IconStorage,
    children: [
      { key: 'data-sources', name: '数据源', path: '/data-sources' },
      { key: 'datasets', name: '数据集', path: '/datasets' },
      { key: 'sync-jobs', name: 'datax任务', path: '/sync-jobs' },
      { key: 'collect-tasks', name: '采集任务', path: '/collect-tasks' },
    ],
  },
  {
    key: 'etl',
    name: 'ETL',
    icon: IconSwap,
    children: [{ key: 'sources', name: '源与目标', path: '/sources' }],
  },
  {
    key: 'operations',
    name: '运行中心',
    icon: IconHistory,
    children: [
      { key: 'run-history', name: '运行历史', path: '/runs' },
      { key: 'service-monitor', name: '服务监控', path: '/monitor' },
    ],
  },
  {
    key: 'alerts',
    name: '告警管理',
    icon: IconNotification,
    children: [
      { key: 'alert-config', name: '告警配置', path: '/alerts/config' },
      { key: 'alert-logs', name: '告警日志', path: '/alerts/logs' },
    ],
  },
  {
    key: 'settings',
    name: '系统设置',
    icon: IconSettings,
    path: '/settings',
  },
]

export function flattenMenuRoutes(routes: MenuRoute[] = menuRoutes): MenuRoute[] {
  const result: MenuRoute[] = []
  const travel = (items: MenuRoute[]) => {
    items.forEach((item) => {
      if (item.path) result.push(item)
      if (item.children) travel(item.children)
    })
  }
  travel(routes)
  return result
}

export function selectedMenuKey(pathname: string): string {
  const matches = flattenMenuRoutes()
    .filter((route) => route.path && route.path !== '/' && pathname.startsWith(route.path))
    .sort((a, b) => (b.path?.length ?? 0) - (a.path?.length ?? 0))
  return matches[0]?.path ?? '/'
}

export function openMenuKeys(pathname: string): string[] {
  if (pathname === '/') return ['dashboard']
  if (pathname.startsWith('/data-sources') || pathname.startsWith('/datasets') || pathname.startsWith('/sync-jobs') || pathname.startsWith('/collect-tasks')) return ['data']
  if (pathname.startsWith('/sources')) return ['etl']
  if (pathname.startsWith('/runs') || pathname.startsWith('/monitor')) return ['operations']
  if (pathname.startsWith('/alerts')) return ['alerts']
  return []
}

function findGroup(pathname: string, routes: MenuRoute[] = menuRoutes): MenuRoute[] {
  for (const route of routes) {
    if (route.children?.some((child) => child.path === pathname)) return [route]
    if (route.children) {
      const nested = findGroup(pathname, route.children)
      if (nested.length) return [route, ...nested]
    }
  }
  return []
}

export function breadcrumbFor(pathname: string): string[] {
  if (pathname.startsWith('/datasets/')) {
    return ['数据管理', '数据集', pathname.endsWith('/edit') ? '编辑数据集' : '新建数据集']
  }
  if (pathname.startsWith('/data-sources/')) return ['数据管理', '数据源', '数据源详情']
  if (pathname.startsWith('/sync-jobs/')) return ['数据管理', 'datax任务', '任务详情']
  if (pathname.startsWith('/runs/')) return ['运行中心', '运行历史', '运行详情']
  const groups = findGroup(pathname)
  const leaf = flattenMenuRoutes().find((route) => route.path === pathname)
  const labels = groups.map((group) => group.name)
  if (leaf) labels.push(leaf.name)
  return labels.length ? labels : ['仪表盘', '工作台']
}
