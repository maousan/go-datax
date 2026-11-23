import { render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import App from './App'

const server = setupServer(
  http.get('http://localhost:3000/api/v1/health', () =>
    HttpResponse.json({ status: 'ok', version: 'test-version' }),
  ),
)

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
afterEach(() => server.resetHandlers())
afterAll(() => server.close())

function renderApp(path = '/') {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[path]}>
        <App />
      </MemoryRouter>
    </QueryClientProvider>,
  )
}

describe('admin shell', () => {
  it('renders the navigation and the server version', async () => {
    renderApp('/')

    expect(screen.getByText('go-datax 管理台')).toBeInTheDocument()
    for (const label of ['概览', '数据源', '同步任务', '运行历史']) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }

    expect(await screen.findByText(/test-version/)).toBeInTheDocument()
  })

  it('serves a direct route with its page content', async () => {
    renderApp('/runs')

    expect(await screen.findByText('还没有运行记录')).toBeInTheDocument()
  })

  it('shows an offline state when the health check fails', async () => {
    server.use(
      http.get('http://localhost:3000/api/v1/health', () =>
        HttpResponse.json({ error: { code: 'internal', message: 'boom' } }, { status: 500 }),
      ),
    )

    renderApp('/')

    expect(await screen.findByText('服务未连接')).toBeInTheDocument()
  })
})
