import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import type { Pipeline } from '../../api/pipelines'
import type { PipelineSchedule } from '../../api/schedules'
import SchedulesPage from './index'

const pipeline: Pipeline = {
  id: 1,
  name: 'orders',
  description: '',
  sourceId: 1,
  sinkId: 2,
  source: null,
  sink: null,
  transforms: [],
  transformPreview: null,
  draftVersionNumber: 0,
  latestVersion: null,
  lastRun: null,
  createdAt: '',
  updatedAt: '',
}

const schedule: PipelineSchedule = {
  id: 1,
  pipelineId: 1,
  pipelineName: 'orders',
  name: 'daily-orders',
  cronExpr: '0 0 2 * * ?',
  timezone: 'Asia/Shanghai',
  enabled: true,
  blockingPolicy: 'queue',
  retryMax: 2,
  retryIntervalSeconds: 30,
  timeoutSeconds: 600,
  pinnedVersionId: 3,
  pinnedVersionNumber: 2,
  nextRunAt: new Date(Date.now() + 3600_000).toISOString(),
  lastRun: null,
  createdAt: '',
  updatedAt: '',
}

let updateBody: Record<string, unknown> | null = null

const server = setupServer(
  http.get(/\/api\/v1\/pipeline-schedules/, () => HttpResponse.json([schedule])),
  http.get(/\/api\/v1\/pipelines$/, () => HttpResponse.json([pipeline])),
  http.put(/\/api\/v1\/pipeline-schedules\/1$/, async ({ request }) => {
    updateBody = (await request.json()) as Record<string, unknown>
    return HttpResponse.json({ ...schedule, enabled: false })
  }),
)

beforeAll(() => server.listen())
afterEach(() => {
  server.resetHandlers()
  updateBody = null
})
afterAll(() => server.close())

describe('SchedulesPage', () => {
  it('renders schedules and toggles enabled state', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <SchedulesPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('daily-orders')).toBeTruthy()
    expect(screen.getByText('0 0 2 * * ?')).toBeTruthy()
    expect(screen.getByText('排队')).toBeTruthy()
    expect(screen.getByText('v2')).toBeTruthy()

    const toggle = screen.getByRole('switch')
    fireEvent.click(toggle)
    await waitFor(() => expect(updateBody).not.toBeNull())
    expect(updateBody).toMatchObject({
      name: 'daily-orders',
      enabled: false,
      blockingPolicy: 'queue',
      versionId: 3,
    })
  })
})
