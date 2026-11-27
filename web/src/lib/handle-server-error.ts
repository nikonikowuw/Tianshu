import { toast } from 'sonner'
import i18n from '@/i18n/index'
import { normalizeApiError } from '@/lib/api/error'

/**
 * Global fallback presenter for API failures.
 *
 * Callers with an inline error surface (such as auth forms) should inspect the
 * normalized error first—especially 429 with `retryAfter`, which needs a
 * persistent countdown banner—and only fall back here for unhandled errors.
 * Any 429 reaching this fallback uses a deduplicated toast id to avoid spam.
 */
export function formatServerErrorMessage(error: unknown): string {
  const apiError = normalizeApiError(error)

  if (apiError.status === 0) {
    return i18n.t('errors:network')
  }

  if (apiError.status === 429) {
    return i18n.t('errors:rateLimited')
  }

  if (apiError.status >= 500) {
    return apiError.requestId
      ? i18n.t('errors:serverError', { requestId: apiError.requestId })
      : i18n.t('errors:generic')
  }

  return apiError.message || i18n.t('errors:generic')
}

export function handleServerError(error: unknown): void {
  const apiError = normalizeApiError(error)

  if (apiError.status === 429) {
    toast.error(formatServerErrorMessage(apiError), { id: 'rate-limited' })
    return
  }

  toast.error(formatServerErrorMessage(apiError))
}
