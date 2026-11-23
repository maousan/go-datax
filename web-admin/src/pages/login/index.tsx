import { Navigate, useLocation } from 'react-router'
import logo from '../../assets/logo.svg'
import Footer from '../../components/Footer'
import { useAuth } from '../../auth/AuthContext'
import LoginBanner from './banner'
import LoginForm from './form'
import styles from './style/index.module.less'

export default function LoginPage() {
  const { token } = useAuth()
  const location = useLocation()
  const from = (location.state as { from?: string } | null)?.from ?? '/'

  if (token) {
    return <Navigate to={from} replace />
  }

  return (
    <div className={styles.container}>
      <div className={styles.banner}>
        <div className={styles['banner-inner']}>
          <LoginBanner />
        </div>
      </div>

      <div className={styles.logo}>
        <img alt="go-datax" className={styles['logo-image']} src={logo} />
        <div className={styles['logo-text']}>go-datax</div>
      </div>

      <div className={styles.content}>
        <div className={styles['content-inner']}>
          <LoginForm from={from} />
        </div>
        <div className={styles.footer}>
          <Footer />
        </div>
      </div>
    </div>
  )
}
