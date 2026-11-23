import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { HttpResponse, http } from 'msw'
import { describe, expect, it } from 'vitest'
import type { DataSource } from '../api/dataSources'
import { ORIGIN, renderApp, server, setupTestServer } from '../test/harness'

setupTestServer()

const sample: DataSource = {
  id: 1,
  name: 'prod-mysql',
  type: 'mysql',
  jdbcUrl: 'jdbc:mysql://localhost/app',
  username: 'root',
  password: '******',
  version: '8.0.33',
  properties: { key: 'warehouse' },
  createdAt: '2026-01-01T00:00:00Z',
  updatedAt: '2026-01-02T00:00:00Z',
}

interface CapturedCalls {
  created: Record<string, unknown> | null
  updated: Record<string, unknown> | null
  deleted: number[]
}

function mockDataSourceApi(initial: DataSource[]): CapturedCalls {
  const state: CapturedCalls & { items: DataSource[] } = {
    items: [...initial],
    created: null,
    updated: null,
    deleted: [],
  }

  server.use(
    http.get(`${ORIGIN}/api/v1/data-sources`, ({ request }) => {
      const query = new URL(request.url).searchParams.get('query')?.toLowerCase() ?? ''
      return HttpResponse.json(
        state.items.filter((item) => item.name.toLowerCase().includes(query)),
      )
    }),
    http.post(`${ORIGIN}/api/v1/data-sources`, async ({ request }) => {
      const body = (await request.json()) as Record<string, unknown>
      state.created = body
      const created: DataSource = {
        ...sample,
        id: state.items.length + 1,
        ...(body as Partial<DataSource>),
        password: '******',
      }
      state.items.push(created)
      return HttpResponse.json(created, { status: 201 })
    }),
    http.put(`${ORIGIN}/api/v1/data-sources/:id`, async ({ request, params }) => {
      const body = (await request.json()) as Record<string, unknown>
      state.updated = body
      const id = Number(params.id)
      state.items = state.items.map((item) =>
        item.id === id
          ? { ...item, ...(body as Partial<DataSource>), password: item.password }
          : item,
      )
      return HttpResponse.json(state.items.find((item) => item.id === id))
    }),
    http.delete(`${ORIGIN}/api/v1/data-sources/:id`, ({ params }) => {
      const id = Number(params.id)
      state.deleted.push(id)
      state.items = state.items.filter((item) => item.id !== id)
      return new HttpResponse(null, { status: 204 })
    }),
  )

  return state
}

async function fillCreateForm() {
  const user = userEvent.setup()
  await user.type(await screen.findByPlaceholderText('例如：prod-mysql'), 'new-mysql')
  await user.type(
    screen.getByPlaceholderText('jdbc:mysql://host:3306/db'),
    'jdbc:mysql://localhost/app',
  )
  await user.type(screen.getByPlaceholderText('数据库用户名'), 'root')
  await user.type(screen.getByPlaceholderText('数据库密码'), 'secret')
}

function mockConnectionTest(result: Record<string, unknown> = {}) {
  const calls: Array<Record<string, unknown>> = []
  server.use(
    http.post(`${ORIGIN}/api/v1/data-sources/test`, async ({ request }) => {
      calls.push((await request.json()) as Record<string, unknown>)
      return HttpResponse.json({
        ok: true,
        latencyMs: 12,
        message: '连接成功',
        ...result,
      })
    }),
  )
  return calls
}

