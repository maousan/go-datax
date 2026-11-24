import { describe, expect, it } from 'vitest'
import {
  buildJdbcUrl,
  defaultConnection,
  parseJdbcUrl,
  validateConnectionParams,
  type ConnectionFields,
} from './connection'

function fields(type: Parameters<typeof defaultConnection>[0], patch: Partial<ConnectionFields>) {
  return { ...defaultConnection(type), ...patch }
}

describe('buildJdbcUrl', () => {
  it('composes MySQL URL with database, charset, SSL and extra params', () => {
    const url = buildJdbcUrl(
      'mysql',
      fields('mysql', {
        host: 'db.example.com',
        port: '3306',
        database: 'orders',
        charset: 'utf8mb4',
        sslMode: 'verify',
        params: [{ key: 'useCursorFetch', value: 'true' }],
      }),
    )

    expect(url).toBe(
      'jdbc:mysql://db.example.com:3306/orders' +
        '?characterEncoding=UTF-8&connectionCollation=utf8mb4_general_ci' +
        '&sslMode=VERIFY_IDENTITY&useCursorFetch=true',
    )
  })

  it('composes PostgreSQL URL with SSL mode', () => {
    const url = buildJdbcUrl(
      'postgres',
      fields('postgres', {
        host: 'pg.example.com',
        port: '5432',
        database: 'analytics',
        sslMode: 'require',
        params: [{ key: 'ApplicationName', value: 'go-datax' }],
      }),
    )

    expect(url).toBe(
      'jdbc:postgresql://pg.example.com:5432/analytics?sslmode=require&ApplicationName=go-datax',
    )
  })

  it('composes SQL Server URL with semicolon parameters', () => {
    const url = buildJdbcUrl(
      'mssql',
      fields('mssql', {
        host: 'sql.example.com',
        port: '1433',
        database: 'warehouse',
        sslMode: 'require',
        params: [{ key: 'applicationName', value: 'go-datax' }],
      }),
    )

    expect(url).toBe(
      'jdbc:sqlserver://sql.example.com:1433;DatabaseName=warehouse' +
        ';encrypt=true;trustServerCertificate=true;applicationName=go-datax',
    )
  })

  it('composes Oracle service URL', () => {
    expect(
      buildJdbcUrl(
        'oracle',
        fields('oracle', {
          host: 'oracle.example.com',
          port: '1521',
          database: 'ORCLPDB1',
        }),
      ),
    ).toBe('jdbc:oracle:thin:@//oracle.example.com:1521/ORCLPDB1')
  })

  it('composes SQLite file URL', () => {
    expect(
      buildJdbcUrl('sqlite', fields('sqlite', { path: 'D:/data/app.db' })),
    ).toBe('jdbc:sqlite:D:/data/app.db')
  })
})

describe('parseJdbcUrl', () => {
  it('round-trips MySQL connection settings', () => {
    const parsed = parseJdbcUrl(
      'mysql',
      'jdbc:mysql://db.example.com:3307/orders' +
        '?characterEncoding=UTF-8&connectionCollation=utf8mb4_general_ci' +
        '&sslMode=VERIFY_IDENTITY&useCursorFetch=true',
    )

    expect(parsed).toEqual({
      host: 'db.example.com',
      port: '3307',
      database: 'orders',
      path: '',
      charset: 'utf8mb4',
      sslMode: 'verify',
      params: [{ key: 'useCursorFetch', value: 'true' }],
    })
  })

  it('parses SQL Server database and trust options', () => {
    const parsed = parseJdbcUrl(
      'mssql',
      'jdbc:sqlserver://sql.example.com:1433;DatabaseName=warehouse' +
        ';encrypt=true;trustServerCertificate=true;applicationName=go-datax',
    )

    expect(parsed?.host).toBe('sql.example.com')
    expect(parsed?.port).toBe('1433')
    expect(parsed?.database).toBe('warehouse')
    expect(parsed?.sslMode).toBe('require')
    expect(parsed?.params).toEqual([{ key: 'applicationName', value: 'go-datax' }])
  })

  it('parses Oracle service names and SQLite paths', () => {
    const oracle = parseJdbcUrl(
      'oracle',
      'jdbc:oracle:thin:@//oracle.example.com:1521/ORCLPDB1',
    )
    expect(oracle?.database).toBe('ORCLPDB1')
    expect(oracle?.host).toBe('oracle.example.com')

    const sqlite = parseJdbcUrl('sqlite', 'jdbc:sqlite:D:/data/app.db')
    expect(sqlite?.path).toBe('D:/data/app.db')
  })

  it('returns null for non-standard URLs so editing can fall back to raw text', () => {
    expect(parseJdbcUrl('mssql', 'jdbc:jtds:sqlserver://host:1433;DatabaseName=db')).toBeNull()
    expect(parseJdbcUrl('oracle', 'jdbc:oracle:thin:@host:1521:SID')).toBeNull()
    expect(parseJdbcUrl('mysql', 'jdbc:postgresql://host:5432/db')).toBeNull()
  })
})

describe('validateConnectionParams', () => {
  it('rejects duplicate and malformed parameter names', () => {
    expect(
      validateConnectionParams('mysql', [
        { key: 'useSSL', value: 'false' },
        { key: 'useSSL', value: 'true' },
      ]),
    ).toBe('连接参数“useSSL”重复')

    expect(validateConnectionParams('mysql', [{ key: 'bad key', value: 'x' }])).toContain(
      '只能包含字母',
    )
  })

  it('rejects semicolons in SQL Server parameter values', () => {
    expect(
      validateConnectionParams('mssql', [{ key: 'applicationName', value: 'a;b' }]),
    ).toContain('不能包含分号或等号')
  })
})
