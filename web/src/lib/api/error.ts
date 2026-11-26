import { isAxiosError } from 'axios'
import type { ApiErrorBody } from './types'

export class ApiError extends Error {
  readonly status: number
  readonly code: string
  readonly requestId: string
  readonly fieldErrors: Record<string, string>
  /** Seconds the server asks the client to wait before retrying (429 Retry-After). */
  readonly retryAfter: number

  constructor(params: {
    status: number
    code: string
    message: string
    requestId?: string
    fieldErrors?: Record<string, string>
    retryAfter?: number
  }) {
    super(params.message)
    this.name = 'ApiError'
    this.status = params.status
    this.code = params.code
    this.requestId = params.requestId ?? ''
    this.fieldErrors = params.fieldErrors ?? {}
    this.retryAfter = params.retryAfter ?? 0
  }
}

/**
 * Parse the Retry-After header. Only the delta-seconds form is supported:
 * the API rate limiter always emits an integer number of seconds.
 */
function parseRetryAfter(raw: unknown): number {
  if (typeof raw !== 'string' && typeof raw !== 'number') return 0
  const seconds = Number(raw)
  if (!Number.isFinite(seconds) || seconds <= 0) return 0
  return Math.ceil(seconds)
}

export function normalizeApiError(error: unknown): ApiError {
  if (error instanceof ApiError) {
    return error
  }

  if (isAxiosError<ApiErrorBody>(error)) {
    const status = error.response?.status ?? 0
    const headers = error.response?.headers
    const rawReqId =
      typeof headers?.['x-request-id'] === 'string'
        ? headers['x-request-id']
        : ''
    const data = error.response?.data

    const fieldErrors: Record<string, string> = {}
    if (status === 422 && Array.isArray(data?.errors)) {
      for (const item of data.errors) {
        if (item.field && !fieldErrors[item.field]) {
          fieldErrors[item.field] = item.message
        }
      }
    }

    return new ApiError({
      status,
      code: data?.code ?? (status === 0 ? 'NETWORK_ERROR' : 'HTTP_ERROR'),
      message: data?.message ?? error.message ?? 'Request failed',
      requestId: rawReqId,
      fieldErrors,
      retryAfter: parseRetryAfter(headers?.['retry-after']),
    })
  }

  if (error instanceof Error) {
    return new ApiError({
      status: 0,
      code: 'UNKNOWN_ERROR',
      message: error.message,
    })
  }

  return new ApiError({
    status: 0,
    code: 'UNKNOWN_ERROR',
    message: 'Unexpected error',
  })
}