describe('data sources page', () => {
  it('lists data sources without exposing passwords', async () => {
    mockDataSourceApi([sample])
    renderApp('/data-sources')

    expect(await screen.findByText('prod-mysql')).toBeInTheDocument()
    expect(screen.getByText('MySQL')).toBeInTheDocument()
    expect(screen.getByText('jdbc:mysql://localhost/app')).toBeInTheDocument()
    expect(screen.queryByText('******')).not.toBeInTheDocument()
  })

  it('filters data sources by name', async () => {
    mockDataSourceApi([
      sample,
      {
        ...sample,
        id: 2,
        name: 'beta-pg',
        type: 'postgres',
        jdbcUrl: 'jdbc:postgresql://localhost/app',
      },
    ])
    renderApp('/data-sources')
    await screen.findByText('prod-mysql')

    const user = userEvent.setup()
    await user.type(screen.getByPlaceholderText('按名称搜索'), 'beta{Enter}')

    await waitFor(() => expect(screen.queryByText('prod-mysql')).not.toBeInTheDocument())
    expect(screen.getByText('beta-pg')).toBeInTheDocument()
  })

  it('creates a data source from the modal form', async () => {
    const calls = mockDataSourceApi([])
    renderApp('/data-sources')
    await screen.findByText('还没有数据源')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /新建数据源/ }))
    await fillCreateForm()
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(calls.created).not.toBeNull())
    expect(calls.created).toMatchObject({
      name: 'new-mysql',
      type: 'mysql',
      jdbcUrl: 'jdbc:mysql://localhost/app',
      username: 'root',
      password: 'secret',
    })
    expect(await screen.findByText('new-mysql')).toBeInTheDocument()
  })

  it('shows a duplicate-name error without closing the modal', async () => {
    mockDataSourceApi([])
    server.use(
      http.post(`${ORIGIN}/api/v1/data-sources`, () =>
        HttpResponse.json(
          { error: { code: 'duplicate_name', message: '数据源名称已存在' } },
          { status: 409 },
        ),
      ),
    )
    renderApp('/data-sources')
    await screen.findByText('还没有数据源')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /新建数据源/ }))
    await fillCreateForm()
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    expect(await screen.findByText('数据源名称已存在')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: /保\s*存/ })).toBeInTheDocument()
  })

  it('offers SQL Server and Oracle as data source types', async () => {
    mockDataSourceApi([])
    renderApp('/data-sources')
    await screen.findByText('还没有数据源')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /新建数据源/ }))

    await user.click(await screen.findByRole('combobox'))
    expect(await screen.findByText('SQL Server')).toBeInTheDocument()
    expect(await screen.findByText('Oracle')).toBeInTheDocument()
  })

  it('tests an unsaved connection before saving', async () => {
    mockDataSourceApi([])
    const testCalls = mockConnectionTest()
    renderApp('/data-sources')
    await screen.findByText('还没有数据源')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /新建数据源/ }))
    await fillCreateForm()
    await user.click(screen.getByRole('button', { name: /测试连接/ }))

    expect(await screen.findByText('连接成功（12 ms）')).toBeInTheDocument()
    await waitFor(() => expect(testCalls).toHaveLength(1))
    expect(testCalls[0]).toMatchObject({
      type: 'mysql',
      jdbcUrl: 'jdbc:mysql://localhost/app',
      username: 'root',
      password: 'secret',
    })
  })

  it('tests a saved connection and reports failure', async () => {
    mockDataSourceApi([sample])
    const testCalls = mockConnectionTest({
      ok: false,
      latencyMs: 0,
      message: 'dial tcp: connection refused',
    })
    renderApp('/data-sources')
    await screen.findByText('prod-mysql')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /测\s*试/ }))

    expect(await screen.findByText('连接失败：dial tcp: connection refused')).toBeInTheDocument()
    await waitFor(() => expect(testCalls).toHaveLength(1))
    expect(testCalls[0]).toMatchObject({ id: 1 })
  })

  it('keeps an existing password when editing with a blank password', async () => {
    const calls = mockDataSourceApi([sample])
    renderApp('/data-sources')
    await screen.findByText('prod-mysql')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /编\s*辑/ }))

    const nameInput = await screen.findByPlaceholderText('例如：prod-mysql')
    await user.clear(nameInput)
    await user.type(nameInput, 'prod-mysql-renamed')
    await user.click(screen.getByRole('button', { name: /保\s*存/ }))

    await waitFor(() => expect(calls.updated).not.toBeNull())
    expect(calls.updated).toMatchObject({
      name: 'prod-mysql-renamed',
      password: '',
      properties: { key: 'warehouse' },
    })
  })

  it('tests an edited connection using the saved password', async () => {
    mockDataSourceApi([sample])
    const testCalls = mockConnectionTest()
    renderApp('/data-sources')
    await screen.findByText('prod-mysql')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /编\s*辑/ }))
    await user.click(screen.getByRole('button', { name: /测试连接/ }))

    expect(await screen.findByText('连接成功（12 ms）')).toBeInTheDocument()
    await waitFor(() => expect(testCalls).toHaveLength(1))
    expect(testCalls[0]).toMatchObject({
      id: 1,
      type: 'mysql',
      jdbcUrl: 'jdbc:mysql://localhost/app',
      password: '',
      properties: { key: 'warehouse' },
    })
  })

  it('deletes a data source after confirmation', async () => {
    const calls = mockDataSourceApi([sample])
    renderApp('/data-sources')
    await screen.findByText('prod-mysql')

    const user = userEvent.setup()
    await user.click(screen.getByRole('button', { name: /删\s*除/ }))
    await user.click(await screen.findByRole('button', { name: /确\s*定/ }))

    await waitFor(() => expect(calls.deleted).toEqual([1]))
    expect(await screen.findByText('还没有数据源')).toBeInTheDocument()
  })
})
