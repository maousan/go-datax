import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import type { AlertLog } from '../../api/alerts'
import AlertLogsPage from './logs'

const logs: AlertLog[] = [
  {
    id: 2,
    sourceKind: 'pipeline',
    event: 'failed',
    status: 'success',
    channelId: 'ch1',
    channelName: 'ops-hook',
    channelType: 'webhook',
    runId: 0,
    syncJobId: null,
    syncJobName: '',
    collectTaskId: null,
    collectTaskName: '',
    pipelineId: 11,
    pipelineName: 'orders-etl',
    pipelineRunId: 44,
    versionNumber: 3,
    error: '',
    durationMs: 12,
    createdAt: new Date().toISOString(),
  },
  {
    id: 1,
    sourceKind: 'datax',
    event: 'succeeded',
    status: 'success',
    channelId: 'ch1',
    channelName: 'ops-hook',
    channelType: 'webhook',
    runId: 42,
    syncJobId: 7,
    syncJobName: 'nightly-sync',
    collectTaskId: null,
    collectTaskName: '',
    pipelineId: null,
    pipelineName: '',
    pipelineRunId: null,
    versionNumber: 0,
    error: '',
    durationMs: 10,
    createdAt: new Date().toISOString(),
  },
]

const server = setupServer(
  http.get(/\/api\/v1\/alerts\/logs/, () =>
    HttpResponse.json({ items: logs, total: 2, page: 1, pageSize: 20 }),
  ),
)

beforeAll(() => server.listen())
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

describe('AlertLogsPage', () => {
  it('shows DataX and Pipeline sources with pipeline run links', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <AlertLogsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('orders-etl')).toBeTruthy()
    expect(screen.getByText('nightly-sync')).toBeTruthy()
    expect(screen.getByText('ETL')).toBeTruthy()
    expect(screen.getByText('DataX')).toBeTruthy()
    expect(screen.getByText('v3')).toBeTruthy()
  })
})
