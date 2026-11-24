import {
  Alert,
  Button,
  Card,
  Empty,
  Input,
  Popconfirm,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconPlus,
  IconSearch,
} from '@arco-design/web-react/icon'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import {
  useDeleteQueryTemplate,
  useQueryTemplates,
  type QueryTemplate,
} from '../../api/queryTemplates'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'

function errorMessage(err: unknown, fallback: string): string {
  return err instanceof ApiError ? err.message : fallback
}

function sqlPreview(sql: string): string {
  const line = sql
    .split(/\r?\n/)
    .map((item) => item.trim())
    .find(Boolean)
  if (!line) return '-'
  return line.length > 90 ? `${line.slice(0, 90)}...` : line
}

export default function QueryTemplatesPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const query = searchParams.get('q') ?? ''
  const [searchInput, setSearchInput] = useState(query)
  const searchTimer = useRef<number | undefined>(undefined)
  const [pageError, setPageError] = useState('')

  const [tableRef, tableHeight] = useFillHeight(42)
  const templatesQuery = useQueryTemplates(query)
  const deleteMutation = useDeleteQueryTemplate()

  useEffect(
    () => () => {
      if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    },
    [],
  )

  const navigateToSearch = (value: string) => {
    const keyword = value.trim()
    navigate(keyword ? `/datasets?q=${encodeURIComponent(keyword)}` : '/datasets', {
      replace: true,
    })
  }

  const applySearch = () => {
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    navigateToSearch(searchInput)
  }

  const handleSearchChange = (value: string) => {
    setSearchInput(value)
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)

    if (!value.trim()) {
      navigateToSearch('')
      return
    }
    searchTimer.current = window.setTimeout(() => navigateToSearch(value), 300)
  }

  const handleDelete = async (template: QueryTemplate) => {
    setPageError('')
    try {
      await deleteMutation.mutateAsync(template.id)
    } catch (err) {
      setPageError(errorMessage(err, '删除数据集失败'))
    }
  }

  const columns: TableColumnProps<QueryTemplate>[] = [
    { title: '名称', dataIndex: 'name', width: 180, ellipsis: true },
    {
      title: '数据源',
      dataIndex: 'dataSourceName',
      width: 150,
      ellipsis: true,
    },
    {
      title: '来源',
      width: 120,
      render: (_, template) =>
        template.sql ? <Tag color="arcoblue">SQL 数据集</Tag> : template.table || '-',
    },
    {
      title: '输出列',
      dataIndex: 'columns',
      width: 220,
      ellipsis: true,
      render: (value: string[]) => value.join(', '),
    },
    {
      title: 'SQL',
      width: 280,
      ellipsis: true,
      render: (_, template) =>
        template.sql ? (
          <Tooltip content={<pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{template.sql}</pre>}>
            <span>{sqlPreview(template.sql)}</span>
          </Tooltip>
        ) : (
          '-'
        ),
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 175,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 100,
      fixed: 'right',
      render: (_, template) => (
        <Space size={2}>
          <Tooltip content="编辑">
            <Button
              type="text"
              size="small"
              icon={<IconEdit />}
              onClick={() => navigate(`/datasets/${template.id}/edit`)}
            />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该数据集？"
              okText="确定"
              cancelText="取消"
              onOk={() => handleDelete(template)}
            >
              <Button type="text" status="danger" size="small" icon={<IconDelete />} />
            </Popconfirm>
          </Tooltip>
        </Space>
      ),
    },
  ]

  return (
    <Card className={listStyles.page}>
      <Typography.Title heading={6}>数据集</Typography.Title>

      <div className={listStyles.toolbar}>
        <Input
          allowClear
          aria-label="数据集名称"
          className={listStyles['search-input']}
          prefix={<IconSearch />}
          placeholder="请输入数据集名称"
          value={searchInput}
          onChange={handleSearchChange}
          onPressEnter={applySearch}
        />

        <div className={listStyles.actions}>
          <Button
            type="primary"
            icon={<IconPlus />}
            onClick={() => navigate('/datasets/new')}
          >
            新建数据集
          </Button>
        </div>
      </div>

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          onClose={() => setPageError('')}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<QueryTemplate>
          rowKey="id"
          tableLayoutFixed
          scroll={{ x: 1300, y: tableHeight }}
          columns={columns}
          data={templatesQuery.data ?? []}
          loading={templatesQuery.isLoading}
          pagination={false}
          noDataElement={<Empty description={query ? '没有匹配的数据集' : '还没有数据集'} />}
        />
      </div>
    </Card>
  )
}
