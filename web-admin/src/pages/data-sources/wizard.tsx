import {
  Alert,
  Button,
  Empty,
  Form,
  Input,
  Message,
  Modal,
  Select,
  Tabs,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import {
  IconArrowRight,
  IconApps,
  IconCaretDown,
  IconCaretRight,
  IconCheck,
  IconDelete,
  IconEdit,
  IconLeft,
  IconList,
  IconPalette,
  IconPlus,
  IconSearch,
  IconStorage,
  IconSync,
  IconThunderbolt,
} from '@arco-design/web-react/icon'
import classNames from 'classnames'
import { useMemo, useState } from 'react'
import { ApiError } from '../../api/client'
import {
  testDataSource,
  useCreateDataSource,
  useUpdateDataSource,
  type ConnectionTestResult,
  type DataSource,
  type DataSourceInput,
  type DataSourceType,
} from '../../api/dataSources'
import {
  buildJdbcUrl,
  describeTestResult,
  parseJdbcUrl,
  testFailure,
  validateConnectionParams,
  type Charset,
  type ConnectionFields,
  type ConnectionParam,
  type SslMode,
} from './connection'
import type { DataSourceCategory } from './types'
import {
  DATA_SOURCE_TYPES,
  TYPE_CATEGORIES,
  UI_COLOR_KEY,
  typeMeta,
} from './types'
import DataSourceTypeIcon from './type-icon'
import styles from './wizard.module.less'

interface DataSourceWizardProps {
  visible: boolean
  editing: DataSource | null
  existingNames: string[]
  onClose: () => void
}

interface WizardFormValues {
  name: string
  host: string
  port: string
  database: string
  path: string
  username: string
  password: string
  urlDraft: string
}

const COLOR_OPTIONS = ['#86909c', '#00b42a', '#ff7d00', '#f77234', '#f53f3f', '#165dff', '#722ed1']

const CHARSET_OPTIONS = [
  { value: '', label: '默认' },
  { value: 'utf8mb4', label: 'utf8mb4（完整 Unicode）' },
  { value: 'gbk', label: 'GBK' },
  { value: 'latin1', label: 'latin1' },
]

const SSL_OPTIONS: Array<{ value: SslMode; label: string }> = [
  { value: 'default', label: '默认（不指定）' },
  { value: 'disable', label: '禁用 SSL' },
  { value: 'require', label: '启用（不校验证书）' },
  { value: 'verify', label: '启用（校验证书）' },
]

function emptyForm(type: DataSourceType): WizardFormValues {
  const meta = typeMeta(type)
  return {
    name: '',
    host: type === 'sqlite' ? '' : '127.0.0.1',
    port: meta.defaultPort,
    database: '',
    path: '',
    username: '',
    password: '',
    urlDraft: '',
  }
}

function uniqueName(base: string, existing: string[], current?: string): string {
  const trimmed = base.replace(/\s+/g, '-').slice(0, 100) || '数据源'
  const taken = new Set(
    existing
      .filter((name) => name.toLowerCase() !== (current ?? '').toLowerCase())
      .map((name) => name.toLowerCase()),
  )
  if (!taken.has(trimmed.toLowerCase())) return trimmed

  let index = 2
  while (taken.has(`${trimmed}-${index}`.toLowerCase())) index += 1
  return `${trimmed}-${index}`.slice(0, 100)
}

function autoName(
  type: DataSourceType,
  values: WizardFormValues,
  existing: string[],
  current?: string,
): string {
  const meta = typeMeta(type)
  let suffix = values.host.trim()
  if (type === 'sqlite') {
    const file = values.path.trim().split('?')[0].split(/[\\/]/).filter(Boolean).pop() ?? ''
    suffix = file
  }
  const base = suffix ? `${meta.label}-${suffix}` : meta.label
  return uniqueName(base, existing, current)
}

function errorMessage(err: unknown, fallback: string): string {
  if (err instanceof ApiError) {
    if (err.code === 'duplicate_name') return '数据源名称已存在'
    return err.message
  }
  return fallback
}

export default function DataSourceWizard({
  visible,
  editing,
  existingNames,
  onClose,
}: DataSourceWizardProps) {
  const [form] = Form.useForm<WizardFormValues>()
  const [initial] = useState<{ parsed: ConnectionFields | null; values: WizardFormValues }>(() => {
    if (!editing) {
      return { parsed: null, values: emptyForm('mysql') }
    }
    const editingMeta = typeMeta(editing.type)
    const parsed = parseJdbcUrl(editing.type, editing.jdbcUrl)
    return {
      parsed,
      values: {
        name: editing.name,
        host: parsed?.host ?? '',
        port: parsed?.port || editingMeta.defaultPort,
        database: parsed?.database ?? '',
        path: parsed?.path ?? '',
        username: editing.username ?? '',
        password: '',
        urlDraft: '',
      },
    }
  })
  const [step, setStep] = useState(editing ? 2 : 1)
  const [category, setCategory] = useState<'all' | DataSourceCategory>('all')
  const [search, setSearch] = useState('')
  const [viewMode, setViewMode] = useState<'grid' | 'list'>('grid')
  const [selectedType, setSelectedType] = useState<DataSourceType>(editing?.type ?? 'mysql')
  const [color, setColor] = useState(
    editing
      ? editing.properties?.[UI_COLOR_KEY] || typeMeta(editing.type).brandColor
      : typeMeta('mysql').brandColor,
  )
  const [charset, setCharset] = useState<Charset>(initial.parsed?.charset ?? '')
  const [sslMode, setSslMode] = useState<SslMode>(initial.parsed?.sslMode ?? 'default')
  const [params, setParams] = useState<ConnectionParam[]>(initial.parsed?.params ?? [])
  const [rawUrl, setRawUrl] = useState<string | null>(
    editing && !initial.parsed ? editing.jdbcUrl : null,
  )
  const [urlError, setUrlError] = useState('')
  const [formError, setFormError] = useState('')
  const [testResult, setTestResult] = useState<ConnectionTestResult | null>(null)
  const [testing, setTesting] = useState(false)
  const [activeTab, setActiveTab] = useState('basic')
  const [infoOpen, setInfoOpen] = useState(false)
  const [base, setBase] = useState<WizardFormValues>(initial.values)

  const createMutation = useCreateDataSource()
  const updateMutation = useUpdateDataSource()
  const saving = createMutation.isPending || updateMutation.isPending
  const meta = typeMeta(selectedType)

  const visibleTypes = useMemo(() => {
    const keyword = search.trim().toLowerCase()
    return DATA_SOURCE_TYPES.filter((item) => {
      if (category !== 'all' && item.category !== category) return false
      if (!keyword) return true
      return item.label.toLowerCase().includes(keyword) || item.value.includes(keyword)
    })
  }, [category, search])

  const toConnectionFields = (values: WizardFormValues): ConnectionFields => ({
    host: values.host ?? '',
    port: values.port ?? '',
    database: values.database ?? '',
    path: values.path ?? '',
    params,
    charset,
    sslMode,
  })

  const previewUrl =
    rawUrl !== null ? rawUrl.trim() : buildJdbcUrl(selectedType, toConnectionFields(base))

  const customParamCount = params.filter((item) => item.key.trim() || item.value.trim()).length

  const handleSelectType = (nextType: DataSourceType) => {
    const previousBrand = typeMeta(selectedType).brandColor
    setSelectedType(nextType)
    if (color === previousBrand) setColor(typeMeta(nextType).brandColor)
  }

  const handleCategoryChange = (nextCategory: 'all' | DataSourceCategory) => {
    setCategory(nextCategory)
    const keyword = search.trim().toLowerCase()
    const candidates = DATA_SOURCE_TYPES.filter((item) => {
      if (nextCategory !== 'all' && item.category !== nextCategory) return false
      if (!keyword) return true
      return item.label.toLowerCase().includes(keyword) || item.value.includes(keyword)
    })
    if (candidates.length > 0 && !candidates.some((item) => item.value === selectedType)) {
      handleSelectType(candidates[0].value)
    }
  }

  const handleNext = () => {
    const currentPort = String(form.getFieldValue('port') ?? '')
    const standardPorts = DATA_SOURCE_TYPES.map((item) => item.defaultPort).filter(Boolean)
    if (selectedType !== 'sqlite' && (!currentPort || standardPorts.includes(currentPort))) {
      form.setFieldValue('port', meta.defaultPort)
      setBase((previous) => ({ ...previous, port: meta.defaultPort }))
    }

    if (editing && selectedType !== editing.type) {
      const oldBrand = typeMeta(editing.type).brandColor
      if (color === oldBrand) setColor(meta.brandColor)
      setRawUrl(null)
      setUrlError('')
    }
    setStep(2)
  }

  const parseUrlDraft = (): boolean => {
    const draft = String(form.getFieldValue('urlDraft') ?? '').trim()
    if (!draft) {
      setUrlError('')
      return true
    }

    const parsed = parseJdbcUrl(selectedType, draft)
    if (!parsed) {
      setUrlError('无法解析该 JDBC URL，请检查格式，或直接在下方填写连接信息')
      return false
    }

    const current = form.getFieldsValue() as WizardFormValues
    const next: WizardFormValues = {
      ...current,
      host: parsed.host,
      port: parsed.port || meta.defaultPort,
      database: parsed.database,
      path: parsed.path,
      urlDraft: '',
    }
    form.setFieldsValue(next)
    setBase(next)
    setCharset(parsed.charset)
    setSslMode(parsed.sslMode)
    setParams(parsed.params)
    setUrlError('')
    Message.success('已从 URL 解析连接信息')
    return true
  }

  const exitRawMode = () => {
    const values = form.getFieldsValue() as WizardFormValues
    const next: WizardFormValues = {
      ...emptyForm(selectedType),
      name: values.name,
      username: values.username,
      password: values.password,
    }
    form.setFieldsValue(next)
    setBase(next)
    setRawUrl(null)
    setUrlError('')
    setCharset('')
    setSslMode('default')
    setParams([])
    setActiveTab('basic')
    Message.info('已切换为表单填写，请补充连接信息')
  }

  const validateForm = async (): Promise<WizardFormValues | null> => {
    setFormError('')
    setTestResult(null)

    if (rawUrl !== null) {
      const values = form.getFieldsValue() as WizardFormValues
      if (!rawUrl.trim()) {
        setFormError('请输入 JDBC URL')
        return null
      }
      if (selectedType !== 'sqlite' && !String(values.username ?? '').trim()) {
        setFormError('请输入用户名')
        return null
      }
      return values
    }

    const paramError = validateConnectionParams(selectedType, params)
    if (paramError) {
      setFormError(paramError)
      setActiveTab('advanced')
      return null
    }

    const draft = String(form.getFieldValue('urlDraft') ?? '').trim()
    if (draft) {
      if (!parseUrlDraft()) return null
      await Promise.resolve()
    }

    try {
      const values = (await form.validate()) as WizardFormValues
      if (selectedType === 'mssql' && /[;=]/.test(String(values.database ?? ''))) {
        setFormError('SQL Server 数据库名不能包含分号或等号')
        setActiveTab('basic')
        return null
      }
      return values
    } catch {
      setActiveTab('basic')
      return null
    }
  }

  const resolveUrl = (values: WizardFormValues): string =>
    rawUrl !== null ? rawUrl.trim() : buildJdbcUrl(selectedType, toConnectionFields(values))

  const resolveInput = (values: WizardFormValues, url: string): DataSourceInput => {
    const label = autoName(selectedType, values, existingNames, editing?.name)
    const name = values.name?.trim() || label
    return {
      name,
      type: selectedType,
      jdbcUrl: url,
      username: selectedType === 'sqlite' ? '' : String(values.username ?? '').trim(),
      password: selectedType === 'sqlite' ? '' : values.password ?? '',
      properties: {
        ...(editing?.properties ?? {}),
        [UI_COLOR_KEY]: color,
      },
    }
  }

  const handleTest = async () => {
    const values = await validateForm()
    if (!values) return

    const url = resolveUrl(values)
    if (!url) {
      setFormError('请填写完整的连接信息')
      setActiveTab('basic')
      return
    }
    if (url.length > 500) {
      setFormError('连接地址不能超过 500 个字符')
      setActiveTab('basic')
      return
    }

    const input = resolveInput(values, url)
    setTesting(true)
    setTestResult(null)
    try {
      const body = editing ? { id: editing.id, ...input } : input
      const result = await testDataSource(body)
      setTestResult(result)
      if (result.ok) {
        Message.success('连接成功')
      }
    } catch (err) {
      setTestResult(testFailure(err))
    } finally {
      setTesting(false)
    }
  }

  const handleSave = async () => {
    const values = await validateForm()
    if (!values) return

    const url = resolveUrl(values)
    if (!url) {
      setFormError('请填写完整的连接信息')
      setActiveTab('basic')
      return
    }
    if (url.length > 500) {
      setFormError('连接地址不能超过 500 个字符')
      setActiveTab('basic')
      return
    }

    const input = resolveInput(values, url)
    try {
      if (editing) {
        await updateMutation.mutateAsync({ id: editing.id, input })
      } else {
        await createMutation.mutateAsync(input)
      }
      Message.success(editing ? '数据源已更新' : '数据源已创建')
      onClose()
    } catch (err) {
      setFormError(errorMessage(err, '保存失败，请稍后重试'))
    }
  }

  const updateParam = (index: number, field: 'key' | 'value', value: string) => {
    setParams((previous) =>
      previous.map((item, itemIndex) =>
        itemIndex === index ? { ...item, [field]: value } : item,
      ),
    )
  }

  const renderTypeStep = () => (
    <>
      <div className={styles['type-step']}>
        <aside className={styles.rail}>
          {TYPE_CATEGORIES.map((item) => {
            const count =
              item.value === 'all'
                ? DATA_SOURCE_TYPES.length
                : DATA_SOURCE_TYPES.filter((type) => type.category === item.value).length
            return (
              <button
                key={item.value}
                type="button"
                className={classNames(
                  styles['rail-item'],
                  category === item.value && styles['rail-active'],
                )}
                onClick={() => handleCategoryChange(item.value)}
              >
                <span>{item.label}</span>
                <span className={styles['rail-count']}>{count}</span>
              </button>
            )
          })}
        </aside>

        <section className={styles.picker}>
          <div className={styles['picker-head']}>
            <Input
              allowClear
              prefix={<IconSearch />}
              placeholder="搜索数据库类型"
              className={styles.search}
              value={search}
              onChange={setSearch}
            />
            <div className={styles['view-toggle']}>
              <Tooltip content="网格视图">
                <button
                  type="button"
                  aria-label="网格视图"
                  className={classNames(
                    styles['view-button'],
                    viewMode === 'grid' && styles['view-button-active'],
                  )}
                  onClick={() => setViewMode('grid')}
                >
                  <IconApps />
                </button>
              </Tooltip>
              <Tooltip content="列表视图">
                <button
                  type="button"
                  aria-label="列表视图"
                  className={classNames(
                    styles['view-button'],
                    viewMode === 'list' && styles['view-button-active'],
                  )}
                  onClick={() => setViewMode('list')}
                >
                  <IconList />
                </button>
              </Tooltip>
            </div>
          </div>

          <div
            className={classNames(styles.cards, viewMode === 'list' && styles['cards-list'])}
          >
            {visibleTypes.map((item) => (
              <button
                key={item.value}
                type="button"
                className={classNames(
                  styles.card,
                  viewMode === 'list' && styles['card-list'],
                  selectedType === item.value && styles['card-selected'],
                )}
                onClick={() => handleSelectType(item.value)}
              >
                {selectedType === item.value ? (
                  <span className={styles.check}>
                    <IconCheck />
                  </span>
                ) : null}
                <span className={styles['card-icon']}>
                  <img src={item.icon} alt="" />
                </span>
                <span className={styles['card-label']}>{item.label}</span>
              </button>
            ))}
            {visibleTypes.length === 0 ? (
              <div className={styles['cards-empty']}>
                <Empty description="没有匹配的数据库类型" />
              </div>
            ) : null}
          </div>
        </section>
      </div>

      <div className={styles.footer}>
        <div className={styles.selected}>
          <DataSourceTypeIcon type={selectedType} color={color} size={26} />
          <span>
            已选择：<strong>{typeMeta(selectedType).label}</strong>
          </span>
        </div>
        <div className={styles.actions}>
          <Button onClick={onClose}>取消</Button>
          <Button type="primary" onClick={handleNext}>
            下一步
            <IconArrowRight style={{ marginLeft: 6 }} />
          </Button>
        </div>
      </div>
    </>
  )

  const renderBaseFields = () => (
    <>
      <Form.Item label="名称" field="name">
        <Input placeholder="连接名称，留空则自动生成" maxLength={100} />
      </Form.Item>

      <Form.Item label="类型">
        <div className={styles['type-value']}>
          <DataSourceTypeIcon type={selectedType} color={color} size={22} />
          <span>{meta.label}</span>
          <Tooltip content="重新选择类型">
            <Button
              type="text"
              size="mini"
              icon={<IconEdit />}
              onClick={() => setStep(1)}
            />
          </Tooltip>
        </div>
      </Form.Item>

      <Form.Item label="颜色">
        <div className={styles.colors}>
          {COLOR_OPTIONS.map((option) => (
            <button
              key={option}
              type="button"
              aria-label={`颜色 ${option}`}
              className={classNames(
                styles.color,
                color.toLowerCase() === option.toLowerCase() && styles['color-active'],
              )}
              style={{ backgroundColor: option }}
              onClick={() => setColor(option)}
            />
          ))}
          <label
            className={classNames(
              styles['color-custom'],
              !COLOR_OPTIONS.some((option) => option.toLowerCase() === color.toLowerCase()) &&
                styles['color-active'],
            )}
            style={{ backgroundColor: color }}
            title="自定义颜色"
          >
            <span className={styles['color-custom-icon']}>
              <IconPalette />
            </span>
            <input
              type="color"
              value={/^#[0-9a-f]{6}$/i.test(color) ? color : meta.brandColor}
              onChange={(event) => setColor(event.target.value)}
            />
          </label>
        </div>
      </Form.Item>
    </>
  )

  const renderCredentials = () => {
    if (selectedType === 'sqlite') return null
    return (
      <>
        <Form.Item
          label="用户名"
          field="username"
          rules={[{ required: true, message: '请输入用户名' }]}
        >
          <Input placeholder="数据库用户名" maxLength={200} />
        </Form.Item>
        <Form.Item
          label="密码"
          field="password"
          extra={editing ? '留空表示不修改已保存的密码' : undefined}
        >
          <Input.Password placeholder="数据库密码" maxLength={500} />
        </Form.Item>
      </>
    )
  }

  const renderDatabaseInfo = () => (
    <Form.Item label="数据库信息">
      <div className={styles['db-info']}>
        <button
          type="button"
          className={styles['db-summary']}
          aria-expanded={infoOpen}
          onClick={() => setInfoOpen((previous) => !previous)}
        >
          <span className={styles['db-summary-icon']}>
            <IconStorage />
          </span>
          <span className={styles['db-summary-label']}>当前配置</span>
          <span className={styles['db-summary-value']}>{meta.label}</span>
          {infoOpen ? (
            <IconCaretDown className={styles['db-summary-caret']} />
          ) : (
            <IconCaretRight className={styles['db-summary-caret']} />
          )}
        </button>
        {infoOpen ? (
          <div className={styles['db-detail']}>
            <div className={styles['db-detail-row']}>
              <span className={styles['db-detail-label']}>连接地址</span>
              {previewUrl ? (
                <Typography.Text
                  code
                  copyable={{ text: previewUrl }}
                  className={styles['db-info-url']}
                >
                  {previewUrl}
                </Typography.Text>
              ) : (
                <Typography.Text type="secondary">填写连接信息后自动生成</Typography.Text>
              )}
            </div>
            {testResult?.version ? (
              <div className={styles['db-detail-row']}>
                <span className={styles['db-detail-label']}>数据库版本</span>
                <span>{testResult.version}</span>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </Form.Item>
  )

  const renderBasicFields = () => (
    <>
      <Form.Item
        label="URL（可选）"
        field="urlDraft"
        extra={
          urlError ? (
            <span className={styles['error-text']}>{urlError}</span>
          ) : (
            '粘贴 JDBC URL 可自动解析并填充下方连接信息'
          )
        }
      >
        <Input
          allowClear
          placeholder={previewUrl || meta.urlPlaceholder}
          onBlur={parseUrlDraft}
          onPressEnter={parseUrlDraft}
          suffix={
            <Tooltip content="解析 URL 并填充表单">
              <Button
                type="text"
                size="mini"
                icon={<IconThunderbolt />}
                onClick={(event) => {
                  event.preventDefault()
                  parseUrlDraft()
                }}
              />
            </Tooltip>
          }
        />
      </Form.Item>

      {renderBaseFields()}

      {selectedType === 'sqlite' ? (
        <Form.Item
          label={meta.databaseLabel}
          field="path"
          rules={[{ required: true, message: '请输入数据库文件路径' }]}
        >
          <Input placeholder={meta.databasePlaceholder} maxLength={500} />
        </Form.Item>
      ) : (
        <>
          <Form.Item label="主机" required>
            <div className={styles['host-inputs']}>
              <Form.Item
                field="host"
                noStyle={{ showErrorTip: true }}
                rules={[{ required: true, message: '请输入主机地址' }]}
              >
                <Input placeholder="127.0.0.1" maxLength={255} />
              </Form.Item>
              <Form.Item
                field="port"
                noStyle={{ showErrorTip: true }}
                rules={[
                  { required: true, message: '请输入端口' },
                  {
                    validator: (value, callback) => {
                      const text = String(value ?? '').trim()
                      if (!text) {
                        callback()
                        return
                      }
                      const port = Number(text)
                      if (!Number.isInteger(port) || port < 1 || port > 65535) {
                        callback('端口必须是 1-65535 的整数')
                      } else {
                        callback()
                      }
                    },
                  },
                ]}
              >
                <Input placeholder={meta.defaultPort} maxLength={5} />
              </Form.Item>
            </div>
          </Form.Item>

          {renderCredentials()}

          <Form.Item
            label={meta.databaseLabel}
            field="database"
            rules={
              selectedType === 'oracle' ? [{ required: true, message: '请输入服务名' }] : []
            }
          >
            <Input placeholder={meta.databasePlaceholder} maxLength={200} />
          </Form.Item>
        </>
      )}

      {selectedType === 'mysql' ? (
        <Form.Item
          label="数据字符集"
          extra="用于指定 JDBC 文本编码，utf8mb4 可完整支持 emoji 和生僻字。"
        >
          <Select
            value={charset}
            options={CHARSET_OPTIONS}
            style={{ maxWidth: 320 }}
            onChange={(value) => setCharset(value as Charset)}
          />
        </Form.Item>
      ) : null}

      {selectedType !== 'sqlite' ? (
        <Form.Item label="连接参数">
          <div className={styles['params-summary']}>
            <span>{customParamCount ? `${customParamCount} 项自定义参数` : '默认'}</span>
            <Button type="text" size="small" onClick={() => setActiveTab('advanced')}>
              配置...
            </Button>
          </div>
        </Form.Item>
      ) : null}

      {renderDatabaseInfo()}
    </>
  )

  const renderRawFields = () => (
    <>
      <Alert
        type="warning"
        className={styles['raw-alert']}
        content={
          <div className={styles['raw-alert-content']}>
            <span>该数据源使用非标准 JDBC URL，当前按原文保存。</span>
            <Button size="mini" onClick={exitRawMode}>
              改用表单填写
            </Button>
          </div>
        }
      />
      <Form.Item label="JDBC URL" required extra="将按原样保存">
        <Input
          value={rawUrl ?? ''}
          placeholder={meta.urlPlaceholder}
          onChange={(value) => setRawUrl(value)}
        />
      </Form.Item>

      {renderBaseFields()}
      {renderCredentials()}
      {renderDatabaseInfo()}
    </>
  )

  const supportsSSL =
    selectedType === 'mysql' || selectedType === 'postgres' || selectedType === 'mssql'

  const renderSSL = () => (
    <>
      <Alert
        type="info"
        content="TLS/SSL 设置会写入 JDBC 连接地址，由数据库驱动执行。"
        style={{ marginBottom: 16 }}
      />
      {supportsSSL ? (
        <Form.Item label="SSL 模式">
          <Select
            value={sslMode}
            options={SSL_OPTIONS}
            style={{ maxWidth: 360 }}
            onChange={(value) => setSslMode(value as SslMode)}
          />
        </Form.Item>
      ) : (
        <Alert
          type="warning"
          content={`${meta.label} 请直接在“高级”的连接参数中配置 SSL。`}
        />
      )}
    </>
  )

  const renderAdvanced = () => {
    if (selectedType === 'sqlite') {
      return <Alert type="info" content="SQLite 暂不支持自定义连接参数。" />
    }
    return (
      <>
        <Alert
          type="info"
          content="参数会追加到 JDBC 连接地址；同名参数会覆盖 TLS/SSL 与字符集设置。"
          style={{ marginBottom: 16 }}
        />
        <div className={styles['param-list']}>
          {params.map((param, index) => (
            <div key={index} className={styles['param-row']}>
              <Input
                value={param.key}
                placeholder="参数名"
                maxLength={100}
                onChange={(value) => updateParam(index, 'key', value)}
              />
              <span className={styles['param-equals']}>=</span>
              <Input
                value={param.value}
                placeholder="参数值"
                maxLength={300}
                onChange={(value) => updateParam(index, 'value', value)}
              />
              <Button
                type="text"
                status="danger"
                icon={<IconDelete />}
                onClick={() =>
                  setParams((previous) => previous.filter((_, itemIndex) => itemIndex !== index))
                }
              />
            </div>
          ))}
        </div>
        <Button
          type="dashed"
          long
          icon={<IconPlus />}
          onClick={() => setParams((previous) => [...previous, { key: '', value: '' }])}
        >
          添加连接参数
        </Button>
        {params.length === 0 ? (
          <div className={styles['param-empty']}>没有自定义参数，默认使用驱动配置。</div>
        ) : null}
      </>
    )
  }

  const renderFormStep = () => (
    <Tabs activeTab={activeTab} onChange={setActiveTab} className={styles.tabs}>
      <Tabs.TabPane key="basic" title="连接信息">
        <div className={styles['tab-pane']}>
          {rawUrl !== null ? renderRawFields() : renderBasicFields()}
        </div>
      </Tabs.TabPane>
      <Tabs.TabPane key="ssl" title="TLS/SSL" disabled={rawUrl !== null}>
        <div className={styles['tab-pane']}>{renderSSL()}</div>
      </Tabs.TabPane>
      <Tabs.TabPane key="advanced" title="高级" disabled={rawUrl !== null}>
        <div className={styles['tab-pane']}>{renderAdvanced()}</div>
      </Tabs.TabPane>
    </Tabs>
  )

  return (
    <Modal
      visible={visible}
      title={
        <div className={styles.title}>
          <span>{editing ? '编辑数据源' : '新建数据源'}</span>
          <span className={styles.subtitle}>{step === 1 ? '选择数据库类型' : '配置连接信息'}</span>
        </div>
      }
      footer={null}
      onCancel={onClose}
      unmountOnExit
      maskClosable={false}
      className={styles.modal}
      style={{ width: 'min(960px, calc(100vw - 48px))', top: 32 }}
    >
      <div className={styles.shell}>
        <div className={classNames(styles.layer, step !== 1 && styles.hidden)}>
          {renderTypeStep()}
        </div>

        <Form
          form={form}
          layout="horizontal"
          labelAlign="left"
          labelCol={{ span: 4 }}
          wrapperCol={{ span: 20 }}
          className={classNames(styles.layer, styles['form-layer'], step !== 2 && styles.hidden)}
          initialValues={initial.values}
          onChange={(_, all) => setBase((previous) => ({ ...previous, ...all }))}
        >
          <div className={styles['form-body']}>
            {formError ? (
              <Alert
                type="error"
                content={formError}
                closable
                className={styles['form-alert']}
                onClose={() => setFormError('')}
              />
            ) : null}
            {renderFormStep()}
          </div>

          {testResult ? (
            <div className={styles['test-strip']}>
              <Alert
                type={testResult.ok ? 'success' : 'error'}
                content={describeTestResult(testResult)}
                closable
                onClose={() => setTestResult(null)}
              />
            </div>
          ) : null}

          <div className={styles.footer}>
            <Button icon={<IconLeft />} onClick={() => setStep(1)}>
              上一步
            </Button>
            <div className={styles.actions}>
              <Button icon={<IconSync />} loading={testing} onClick={handleTest}>
                测试连接
              </Button>
              <Button type="primary" loading={saving} onClick={handleSave}>
                {editing ? '保存修改' : '保存数据源'}
              </Button>
            </div>
          </div>
        </Form>
      </div>
    </Modal>
  )
}
