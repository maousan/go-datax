import { Layout } from '@arco-design/web-react'
import classNames from 'classnames'
import styles from './style/index.module.less'

export default function Footer({ className }: { className?: string }) {
  return <Layout.Footer className={classNames(styles.footer, className)}>go-datax 管理台</Layout.Footer>
}
