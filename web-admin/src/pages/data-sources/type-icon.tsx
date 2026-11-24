import type { DataSourceType } from '../../api/dataSources'
import { typeMeta, withAlpha } from './types'

interface DataSourceTypeIconProps {
  type: DataSourceType
  color?: string
  size?: number
}

export default function DataSourceTypeIcon({
  type,
  color,
  size = 28,
}: DataSourceTypeIconProps) {
  const meta = typeMeta(type)
  const tint = color || meta.brandColor

  return (
    <span
      style={{
        alignItems: 'center',
        backgroundColor: withAlpha(tint, 0.12),
        border: `1px solid ${withAlpha(tint, 0.24)}`,
        borderRadius: Math.max(4, Math.round(size / 4)),
        display: 'inline-flex',
        flexShrink: 0,
        height: size,
        justifyContent: 'center',
        width: size,
      }}
    >
      <img
        src={meta.icon}
        alt=""
        style={{
          height: Math.round(size * 0.62),
          objectFit: 'contain',
          width: Math.round(size * 0.62),
        }}
      />
    </span>
  )
}
