import { EyeOutlined } from '@ant-design/icons'
import { Button } from 'antd'
import { useNavigate } from 'react-router'

export default function RunDetailLink({ runId }: { runId: number }) {
  const navigate = useNavigate()

  return (
    <Button
      type="link"
      size="small"
      icon={<EyeOutlined />}
      onClick={() => navigate(`/runs/${runId}`)}
    >
      详情
    </Button>
  )
}
