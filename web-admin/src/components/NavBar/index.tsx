import {
  Avatar,
  Dropdown,
  Input,
  Menu,
  Message,
  Space,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import {
  IconLanguage,
  IconMoonFill,
  IconNotification,
  IconPoweroff,
  IconQuestionCircle,
  IconSettings,
  IconSunFill,
} from '@arco-design/web-react/icon'
import { useQueryClient } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router'
import { useMe } from '../../api/auth'
import { useAuth } from '../../auth/AuthContext'
import logo from '../../assets/logo.svg'
import IconButton from './IconButton'
import styles from './style/index.module.less'

const THEME_KEY = 'go-datax:theme'

export default function NavBar() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { token, logout } = useAuth()
  const me = useMe(Boolean(token))
  const [theme, setTheme] = useState(() => window.localStorage.getItem(THEME_KEY) ?? 'light')

  useEffect(() => {
    document.body.setAttribute('arco-theme', theme)
    window.localStorage.setItem(THEME_KEY, theme)
  }, [theme])

  const username = me.data?.username ?? 'admin'

  const handleLogout = () => {
    logout()
    queryClient.clear()
    navigate('/login', { replace: true })
  }

  const handleSearch = (value: string) => {
    const query = value.trim()
    if (query) {
      navigate(`/sync-jobs?q=${encodeURIComponent(query)}`)
    }
  }

  const droplist = (
    <Menu onClickMenuItem={(key) => key === 'logout' && handleLogout()}>
      <Menu.Item key="logout">
        <IconPoweroff className={styles['dropdown-icon']} />
        退出登录
      </Menu.Item>
    </Menu>
  )

  return (
    <div className={styles.navbar}>
      <div className={styles.left}>
        <div className={styles.logo}>
          <img alt="go-datax" className={styles['logo-image']} src={logo} />
          <div className={styles['logo-name']}>go-datax</div>
        </div>
      </div>
      <ul className={styles.right}>
        <li>
          <Input.Search
            className={styles.round}
            placeholder="输入内容查询"
            onPressEnter={(event) => handleSearch((event.target as HTMLInputElement).value)}
          />
        </li>
        <li>
          <Tooltip content="当前仅支持中文">
            <IconButton
              icon={<IconLanguage />}
              onClick={() => Message.info('当前仅支持中文')}
            />
          </Tooltip>
        </li>
        <li>
          <Tooltip content="通知">
            <IconButton
              icon={<IconNotification />}
              onClick={() => Message.info('暂无新通知')}
            />
          </Tooltip>
        </li>
        <li>
          <Tooltip content={theme === 'light' ? '切换到暗色模式' : '切换到亮色模式'}>
            <IconButton
              icon={theme === 'light' ? <IconMoonFill /> : <IconSunFill />}
              onClick={() => setTheme(theme === 'light' ? 'dark' : 'light')}
            />
          </Tooltip>
        </li>
        <li>
          <Tooltip content="设置">
            <IconButton
              icon={<IconSettings />}
              onClick={() => Message.info('主题与布局设置后续开放')}
            />
          </Tooltip>
        </li>
        <li>
          <Tooltip content="帮助">
            <IconButton
              icon={<IconQuestionCircle />}
              onClick={() =>
                window.open('https://github.com/maousan/go-datax#readme', '_blank', 'noopener')
              }
            />
          </Tooltip>
        </li>
        <li>
          <Space size={8}>
            <Typography.Text>{username}</Typography.Text>
            <Dropdown droplist={droplist} position="br">
              <Avatar
                size={32}
                style={{ backgroundColor: 'rgb(var(--arcoblue-6))', cursor: 'pointer' }}
              >
                {username.slice(0, 1).toUpperCase()}
              </Avatar>
            </Dropdown>
          </Space>
        </li>
      </ul>
    </div>
  )
}
