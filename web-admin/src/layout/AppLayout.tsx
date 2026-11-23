import { Breadcrumb, Layout, Menu } from '@arco-design/web-react'
import { IconHome, IconMenuFold, IconMenuUnfold } from '@arco-design/web-react/icon'
import { useMemo, useState } from 'react'
import { Outlet, useLocation, useNavigate } from 'react-router'
import NavBar from '../components/NavBar'
import {
  breadcrumbFor,
  menuRoutes,
  openMenuKeys,
  selectedMenuKey,
  type MenuRoute,
} from '../routes'
import styles from '../style/layout.module.less'

const Sider = Layout.Sider
const Content = Layout.Content
const MenuItem = Menu.Item
const SubMenu = Menu.SubMenu

function renderMenuRoutes(routes: MenuRoute[]) {
  return routes.map((route) => {
    if (route.children?.length) {
      const Icon = route.icon
      return (
        <SubMenu
          key={route.key}
          title={
            <>
              {Icon ? <Icon className={styles.icon} /> : null} {route.name}
            </>
          }
        >
          {renderMenuRoutes(route.children)}
        </SubMenu>
      )
    }
    return <MenuItem key={route.path ?? route.key}>{route.name}</MenuItem>
  })
}

export default function AppLayout() {
  const location = useLocation()
  const navigate = useNavigate()
  const [collapsed, setCollapsed] = useState(false)
  const [openKeys, setOpenKeys] = useState<string[]>(openMenuKeys(location.pathname))

  const menuWidth = collapsed ? 48 : 220
  const visibleOpenKeys = useMemo(
    () => Array.from(new Set([...openKeys, ...openMenuKeys(location.pathname)])),
    [openKeys, location.pathname],
  )

  return (
    <Layout className={styles.layout}>
      <div className={styles['layout-navbar']}>
        <NavBar />
      </div>

      <Layout>
        <Sider
          className={styles['layout-sider']}
          width={menuWidth}
          collapsed={collapsed}
          onCollapse={setCollapsed}
          trigger={null}
          collapsible
          breakpoint="xl"
          style={{ paddingTop: 60 }}
        >
          <div className={styles['menu-wrapper']}>
            <Menu
              collapse={collapsed}
              selectedKeys={[selectedMenuKey(location.pathname)]}
              openKeys={visibleOpenKeys}
              onClickMenuItem={(key) => navigate(key)}
              onClickSubMenu={(_, keys) => setOpenKeys(keys)}
            >
              {renderMenuRoutes(menuRoutes)}
            </Menu>
          </div>
          <div className={styles['collapse-btn']} onClick={() => setCollapsed(!collapsed)}>
            {collapsed ? <IconMenuUnfold /> : <IconMenuFold />}
          </div>
        </Sider>

        <Layout
          className={styles['layout-content']}
          style={{ paddingLeft: menuWidth, paddingTop: 60 }}
        >
          <div className={styles['layout-content-wrapper']}>
            <div className={styles['layout-breadcrumb']}>
              <Breadcrumb>
                <Breadcrumb.Item key="home">
                  <IconHome />
                </Breadcrumb.Item>
                {breadcrumbFor(location.pathname).map((item) => (
                  <Breadcrumb.Item key={item}>{item}</Breadcrumb.Item>
                ))}
              </Breadcrumb>
            </div>
            <Content>
              <Outlet />
            </Content>
          </div>
        </Layout>
      </Layout>
    </Layout>
  )
}
