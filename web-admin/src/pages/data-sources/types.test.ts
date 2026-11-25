import { describe, expect, it } from 'vitest'
import { isJdbcDataSourceType } from '../../api/dataSources'
import { DATA_SOURCE_TYPES, typeMeta } from './types'

describe('filesystem data source metadata', () => {
  it('is not a JDBC source', () => {
    expect(isJdbcDataSourceType('filesystem')).toBe(false)
    expect(isJdbcDataSourceType('mysql')).toBe(true)
  })

  it('is present in the picker with a file category', () => {
    const meta = typeMeta('filesystem')
    expect(meta.label).toBe('文件系统')
    expect(meta.category).toBe('file')
    expect(DATA_SOURCE_TYPES.some((item) => item.value === 'filesystem')).toBe(true)
  })
})
