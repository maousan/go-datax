import { screen } from '@testing-library/react'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import { ORIGIN, renderApp, server, setupTestServer } from '../test/harness'

setupTestServer()

const sampleRun = {
  id: 7,
  syncJobId: 3,
  syncJobName: 'orders-sync',
  status: 'succeeded',
  trigger: 'manual',
  startedAt: '2026-01-01T10:00:00Z',
  finishedAt: '2026-01-01T10:00:05Z',
  exitCode: 0,
  errorSummary: '',
  logSize: 120,
  createdAt: '2026-01-01T10:00:00Z',
}

describe('runs page', () => {
  it('lists runs with status and duration', async () => {
    server.use(
      http.get(`${ORIGIN}/api/v1/runs`, () =>
        HttpResponse.json({ items: [sampleRun], total: 1, page: 1, pageSize: 20 }),
      ),
    )

    renderApp('/runs')

    expect(await screen.findByText('orders-sync')).toBeInTheDocument()
    expect(await screen.findByText('成功')).toBeInTheDocument()
    expect(screen.getByText('5s')).toBeInTheDocument()
  })
})

describe('run detail page', () => {
  it('streams logs and shows the final status', async () => {
    server.use(
      http.get(`${ORIGIN}/api/v1/runs/7`, () => HttpResponse.json(sampleRun)),
      http.get(`${ORIGIN}/api/v1/runs/7/logs/stream`, () =>
        new HttpResponse(
          [
            'data: {"type":"log","line":"hello log","seq":10}',
            '',
            'data: {"type":"status","status":"succeeded","exitCode":0}',
            '',
            'data: {"type":"end"}',
            '',
            '',
          ].join('\n'),
          { headers: { 'Content-Type': 'text/event-stream' } },
        ),
      ),
    )

    renderApp('/runs/7')

    expect(await screen.findByText(/hello log/)).toBeInTheDocument()
    expect(await screen.findByText('成功')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /下载日志/ })).toBeInTheDocument()
  })
})
