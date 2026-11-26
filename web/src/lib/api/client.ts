import axios, { type InternalAxiosRequestConfig } from 'axios'
import { getCurrentLanguage } from '@/i18n/index'
import { ApiError, normalizeApiError } from './error'

const PUBLIC_PATHS = [
  '/setup/status',
  '/setup/init',
  '/auth/login',
  '/auth/refresh',
]

type RetriableConfig = InternalAxiosRequestConfig & {
  _retried?: boolean
}

type ApiClientHooks = {
  getAccessToken: () => string
  refreshAccessToken: () => Promise<string | null>
  onAccountDisabled: () => void
  onForbiddenRefresh?: () => Promise<void>
}

let hooks: ApiClientHooks = {
  getAccessToken: () => '',
  refreshAccessToken: async () => null,
  onAccountDisabled: () => {},
}

export function configureApiClient(next: ApiClientHooks): void {
  hooks = next
}

export const apiClient = axios.create({
  baseURL: import.meta.env.VITE_API_BASE_URL ?? '/api/v1',
  withCredentials: true,
  timeout: 15_000,
  headers: {
    Accept: 'application/json',
  },
})

function isPublicEndpoint(url?: string): boolean {
  if (!url) return false
  return PUBLIC_PATHS.some(
    (prefix) => url === prefix || url.startsWith(`${prefix}?`)
  )
}

apiClient.interceptors.request.use((config) => {
  config.headers['Accept-Language'] = getCurrentLanguage()

  if (!isPublicEndpoint(config.url)) {
    const token = hooks.getAccessToken()
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
  }
  return config
})

apiClient.interceptors.response.use(
  (response) => response,
  async (rawError: unknown) => {
    const normalized = normalizeApiError(rawError)

    if (axios.isAxiosError(rawError) && rawError.config) {
      const original = rawError.config as RetriableConfig

      if (
        normalized.status === 401 &&
        !original._retried &&
        !isPublicEndpoint(original.url)
      ) {
        original._retried = true
        const nextToken = await hooks.refreshAccessToken()
        if (nextToken) {
          original.headers.Authorization = `Bearer ${nextToken}`
          return apiClient.request(original)
        }
      }

      // Public auth endpoints own their 401/403 feedback inline: a global
      // logout/redirect here would race the form banner and duplicate the toast.
      if (normalized.status === 403 && !isPublicEndpoint(original.url)) {
        if (normalized.code === 'AUTH_ACCOUNT_DISABLED') {
          hooks.onAccountDisabled()
        } else if (
          normalized.code === 'AUTH_FORBIDDEN' &&
          original.url !== '/auth/me' &&
          hooks.onForbiddenRefresh
        ) {
          await hooks.onForbiddenRefresh()
        }
      }
    }

    return Promise.reject(
      normalized instanceof ApiError ? normalized : normalizeApiError(rawError)
    )
  }
)
