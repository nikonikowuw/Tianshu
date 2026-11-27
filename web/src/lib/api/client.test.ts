import {
  AxiosError,
  AxiosHeaders,
  type InternalAxiosRequestConfig,
} from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient, configureApiClient } from './client'
import { ApiError } from './error'

const originalAdapter = apiClient.defaults.adapter

function errorAdapter(status: number, code: string, message = 'Error') {
  return async (config: InternalAxiosRequestConfig) => {
    throw new AxiosError(
      'Request failed',
      'ERR_BAD_RESPONSE',
      config,
      undefined,
      {
        status,
        statusText: String(status),
        data: { code, message },
        headers: new AxiosHeaders(),
        config,
      }
    )
  }
}

describe('apiClient 401 and 403 handling', () => {
  afterEach(() => {
    apiClient.defaults.adapter = originalAdapter
    configureApiClient({
      getAccessToken: () => '',
      refreshAccessToken: async () => null,
      onAccountDisabled: () => {},
    })
  })

  it('leaves public login failures to the form instead of logging out globally', async () => {
    const onAccountDisabled = vi.fn()
    const onSessionExpired = vi.fn()
    const refreshAccessToken = vi.fn(async () => null)
    configureApiClient({
      getAccessToken: () => '',
      refreshAccessToken,
      onAccountDisabled,
      onSessionExpired,
    })
    apiClient.defaults.adapter = errorAdapter(
      403,
      'AUTH_ACCOUNT_DISABLED',
      'Forbidden'
    )

    await expect(apiClient.post('/auth/login', {})).rejects.toBeInstanceOf(
      ApiError
    )
    expect(onAccountDisabled).not.toHaveBeenCalled()

    apiClient.defaults.adapter = errorAdapter(
      401,
      'AUTH_INVALID_CREDENTIALS',
      'Unauthorized'
    )
    await expect(apiClient.post('/auth/login', {})).rejects.toBeInstanceOf(
      ApiError
    )
    expect(refreshAccessToken).not.toHaveBeenCalled()
    expect(onSessionExpired).not.toHaveBeenCalled()
  })

  it('still clears the session when a protected endpoint reports a disabled account', async () => {
    const onAccountDisabled = vi.fn()
    configureApiClient({
      getAccessToken: () => 'token',
      refreshAccessToken: async () => null,
      onAccountDisabled,
    })
    apiClient.defaults.adapter = errorAdapter(
      403,
      'AUTH_ACCOUNT_DISABLED',
      'Forbidden'
    )

    await expect(apiClient.post('/users', {})).rejects.toBeInstanceOf(ApiError)
    expect(onAccountDisabled).toHaveBeenCalledTimes(1)
  })

  it('invokes onSessionExpired when a protected endpoint gets 401 and refresh fails', async () => {
    const refreshAccessToken = vi.fn(async () => null)
    const onSessionExpired = vi.fn()
    configureApiClient({
      getAccessToken: () => 'revoked-token',
      refreshAccessToken,
      onAccountDisabled: () => {},
      onSessionExpired,
    })
    apiClient.defaults.adapter = errorAdapter(
      401,
      'AUTH_UNAUTHORIZED',
      'Unauthorized'
    )

    await expect(apiClient.get('/profile/sessions')).rejects.toBeInstanceOf(
      ApiError
    )
    expect(refreshAccessToken).toHaveBeenCalledTimes(1)
    expect(onSessionExpired).toHaveBeenCalledTimes(1)
  })

  it('replays a protected 401 request after refresh succeeds without invoking onSessionExpired', async () => {
    let token = 'stale-token'
    const refreshAccessToken = vi.fn(async () => {
      token = 'fresh-token'
      return token
    })
    const onSessionExpired = vi.fn()
    configureApiClient({
      getAccessToken: () => token,
      refreshAccessToken,
      onAccountDisabled: () => {},
      onSessionExpired,
    })

    let attempts = 0
    apiClient.defaults.adapter = async (config) => {
      attempts += 1
      if (attempts === 1) {
        throw new AxiosError(
          'Unauthorized',
          'ERR_BAD_RESPONSE',
          config,
          undefined,
          {
            status: 401,
            statusText: 'Unauthorized',
            data: { code: 'AUTH_UNAUTHORIZED', message: 'Unauthorized' },
            headers: new AxiosHeaders(),
            config,
          }
        )
      }
      return {
        data: { ok: true, auth: config.headers.Authorization },
        status: 200,
        statusText: 'OK',
        headers: new AxiosHeaders(),
        config,
      }
    }

    const res = await apiClient.get<{ ok: boolean; auth: string }>('/profile')
    expect(res.data).toEqual({ ok: true, auth: 'Bearer fresh-token' })
    expect(refreshAccessToken).toHaveBeenCalledTimes(1)
    expect(onSessionExpired).not.toHaveBeenCalled()
  })

  it('skips refresh and onSessionExpired when /auth/logout returns 401', async () => {
    const refreshAccessToken = vi.fn(async () => null)
    const onSessionExpired = vi.fn()
    configureApiClient({
      getAccessToken: () => 'expired-token',
      refreshAccessToken,
      onAccountDisabled: () => {},
      onSessionExpired,
    })
    apiClient.defaults.adapter = errorAdapter(
      401,
      'AUTH_UNAUTHORIZED',
      'Unauthorized'
    )

    await expect(apiClient.post('/auth/logout')).rejects.toBeInstanceOf(
      ApiError
    )
    expect(refreshAccessToken).not.toHaveBeenCalled()
    expect(onSessionExpired).not.toHaveBeenCalled()
  })
})
