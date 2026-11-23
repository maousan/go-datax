import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { MemoryRouter } from 'react-router'
import { http, HttpResponse } from 'msw'
import { setupServer } from 'msw/node'
import { afterAll, afterEach, beforeAll, describe, expect, it } from 'vitest'
import App from './App'
import AuthProvider from './auth/AuthProvider'
import { TOKEN_STORAGE_KEY } from './auth/storage'

const TOKEN_KEY = TOKEN_STORAGE_KEY
const ORIGIN = 'http://localhost:3000'

const server = setupServer(
  http.get(`${ORIGIN}/api/v1/health`, () =>
    HttpResponse.json({ status: 'ok', version: 'test-version' }),
  ),
  http.post(`${ORIGIN}/api/v1/auth/login`, async ({ request }) => {
    const body = (await request.json()) as { username: string; password: string }
    if (body.username === 'admin' && body.password === 'secret') {
      return HttpResponse.json({
        token: 'valid-token',
        expiresAt: '2030-01-01T00:00:00Z',
      })
    }
    return HttpResponse.json(
      { error: { code: 'invalid_credentials', message: '用户名或密码错误' } },
      { status: 401 },
    )
  }),
  http.get(`${ORIGIN}/api/v1/auth/me`, ({ request }) => {
    if (request.headers.get('Authorization') !== 'Bearer valid-token') {
      return HttpResponse.json(
        { error: { code: 'unauthorized', message: '请先登录' } },
        { status: 401 },
      )
    }
    return HttpResponse.json({ username: 'admin', expiresAt: '2030-01-01T00:00:00Z' })
  }),
)

beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
afterEach(() => {
  server.resetHandlers()
  localStorage.clear()
})
afterAll(() => server.close())

function renderApp(path = '/', authenticated = true) {
  localStorage.clear()
  if (authenticated) {
    localStorage.setItem(TOKEN_KEY, 'valid-token')
  }

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <QueryClientProvider client={queryClient}>
      <AuthProvider>
        <MemoryRouter initialEntries={[path]}>
          <App />
        </MemoryRouter>
      </AuthProvider>
    </QueryClientProvider>,
  )
}

async function submitLogin(password = 'secret') {
  const user = userEvent.setup()
  await user.type(await screen.findByPlaceholderText('用户名'), 'admin')
  await user.type(screen.getByPlaceholderText('密码'), password)
  await user.click(screen.getByRole('button', { name: /登\s*录/ }))
}

describe('admin shell', () => {
  it('redirects unauthenticated visitors to the login page', async () => {
    renderApp('/', false)

    expect(await screen.findByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
    expect(screen.getByText('go-datax 管理台')).toBeInTheDocument()
    expect(screen.queryByText('数据源')).not.toBeInTheDocument()
  })

  it('logs in and returns to the requested page', async () => {
    renderApp('/runs', false)

    await submitLogin()

    expect(await screen.findByText('还没有运行记录')).toBeInTheDocument()
    expect(localStorage.getItem(TOKEN_KEY)).toBe('valid-token')
  })

  it('keeps the user on the login page with a uniform error for bad credentials', async () => {
    renderApp('/', false)

    await submitLogin('wrong')

    expect(await screen.findByText('用户名或密码错误')).toBeInTheDocument()
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
  })

  it('reports a connection problem when login fails for another reason', async () => {
    server.use(
      http.post(`${ORIGIN}/api/v1/auth/login`, () =>
        HttpResponse.json({ error: { code: 'internal', message: 'boom' } }, { status: 500 }),
      ),
    )

    renderApp('/', false)
    await submitLogin()

    expect(await screen.findByText('无法连接服务，请稍后重试')).toBeInTheDocument()
  })

  it('renders the navigation, user and server version when authenticated', async () => {
    renderApp('/')

    expect(screen.getByText('go-datax 管理台')).toBeInTheDocument()
    for (const label of ['概览', '数据源', '同步任务', '运行历史']) {
      expect(screen.getAllByText(label).length).toBeGreaterThan(0)
    }

    expect(await screen.findByText(/test-version/)).toBeInTheDocument()
    expect(await screen.findByText('admin')).toBeInTheDocument()
  })

  it('serves a direct route with its page content', async () => {
    renderApp('/runs')

    expect(await screen.findByText('还没有运行记录')).toBeInTheDocument()
  })

  it('logs out and returns to the login page', async () => {
    renderApp('/')
    await screen.findByText('admin')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /退出登录/ }))

    expect(await screen.findByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
  })

  it('clears the session when the server rejects the token', async () => {
    server.use(
      http.get(`${ORIGIN}/api/v1/auth/me`, () =>
        HttpResponse.json(
          { error: { code: 'unauthorized', message: '登录已失效' } },
          { status: 401 },
        ),
      ),
    )

    renderApp('/')

    await waitFor(() => expect(localStorage.getItem(TOKEN_KEY)).toBeNull())
    expect(await screen.findByRole('button', { name: /登\s*录/ })).toBeInTheDocument()
    expect(localStorage.getItem(TOKEN_KEY)).toBeNull()
  })

  it('shows an offline state when the health check fails', async () => {
    server.use(
      http.get(`${ORIGIN}/api/v1/health`, () =>
        HttpResponse.json({ error: { code: 'internal', message: 'boom' } }, { status: 500 }),
      ),
    )

    renderApp('/')

    expect(await screen.findByText('服务未连接')).toBeInTheDocument()
  })
})
