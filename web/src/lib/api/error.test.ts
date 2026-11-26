import { AxiosError, AxiosHeaders } from 'axios'
import { describe, expect, it } from 'vitest'
import { ApiError, normalizeApiError } from './error'

function axiosError(params: {
  status: number
  data?: unknown
  headers?: Record<string, string>
}) {
  const headers = new AxiosHeaders(params.headers)
  return new AxiosError(
    'Request failed',
    'ERR_BAD_RESPONSE',
    undefined,
    undefined,
    {
      status: params.status,
      statusText: '',
      data: params.data,
      headers,
      config: { headers: new AxiosHeaders() },
    }
  )
}

describe('normalizeApiError', () => {
  it('keeps the Retry-After window from 429 responses', () => {
    const error = normalizeApiError(
      axiosError({
        status: 429,
        data: { code: 'RATE_LIMITED', message: 'Too many requests' },
        headers: { 'retry-after': '42' },
      })
    )

    expect(error.status).toBe(429)
    expect(error.retryAfter).toBe(42)
  })

  it('rounds fractional Retry-After values up and ignores invalid ones', () => {
    const rounded = normalizeApiError(
      axiosError({
        status: 429,
        data: { code: 'RATE_LIMITED', message: 'Too many requests' },
        headers: { 'retry-after': '1.2' },
      })
    )
    const invalid = normalizeApiError(
      axiosError({
        status: 429,
        data: { code: 'RATE_LIMITED', message: 'Too many requests' },
        headers: { 'retry-after': 'not-a-number' },
      })
    )

    expect(rounded.retryAfter).toBe(2)
    expect(invalid.retryAfter).toBe(0)
  })

  it('extracts X-Request-Id header and deduplicates 422 field errors', () => {
    const error = normalizeApiError(
      axiosError({
        status: 422,
        headers: { 'x-request-id': 'req-12345678' },
        data: {
          code: 'VALIDATION_FAILED',
          message: 'Validation failed',
          errors: [
            { field: 'email', message: 'Invalid email' },
            { field: 'email', message: 'Duplicate should be ignored' },
            { field: 'password', message: 'Too long' },
          ],
        },
      })
    )

    expect(error).toBeInstanceOf(ApiError)
    expect(error.status).toBe(422)
    expect(error.code).toBe('VALIDATION_FAILED')
    expect(error.requestId).toBe('req-12345678')
    expect(error.fieldErrors).toEqual({
      email: 'Invalid email',
      password: 'Too long',
    })
    expect(error.retryAfter).toBe(0)
  })

  it('normalizes network errors without response', () => {
    const error = normalizeApiError(new AxiosError('Network Error'))

    expect(error.status).toBe(0)
    expect(error.code).toBe('NETWORK_ERROR')
    expect(error.requestId).toBe('')
    expect(error.retryAfter).toBe(0)
  })

  it('passes through an existing ApiError instance', () => {
    const original = new ApiError({
      status: 429,
      code: 'RATE_LIMITED',
      message: 'Too many requests',
      retryAfter: 5,
    })

    expect(normalizeApiError(original)).toBe(original)
  })
})
