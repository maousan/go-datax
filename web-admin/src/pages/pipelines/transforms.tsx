import {
  Alert,
  Button,
  Empty,
  Form,
  Input,
  Select,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@arco-design/web-react'
import {
  IconArrowDown,
  IconArrowUp,
  IconDelete,
  IconPlus,
} from '@arco-design/web-react/icon'
import { useEffect, useState } from 'react'
import { ApiError } from '../../api/client'
import { DATA_TYPE_OPTIONS, type DataType, type SchemaColumn } from '../../api/endpoints'
import {
  previewPipelineTransforms,
  type TransformPreview,
  type TransformStep,
  type TransformStepType,
} from '../../api/pipelines'
import { schemaText, schemasMatch, defaultTransformStep } from './helpers'
import styles from './transforms.module.less'

const STEP_LABELS: Record<TransformStepType, string> = {
  project: 'Project 字段',
  filter: 'Filter 过滤',
  cast: 'Cast 类型',
  rename: 'Rename 重命名',
  drop: 'Drop 删除列',
}

const STEP_TYPES: TransformStepType[] = ['project', 'filter', 'cast', 'rename', 'drop']

export default function TransformEditor({
  sourceId,
  sourceSchema,
  sinkSchema,
  steps,
  initialPreview,
  onChange,
}: {
  sourceId: number
  sourceSchema: SchemaColumn[]
  sinkSchema: SchemaColumn[]
  steps: TransformStep[]
  initialPreview?: TransformPreview | null
  onChange: (steps: TransformStep[]) => void
}) {
  const [preview, setPreview] = useState<TransformPreview | undefined>(
    initialPreview ?? undefined,
  )
  const [previewError, setPreviewError] = useState('')

  useEffect(() => {
    if (!sourceId || sourceSchema.length === 0) return
    let ignored = false
    const timer = window.setTimeout(async () => {
      try {
        const result = await previewPipelineTransforms(sourceId, sourceSchema, steps)
        if (!ignored) {
          setPreview(result)
          setPreviewError('')
        }
      } catch (err) {
        if (!ignored) {
          setPreviewError(err instanceof ApiError ? err.message : 'Transform 预览失败')
        }
      }
    }, 250)
    return () => {
      ignored = true
      window.clearTimeout(timer)
    }
  }, [sourceId, sourceSchema, steps])

  const inputFor = (index: number): SchemaColumn[] => {
    const step = preview?.steps[index]
    if (step) return step.inputSchema
    if (index === 0) return sourceSchema
    return preview?.steps[index - 1]?.outputSchema ?? sourceSchema
  }

  const append = (type: TransformStepType) => {
    onChange([...steps, defaultTransformStep(type, preview?.outputSchema ?? sourceSchema)])
  }

  const replace = (index: number, step: TransformStep) => {
    onChange(steps.map((current, position) => (position === index ? step : current)))
  }

  const move = (index: number, direction: -1 | 1) => {
    const target = index + direction
    if (target < 0 || target >= steps.length) return
    const next = [...steps]
    ;[next[index], next[target]] = [next[target], next[index]]
    onChange(next)
  }

  const remove = (index: number) => {
    onChange(steps.filter((_, position) => position !== index))
  }

  if (!sourceId || sourceSchema.length === 0) {
    return <Empty description="请先选择源，Transform 需要源字段" />
  }

  const outputSchema = preview?.outputSchema ?? sourceSchema
  const outputMatches = schemasMatch(outputSchema, sinkSchema)

  return (
    <Form layout="vertical" className={styles.editor}>
      <div className={styles.toolbar}>
        <Typography.Text type="secondary">
          按顺序执行；列名可直接引用，含空格时用 <code>_row[&quot;列名&quot;]</code>
        </Typography.Text>
        <Space wrap>
          {STEP_TYPES.map((type) => (
            <Button
              key={type}
              size="small"
              icon={<IconPlus />}
              onClick={() => append(type)}
            >
              {STEP_LABELS[type]}
            </Button>
          ))}
        </Space>
      </div>

      {previewError ? (
        <Alert type="error" content={previewError} closable onClose={() => setPreviewError('')} />
      ) : null}

      {steps.map((step, index) => (
        <div key={index} className={styles.step}>
          <div className={styles['step-header']}>
            <Space>
              <Tag color="arcoblue">步骤 {index + 1}</Tag>
              <strong>{STEP_LABELS[step.type]}</strong>
            </Space>
            <Space size={2}>
              <Tooltip content="上移">
                <Button
                  type="text"
                  size="small"
                  disabled={index === 0}
                  icon={<IconArrowUp />}
                  onClick={() => move(index, -1)}
                />
              </Tooltip>
              <Tooltip content="下移">
                <Button
                  type="text"
                  size="small"
                  disabled={index === steps.length - 1}
                  icon={<IconArrowDown />}
                  onClick={() => move(index, 1)}
                />
              </Tooltip>
              <Tooltip content="删除步骤">
                <Button
                  type="text"
                  status="danger"
                  size="small"
                  icon={<IconDelete />}
                  onClick={() => remove(index)}
                />
              </Tooltip>
            </Space>
          </div>

          {step.type === 'project' ? (
            <Form.Item label="输出字段" required>
              <div className={styles.rows}>
                {(step.project ?? []).map((column, columnIndex) => (
                  <div key={columnIndex} className={styles['project-row']}>
                    <Input
                      value={column.name}
                      placeholder="字段名"
                      aria-label={`步骤 ${index + 1} 输出字段 ${columnIndex + 1} 名称`}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          project: (step.project ?? []).map((current, position) =>
                            position === columnIndex ? { ...current, name: value } : current,
                          ),
                        })
                      }
                    />
                    <Select
                      value={column.type}
                      options={DATA_TYPE_OPTIONS}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          project: (step.project ?? []).map((current, position) =>
                            position === columnIndex
                              ? { ...current, type: value as DataType }
                              : current,
                          ),
                        })
                      }
                    />
                    <Input
                      value={column.expr}
                      placeholder="表达式，例如 upper(name)"
                      aria-label={`步骤 ${index + 1} 输出字段 ${columnIndex + 1} 表达式`}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          project: (step.project ?? []).map((current, position) =>
                            position === columnIndex ? { ...current, expr: value } : current,
                          ),
                        })
                      }
                    />
                    <Button
                      type="text"
                      status="danger"
                      icon={<IconDelete />}
                      aria-label={`删除步骤 ${index + 1} 输出字段 ${columnIndex + 1}`}
                      onClick={() =>
                        replace(index, {
                          ...step,
                          project: (step.project ?? []).filter(
                            (_, position) => position !== columnIndex,
                          ),
                        })
                      }
                    />
                  </div>
                ))}
                <Button
                  size="small"
                  icon={<IconPlus />}
                  onClick={() =>
                    replace(index, {
                      ...step,
                      project: [
                        ...(step.project ?? []),
                        { name: '', type: 'string', expr: '' },
                      ],
                    })
                  }
                >
                  添加输出列
                </Button>
              </div>
            </Form.Item>
          ) : null}

          {step.type === 'filter' ? (
            <Form.Item
              label="过滤条件"
              required
              extra="表达式结果必须为 bool；返回 false 的行会被丢弃"
            >
              <Input
                value={step.condition}
                placeholder="例如 amount > 10 && is_null(name) == false"
                onChange={(value) => replace(index, { ...step, condition: value })}
              />
            </Form.Item>
          ) : null}

          {step.type === 'cast' ? (
            <Form.Item label="类型转换" required>
              <div className={styles.rows}>
                {(step.cast ?? []).map((cast, castIndex) => (
                  <div key={castIndex} className={styles['cast-row']}>
                    <Select
                      showSearch
                      value={cast.column}
                      placeholder="选择字段"
                      options={inputFor(index).map((column) => ({
                        value: column.name,
                        label: `${column.name}（${column.type}）`,
                      }))}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          cast: (step.cast ?? []).map((current, position) =>
                            position === castIndex
                              ? { ...current, column: String(value) }
                              : current,
                          ),
                        })
                      }
                    />
                    <Select
                      value={cast.type}
                      options={DATA_TYPE_OPTIONS}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          cast: (step.cast ?? []).map((current, position) =>
                            position === castIndex
                              ? { ...current, type: value as DataType }
                              : current,
                          ),
                        })
                      }
                    />
                    <Button
                      type="text"
                      status="danger"
                      icon={<IconDelete />}
                      aria-label={`删除步骤 ${index + 1} 类型转换 ${castIndex + 1}`}
                      onClick={() =>
                        replace(index, {
                          ...step,
                          cast: (step.cast ?? []).filter(
                            (_, position) => position !== castIndex,
                          ),
                        })
                      }
                    />
                  </div>
                ))}
                <Button
                  size="small"
                  icon={<IconPlus />}
                  onClick={() =>
                    replace(index, {
                      ...step,
                      cast: [...(step.cast ?? []), { column: '', type: 'string' }],
                    })
                  }
                >
                  添加转换
                </Button>
              </div>
            </Form.Item>
          ) : null}

          {step.type === 'rename' ? (
            <Form.Item label="字段重命名" required>
              <div className={styles.rows}>
                {(step.rename ?? []).map((rename, renameIndex) => (
                  <div key={renameIndex} className={styles['rename-row']}>
                    <Select
                      showSearch
                      value={rename.from}
                      placeholder="原字段"
                      options={inputFor(index).map((column) => ({
                        value: column.name,
                        label: column.name,
                      }))}
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          rename: (step.rename ?? []).map((current, position) =>
                            position === renameIndex
                              ? { ...current, from: String(value) }
                              : current,
                          ),
                        })
                      }
                    />
                    <Input
                      value={rename.to}
                      placeholder="新字段名"
                      onChange={(value) =>
                        replace(index, {
                          ...step,
                          rename: (step.rename ?? []).map((current, position) =>
                            position === renameIndex ? { ...current, to: value } : current,
                          ),
                        })
                      }
                    />
                    <Button
                      type="text"
                      status="danger"
                      icon={<IconDelete />}
                      aria-label={`删除步骤 ${index + 1} 重命名 ${renameIndex + 1}`}
                      onClick={() =>
                        replace(index, {
                          ...step,
                          rename: (step.rename ?? []).filter(
                            (_, position) => position !== renameIndex,
                          ),
                        })
                      }
                    />
                  </div>
                ))}
                <Button
                  size="small"
                  icon={<IconPlus />}
                  onClick={() =>
                    replace(index, {
                      ...step,
                      rename: [...(step.rename ?? []), { from: '', to: '' }],
                    })
                  }
                >
                  添加重命名
                </Button>
              </div>
            </Form.Item>
          ) : null}

          {step.type === 'drop' ? (
            <Form.Item label="删除字段" required>
              <Select
                mode="multiple"
                value={step.drop ?? []}
                placeholder="选择要删除的字段"
                options={inputFor(index).map((column) => ({
                  value: column.name,
                  label: column.name,
                }))}
                onChange={(value) => replace(index, { ...step, drop: value as string[] })}
              />
            </Form.Item>
          ) : null}

          <div className={styles['step-schema']}>
            <Typography.Text type="secondary">输入</Typography.Text>
            <span>{schemaText(inputFor(index))}</span>
            <Typography.Text type="secondary">输出</Typography.Text>
            <span>{schemaText(preview?.steps[index]?.outputSchema ?? [])}</span>
          </div>
        </div>
      ))}

      <Alert
        type={outputMatches ? 'success' : 'warning'}
        content={
          outputMatches
            ? 'Transform 输出字段与目标字段一致，可以发布。'
            : 'Transform 输出字段与目标字段不一致；草稿可保存，发布会被拒绝。'
        }
      />
    </Form>
  )
}
