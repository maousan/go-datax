import { Button } from '@arco-design/web-react'
import type { ButtonProps } from '@arco-design/web-react'
import classNames from 'classnames'
import { forwardRef, type ReactNode } from 'react'
import styles from './style/icon-button.module.less'

type IconButtonProps = ButtonProps & { icon: ReactNode }

const IconButton = forwardRef<HTMLButtonElement, IconButtonProps>(function IconButton(
  { icon, className, ...rest },
  ref,
) {
  return (
    <Button
      ref={ref}
      icon={icon}
      shape="circle"
      type="secondary"
      className={classNames(styles['icon-button'], className)}
      {...rest}
    />
  )
})

export default IconButton
