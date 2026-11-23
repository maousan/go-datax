import { Card, Empty } from 'antd'

interface PlaceholderCardProps {
  title: string
  emptyText: string
}

export default function PlaceholderCard({ title, emptyText }: PlaceholderCardProps) {
  return (
    <Card title={title}>
      <Empty description={emptyText} />
    </Card>
  )
}
