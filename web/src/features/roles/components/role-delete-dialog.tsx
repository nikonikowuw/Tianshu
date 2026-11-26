import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { useDeleteRoleMutation } from '../queries'
import type { RoleItem } from '../types'

type RoleDeleteDialogProps = {
  role: RoleItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function RoleDeleteDialog({
  role,
  open,
  onOpenChange,
}: RoleDeleteDialogProps) {
  const { t } = useTranslation('roles')
  const { t: tCommon } = useTranslation('common')
  const deleteMutation = useDeleteRoleMutation()

  const handleConfirm = async () => {
    if (!role) return
    try {
      await deleteMutation.mutateAsync(role.id)
      toast.success(t('toast.deleted'))
      onOpenChange(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      toast.error(apiErr.message)
    }
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && deleteMutation.isPending) return
        onOpenChange(nextOpen)
      }}
      destructive
      isLoading={deleteMutation.isPending}
      title={t('deleteDialog.title')}
      desc={
        role
          ? t('deleteDialog.description', {
              name: role.name,
              code: role.code,
            })
          : ''
      }
      cancelBtnText={tCommon('cancel')}
      confirmText={t('deleteDialog.confirm')}
      handleConfirm={() => void handleConfirm()}
    />
  )
}
