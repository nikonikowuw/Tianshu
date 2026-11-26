import {
  AxiosError,
  AxiosHeaders,
  type InternalAxiosRequestConfig,
} from 'axios'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { apiClient, configureApiClient } from './client'
import { ApiError } from './error'

const originalAdapter = apiClient.defaults.adapter

function forbiddenAdapter(code: string) {
  return async (config: InternalAxiosRequestConfig) => {
    throw new AxiosError(
      'Request failed',
      'ERR_BAD_RESPONSE',
      config,
      undefined,
      {
        status: 403,
        statusText: 'Forbidden',
        data: { code, message: 'Forbidden' },
        headers: new AxiosHeaders(),
        config,
      }
    )
  }
}

describe('apiClient 403 handling', () => {
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
    configureApiClient({
      getAccessToken: () => '',
      refreshAccessToken: async () => null,
      onAccountDisabled,
    })
    apiClient.defaults.adapter = forbiddenAdapter('AUTH_ACCOUNT_DISABLED')

    await expect(apiClient.post('/auth/login', {})).rejects.toBeInstanceOf(
      ApiError
    )
    expect(onAccountDisabled).not.toHaveBeenCalled()
  })

  it('still clears the session when a protected endpoint reports a disabled account', async () => {
    const onAccountDisabled = vi.fn()
    configureApiClient({
      getAccessToken: () => 'token',
      refreshAccessToken: async () => null,
      onAccountDisabled,
    })
    apiClient.defaults.adapter = forbiddenAdapter('AUTH_ACCOUNT_DISABLED')

    await expect(apiClient.post('/users', {})).rejects.toBeInstanceOf(ApiError)
    expect(onAccountDisabled).toHaveBeenCalledTimes(1)
  })
})
