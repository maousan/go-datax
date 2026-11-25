import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import { MemoryRouter } from 'react-router'
import type { Pipeline, PipelineRun } from '../../api/pipelines'
import PipelineRunsPage from './index'

const pipeline: Pipeline = {
  id: 1,
  name: 'ui-pipeline',
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

const run: PipelineRun = {
  id: 9,
  pipelineId: 1,
  versionId: 3,
  scheduleId: null,
  versionNumber: 1,
  pipelineName: 'ui-pipeline',
  status: 'succeeded',
  trigger: 'manual',
  startedAt: new Date().toISOString(),
  finishedAt: new Date().toISOString(),
  readRecords: 3,
  writeRecords: 3,
  readBytes: 0,
  errorSummary: '',
  retryAttempt: 0,
  createdAt: '',
}

let lastRunsURL = ''

const server = setupServer(
  http.get(/\/api\/v1\/pipelines$/, () => HttpResponse.json([pipeline])),
  http.get(/\/api\/v1\/pipeline-runs/, ({ request }) => {
    lastRunsURL = request.url
    return HttpResponse.json({
      items: [run],
      total: 1,
      page: 1,
      pageSize: 20,
    })
  }),
)

beforeAll(() => server.listen())
afterEach(() => {
  server.resetHandlers()
  lastRunsURL = ''
})
afterAll(() => server.close())

describe('PipelineRunsPage', () => {
  it('filters runs by pipeline and status', async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={queryClient}>
        <MemoryRouter>
          <PipelineRunsPage />
        </MemoryRouter>
      </QueryClientProvider>,
    )

    expect(await screen.findByText('ui-pipeline')).toBeTruthy()
    expect(await screen.findByText('成功')).toBeTruthy()
    const statusCombo = screen
      .getAllByRole('combobox')
      .find((element) => element.textContent?.includes('全部状态'))
    expect(statusCombo).toBeTruthy()
    fireEvent.mouseDown(statusCombo as HTMLElement)
    fireEvent.click(statusCombo as HTMLElement)
    fireEvent.click(await screen.findByText('失败'))
    await waitFor(() => expect(lastRunsURL).toContain('status=failed'))

    const pipelineCombo = screen
      .getAllByRole('combobox')
      .find((element) => element.textContent?.includes('全部 Pipeline'))
    expect(pipelineCombo).toBeTruthy()
    fireEvent.mouseDown(pipelineCombo as HTMLElement)
    fireEvent.click(pipelineCombo as HTMLElement)
    fireEvent.click(await screen.findByRole('option', { name: 'ui-pipeline' }))
    await waitFor(() => {
      expect(lastRunsURL).toContain('pipelineId=1')
      expect(lastRunsURL).toContain('status=failed')
    })
  })
})
