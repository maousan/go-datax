import {
  Alert,
  Button,
  Card,
  Empty,
  Input,
  Link,
  Message,
  Popconfirm,
  Space,
  Table,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import type { TableColumnProps } from '@arco-design/web-react'
import {
  IconDelete,
  IconEdit,
  IconLink,
  IconPlus,
  IconSearch,
} from '@arco-design/web-react/icon'
import { useEffect, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router'
import { ApiError } from '../../api/client'
import {
  testDataSource,
  useDataSources,
  useDeleteDataSource,
  type ConnectionTestResult,
  type DataSource,
  type DataSourceType,
} from '../../api/dataSources'
import useFillHeight from '../../hooks/useFillHeight'
import listStyles from '../../style/list.module.less'
import { formatTime } from '../../utils/format'
import { describeTestResult, testFailure } from './connection'
import styles from './index.module.less'
import DataSourceTypeIcon from './type-icon'
import { dataSourceColor, typeMeta } from './types'
import DataSourceWizard from './wizard'

export default function DataSourcesPage() {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()
  const [searchInput, setSearchInput] = useState('')
  const [query, setQuery] = useState('')
  const searchTimer = useRef<number | undefined>(undefined)
  const [wizardOpen, setWizardOpen] = useState(searchParams.get('action') === 'create')
  const [wizardSession, setWizardSession] = useState(0)
  const [editing, setEditing] = useState<DataSource | null>(null)
  const [pageTestResult, setPageTestResult] = useState<ConnectionTestResult | null>(null)
  const [pageError, setPageError] = useState('')

  const [tableRef, tableHeight] = useFillHeight(42)
  const listQuery = useDataSources(query)
  const deleteMutation = useDeleteDataSource()

  useEffect(
    () => () => {
      if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    },
    [],
  )

  const applySearch = () => {
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)
    setQuery(searchInput.trim())
  }

  const handleSearchChange = (value: string) => {
    setSearchInput(value)
    if (searchTimer.current !== undefined) window.clearTimeout(searchTimer.current)

    const keyword = value.trim()
    if (!keyword) {
      setQuery('')
      return
    }
    searchTimer.current = window.setTimeout(() => setQuery(keyword), 300)
  }

  const clearAction = () => {
    if (!searchParams.has('action')) return
    const next = new URLSearchParams(searchParams)
    next.delete('action')
    setSearchParams(next, { replace: true })
  }

  const openCreate = () => {
    setEditing(null)
    setWizardSession((value) => value + 1)
    setWizardOpen(true)
  }

  const openEdit = (item: DataSource) => {
    setEditing(item)
    setWizardSession((value) => value + 1)
    setWizardOpen(true)
  }

  const closeWizard = () => {
    setWizardOpen(false)
    setEditing(null)
    clearAction()
  }

  const testSaved = async (item: DataSource) => {
    setPageTestResult(null)
    try {
      const result = await testDataSource({ id: item.id })
      if (result.ok) {
        Message.success('连接成功')
      } else {
        setPageTestResult(result)
      }
    } catch (err) {
      setPageTestResult(testFailure(err))
    }
  }

  const columns: TableColumnProps<DataSource>[] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 130,
      ellipsis: true,
      render: (value: string, item) => (
        <Link onClick={() => navigate(`/data-sources/${item.id}`)}>{value}</Link>
      ),
    },
    {
      title: '类型',
      dataIndex: 'type',
      width: 150,
      render: (value: DataSourceType, item) => (
        <div className={styles['type-cell']}>
          <DataSourceTypeIcon
            type={value}
            color={dataSourceColor(value, item.properties)}
            size={26}
          />
          <span>{typeMeta(value).label}</span>
        </div>
      ),
    },
    { title: '连接地址', dataIndex: 'jdbcUrl', ellipsis: true },
    {
      title: '版本号',
      dataIndex: 'version',
      width: 130,
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      title: '创建时间',
      dataIndex: 'createdAt',
      width: 195,
      render: (value: string) => formatTime(value),
    },
    {
      title: '操作',
      width: 180,
      fixed: 'right',
      render: (_, item) => (
        <Space size={2}>
          <Tooltip content="测试连接">
            <Button
              type="text"
              size="small"
              icon={<IconLink />}
              onClick={() => testSaved(item)}
            />
          </Tooltip>
          <Tooltip content="编辑">
            <Button type="text" size="small" icon={<IconEdit />} onClick={() => openEdit(item)} />
          </Tooltip>
          <Tooltip content="删除">
            <Popconfirm
              content="确认删除该数据源？"
              okText="确定"
              cancelText="取消"
              onOk={async () => {
                setPageError('')
                try {
                  await deleteMutation.mutateAsync(item.id)
                } catch (err) {
                  setPageError(err instanceof ApiError ? err.message : '删除失败，请稍后重试')
                }
              }}
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
      <Typography.Title heading={6}>数据源</Typography.Title>

      <div className={listStyles.toolbar}>
        <Input
          allowClear
          aria-label="数据源名称"
          className={listStyles['search-input']}
          prefix={<IconSearch />}
          placeholder="请输入数据源名称"
          value={searchInput}
          onChange={handleSearchChange}
          onPressEnter={applySearch}
        />

        <div className={listStyles.actions}>
          <Button type="primary" icon={<IconPlus />} onClick={openCreate}>
            新建数据源
          </Button>
        </div>
      </div>

      {listQuery.isError ? (
        <Alert type="error" content="读取数据源失败，请稍后重试" style={{ marginBottom: 16 }} />
      ) : null}

      {pageError ? (
        <Alert
          type="error"
          content={pageError}
          closable
          style={{ marginBottom: 16 }}
          onClose={() => setPageError('')}
        />
      ) : null}

      {pageTestResult ? (
        <Alert
          type={pageTestResult.ok ? 'success' : 'error'}
          content={describeTestResult(pageTestResult)}
          closable
          style={{ marginBottom: 16 }}
          onClose={() => setPageTestResult(null)}
        />
      ) : null}

      <div ref={tableRef} className={listStyles['table-fill']}>
        <Table<DataSource>
          rowKey="id"
          tableLayoutFixed
          columns={columns}
          data={listQuery.data ?? []}
          loading={listQuery.isLoading}
          pagination={false}
          scroll={{ x: 1050, y: tableHeight }}
          noDataElement={<Empty description={query ? '没有匹配的数据源' : '还没有数据源'} />}
        />
      </div>

      <DataSourceWizard
        key={wizardSession}
        visible={wizardOpen}
        editing={editing}
        existingNames={(listQuery.data ?? []).map((item) => item.name)}
        onClose={closeWizard}
      />
    </Card>
  )
}
