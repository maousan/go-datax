import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it, vi } from 'vitest'
import type { SchemaColumn } from '../../api/endpoints'
import type { TransformStep } from '../../api/pipelines'
import TransformEditor from './transforms'

const schema: SchemaColumn[] = [
  { name: 'id', type: 'int64' },
  { name: 'name', type: 'string' },
]

let previewBodies: Array<{ sourceId: number; transforms: TransformStep[] }> = []

const server = setupServer(
  http.post(/\/api\/v1\/pipelines\/transform-preview$/, async ({ request }) => {
    const body = (await request.json()) as {
      sourceId: number
      sourceSchema: SchemaColumn[]
      transforms: TransformStep[]
    }
    previewBodies.push({ sourceId: body.sourceId, transforms: body.transforms })
    return HttpResponse.json({
      steps: body.transforms.map((step, index) => ({
        index,
        type: step.type,
        inputSchema: body.sourceSchema,
        outputSchema: body.sourceSchema,
      })),
      outputSchema: body.sourceSchema,
    })
  }),
)

beforeAll(() => server.listen())
afterEach(() => {
  server.resetHandlers()
  previewBodies = []
})
afterAll(() => server.close())

describe('TransformEditor', () => {
  it('previews the chain and appends a project step from the output schema', async () => {
    const onChange = vi.fn()
    const steps: TransformStep[] = [{ type: 'filter', condition: 'id > 0' }]

    render(
      <TransformEditor
        sourceId={11}
        sourceSchema={schema}
        sinkSchema={schema}
        steps={steps}
        onChange={onChange}
      />,
    )

    expect(await screen.findByText('Transform 输出字段与目标字段一致，可以发布。')).toBeTruthy()
    await waitFor(() => expect(previewBodies.length).toBeGreaterThan(0))
    expect(previewBodies.at(-1)).toEqual({
      sourceId: 11,
      transforms: [{ type: 'filter', condition: 'id > 0' }],
    })

    fireEvent.click(screen.getByText('Project 字段'))
    expect(onChange).toHaveBeenCalledWith([
      { type: 'filter', condition: 'id > 0' },
      { type: 'project', project: [{ name: '', type: 'string', expr: '' }] },
    ])
  })

  it('warns when the transform output does not match the sink schema', async () => {
    render(
      <TransformEditor
        sourceId={11}
        sourceSchema={schema}
        sinkSchema={[...schema, { name: 'extra', type: 'string' }]}
        steps={[{ type: 'filter', condition: 'id > 0' }]}
        onChange={vi.fn()}
      />,
    )

    expect(
      await screen.findByText(
        'Transform 输出字段与目标字段不一致；草稿可保存，发布会被拒绝。',
      ),
    ).toBeTruthy()
  })

  it('shows preview errors from the API', async () => {
    server.use(
      http.post(/\/api\/v1\/pipelines\/transform-preview$/, () =>
        HttpResponse.json(
          { error: { code: 'invalid_transform', message: '步骤 1 (filter): 条件必须是 bool' } },
          { status: 400 },
        ),
      ),
    )

    render(
      <TransformEditor
        sourceId={11}
        sourceSchema={schema}
        sinkSchema={schema}
        steps={[{ type: 'filter', condition: 'name' }]}
        onChange={vi.fn()}
      />,
    )

    expect(await screen.findByText('步骤 1 (filter): 条件必须是 bool')).toBeTruthy()
  })
})
