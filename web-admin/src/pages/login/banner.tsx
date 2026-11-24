import { Carousel } from '@arco-design/web-react'
import bannerImage from '../../assets/login-banner.png'
import styles from './style/index.module.less'

const SLIDES = [
  {
    slogan: '开箱即用的数据同步管理台',
    subSlogan: '统一管理数据源、datax任务与运行记录，覆盖常见 ETL 场景',
  },
  {
    slogan: '支持多种数据源',
    subSlogan: 'MySQL、PostgreSQL、SQLite、SQL Server、Oracle 一站式接入',
  },
  {
    slogan: '运行过程实时可见',
    subSlogan: '基于 SSE 的日志回放与续传，同步进度与错误一目了然',
  },
]

export default function LoginBanner() {
  return (
    <Carousel className={styles.carousel} animation="fade">
      {SLIDES.map((item) => (
        <div key={item.slogan}>
          <div className={styles['carousel-item']}>
            <div className={styles['carousel-title']}>{item.slogan}</div>
            <div className={styles['carousel-sub-title']}>{item.subSlogan}</div>
            <img alt="banner" className={styles['carousel-image']} src={bannerImage} />
          </div>
        </div>
      ))}
    </Carousel>
  )
}
