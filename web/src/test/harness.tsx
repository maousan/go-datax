import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import { HttpResponse, http } from 'msw'
import { setupServer } from 'msw/node'
import { MemoryRouter } from 'react-router'
import { afterAll, afterEach, beforeAll } from 'vitest'
import App from '../App'
import AuthProvider from '../auth/AuthProvider'
import { TOKEN_STORAGE_KEY } from '../auth/storage'

export const ORIGIN = 'http://localhost:3000'
export const TEST_TOKEN = 'valid-token'
export const TEST_PASSWORD = 'secret'
export const TOKEN_KEY = TOKEN_STORAGE_KEY

export const server = setupServer(
  http.get(`${ORIGIN}/api/v1/health`, () =>
    HttpResponse.json({ status: 'ok', version: 'test-version' }),
  ),
  http.post(`${ORIGIN}/api/v1/auth/login`, async ({ request }) => {
    const body = (await request.json()) as { username: string; password: string }
    if (body.username === 'admin' && body.password === TEST_PASSWORD) {
      return HttpResponse.json({
        token: TEST_TOKEN,
        expiresAt: '2030-01-01T00:00:00Z',
      })
    }
    return HttpResponse.json(
      { error: { code: 'invalid_credentials', message: '用户名或密码错误' } },
      { status: 401 },
    )
  }),
  http.get(`${ORIGIN}/api/v1/auth/me`, ({ request }) => {
    if (request.headers.get('Authorization') !== `Bearer ${TEST_TOKEN}`) {
      return HttpResponse.json(
        { error: { code: 'unauthorized', message: '请先登录' } },
        { status: 401 },
      )
    }
    return HttpResponse.json({ username: 'admin', expiresAt: '2030-01-01T00:00:00Z' })
  }),
)

export function setupTestServer() {
  // MSW v3 将 onUnhandledRequest 更名为 onUnhandledFrame。
  beforeAll(() => server.listen({ onUnhandledFrame: 'error' }))
  afterEach(() => {
    server.resetHandlers()
    localStorage.clear()
  })
  afterAll(() => server.close())
}

export function renderApp(path = '/', authenticated = true) {
  localStorage.clear()
  if (authenticated) {
    localStorage.setItem(TOKEN_KEY, TEST_TOKEN)
  }

  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })

  return render(
    <ConfigProvider locale={zhCN}>
      <QueryClientProvider client={queryClient}>
        <AuthProvider>
          <MemoryRouter initialEntries={[path]}>
            <App />
          </MemoryRouter>
        </AuthProvider>
      </QueryClientProvider>
    </ConfigProvider>,
  )
}

export async function submitLogin(password = TEST_PASSWORD) {
  const user = userEvent.setup()
  await user.type(await screen.findByPlaceholderText('用户名'), 'admin')
  await user.type(screen.getByPlaceholderText('密码'), password)
  await user.click(screen.getByRole('button', { name: /登\s*录/ }))
}
