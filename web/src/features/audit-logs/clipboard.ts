import type { TFunction } from 'i18next'
import { toast } from 'sonner'

export async function copyAuditText(
  value: string,
  t: TFunction<'audit'>
): Promise<boolean> {
  if (!value) return false
  if (typeof navigator === 'undefined' || !navigator.clipboard?.writeText) {
    toast.error(t('copyFailed'))
    return false
  }
  try {
    await navigator.clipboard.writeText(value)
    toast.success(t('copied'))
    return true
  } catch {
    toast.error(t('copyFailed'))
    return false
  }
}
