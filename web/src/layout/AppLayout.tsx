import { DashboardOutlined, DatabaseOutlined, HistoryOutlined, SyncOutlined } from '@ant-design/icons'
import { Layout, Menu, Space, Tag, Typography } from 'antd'
import { Link, Outlet, useLocation } from 'react-router'
import { useHealth } from '../api/health'

const NAV_ITEMS = [
  { key: '/', icon: <DashboardOutlined />, label: <Link to="/">概览</Link> },
  { key: '/data-sources', icon: <DatabaseOutlined />, label: <Link to="/data-sources">数据源</Link> },
  { key: '/sync-jobs', icon: <SyncOutlined />, label: <Link to="/sync-jobs">同步任务</Link> },
  { key: '/runs', icon: <HistoryOutlined />, label: <Link to="/runs">运行历史</Link> },
]

function selectedKey(pathname: string): string {
  const match = NAV_ITEMS.filter((item) => item.key !== '/').find((item) =>
    pathname.startsWith(item.key),
  )
  return match?.key ?? '/'
}

export default function AppLayout() {
  const location = useLocation()
  const health = useHealth()

  const healthTag = health.isError
    ? { color: 'red', text: '服务未连接' }
    : health.isSuccess
      ? { color: 'green', text: `服务 ${health.data?.version ?? ''}` }
      : { color: 'default', text: '服务连接中' }

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Layout.Sider width={220} style={{ background: '#fff' }}>
        <div style={{ padding: '20px 16px', fontSize: 16, fontWeight: 600 }}>go-datax 管理台</div>
        <Menu mode="inline" selectedKeys={[selectedKey(location.pathname)]} items={NAV_ITEMS} />
      </Layout.Sider>

      <Layout>
        <Layout.Header
          style={{
            alignItems: 'center',
            background: '#fff',
            display: 'flex',
            justifyContent: 'space-between',
            padding: '0 24px',
          }}
        >
          <Typography.Title level={4} style={{ margin: 0 }}>
            数据同步管理
          </Typography.Title>
          <Space>
            <Tag color={healthTag.color}>{healthTag.text}</Tag>
          </Space>
        </Layout.Header>

        <Layout.Content style={{ padding: 24 }}>
          <Outlet />
        </Layout.Content>
      </Layout>
    </Layout>
  )
}
