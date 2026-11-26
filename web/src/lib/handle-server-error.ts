import { toast } from 'sonner'
import i18n from '@/i18n/index'
import { normalizeApiError } from '@/lib/api/error'

export function handleServerError(error: unknown): void {
  const apiError = normalizeApiError(error)

  if (apiError.status === 0) {
    toast.error(i18n.t('errors:network'))
    return
  }

  if (apiError.status >= 500) {
    toast.error(
      apiError.requestId
        ? i18n.t('errors:serverError', { requestId: apiError.requestId })
        : i18n.t('errors:generic')
    )
    return
  }

  toast.error(apiError.message || i18n.t('errors:generic'))
}
