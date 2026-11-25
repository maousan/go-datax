import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { MemoryRouter } from 'react-router'
import type { Endpoint } from '../../api/endpoints'
import PipelinesPage from './index'

const schema = [
  { name: 'id', type: 'int64' as const },
  { name: 'name', type: 'string' as const },
]

function endpoint(
  kind: 'source' | 'sink',
  id: number,
  name: string,
  connector: 'csv' | 'sqlite',
): Endpoint {
  return {
    id,
    kind,
    name,
    description: '',
    connector,
    dataSourceId: 1,
    dataSourceName: 'demo',
    table: kind === 'sink' ? 'dst' : '',
    query: '',
    path: kind === 'source' ? 'source.csv' : '',
    schema,
    options: {},
    createdAt: '',
    updatedAt: '',
  }
}

const source = endpoint('source', 11, 'ui-source', 'csv')
const sink = endpoint('sink', 12, 'ui-sink', 'sqlite')

const server = setupServer(
  http.get(/\/api\/v1\/pipelines$/, () => HttpResponse.json([])),
  http.get(/\/api\/v1\/sources$/, () => HttpResponse.json([source])),
  http.get(/\/api\/v1\/sinks$/, () => HttpResponse.json([sink])),
  http.post(/\/api\/v1\/pipelines$/, async ({ request }) => {
    const body = (await request.json()) as Record<string, unknown>
    return HttpResponse.json(
      {
        id: 1,
        name: body.name,
        description: body.description,
        sourceId: body.sourceId,
        sinkId: body.sinkId,
        source,
        sink,
        latestVersion: null,
        lastRun: null,
        createdAt: '',
        updatedAt: '',
      },
      { status: 201 },
    )
  }),
)

beforeAll(() => server.listen())
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

describe('PipelinesPage', () => {
  it('creates a pipeline from the modal', async () => {
    let created: Record<string, unknown> | null = null
    server.use(
      http.post(/\/api\/v1\/pipelines$/, async ({ request }) => {
        created = (await request.json()) as Record<string, unknown>
        return HttpResponse.json(
          {
            id: 1,
            ...created,
            source,
            sink,
            latestVersion: null,
            lastRun: null,
            createdAt: '',
            updatedAt: '',
          },
          { status: 201 },
        )
      }),
    )

    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <PipelinesPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('还没有 Pipeline')).toBeTruthy()
    fireEvent.click(screen.getByText('新建 Pipeline'))
    const nameInput = await screen.findByPlaceholderText('例如：订单同步')
    fireEvent.change(nameInput, { target: { value: 'ui-pipeline' } })

    const comboboxes = screen.getAllByRole('combobox')
    expect(comboboxes).toHaveLength(2)
    fireEvent.mouseDown(comboboxes[0])
    fireEvent.click(comboboxes[0])
    fireEvent.click(await screen.findByText('ui-source（csv）'))
    fireEvent.mouseDown(comboboxes[1])
    fireEvent.click(comboboxes[1])
    fireEvent.click(await screen.findByText('ui-sink（sqlite）'))
    fireEvent.click(screen.getByText('创建'))

    await waitFor(() => expect(created).not.toBeNull())
    expect(created).toMatchObject({
      name: 'ui-pipeline',
      sourceId: 11,
      sinkId: 12,
      transforms: [],
    })
  })
})
