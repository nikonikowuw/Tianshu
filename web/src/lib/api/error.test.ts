import { AxiosError, type AxiosResponse } from 'axios'
import { describe, expect, it } from 'vitest'
import { ApiError, normalizeApiError } from './error'
import type { ApiErrorBody } from './types'

describe('normalizeApiError', () => {
  it('extracts X-Request-Id header and 422 field errors', () => {
    const response = {
      status: 422,
      statusText: 'Unprocessable Entity',
      headers: { 'x-request-id': 'req-12345678' },
      config: {} as never,
      data: {
        code: 'VALIDATION_FAILED',
        message: 'Validation failed',
        errors: [
          { field: 'email', message: 'Invalid email' },
          { field: 'email', message: 'Duplicate should be ignored' },
        ],
      } satisfies ApiErrorBody,
    } as AxiosResponse<ApiErrorBody>

    const axiosErr = new AxiosError<ApiErrorBody>(
      'Validation failed',
      'ERR_BAD_REQUEST',
      undefined,
      undefined,
      response
    )

    const normalized = normalizeApiError(axiosErr)
    expect(normalized).toBeInstanceOf(ApiError)
    expect(normalized.status).toBe(422)
    expect(normalized.code).toBe('VALIDATION_FAILED')
    expect(normalized.requestId).toBe('req-12345678')
    expect(normalized.fieldErrors).toEqual({ email: 'Invalid email' })
  })

  it('normalizes network errors without response', () => {
    const axiosErr = new AxiosError('Network Error')
    const normalized = normalizeApiError(axiosErr)
    expect(normalized.status).toBe(0)
    expect(normalized.code).toBe('NETWORK_ERROR')
    expect(normalized.requestId).toBe('')
  })
})
