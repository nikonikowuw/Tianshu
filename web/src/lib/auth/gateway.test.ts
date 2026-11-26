import { describe, expect, it } from 'vitest'
import { isSafeRedirect, sanitizeRedirect } from './gateway'

describe('redirect safety validation', () => {
  it('accepts internal relative paths', () => {
    expect(isSafeRedirect('/')).toBe(true)
    expect(isSafeRedirect('/users?page=2')).toBe(true)
    expect(sanitizeRedirect('/roles')).toBe('/roles')
  })

  it('rejects protocol-relative and external URLs', () => {
    expect(isSafeRedirect('//evil.com')).toBe(false)
    expect(isSafeRedirect('https://evil.com')).toBe(false)
    expect(isSafeRedirect('javascript:alert(1)')).toBe(false)
    expect(sanitizeRedirect('https://evil.com')).toBe('/')
    expect(sanitizeRedirect(undefined)).toBe('/')
  })
})
