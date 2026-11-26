import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { useDeleteUserMutation } from '../queries'
import type { UserItem } from '../types'

type UserDeleteDialogProps = {
  user: UserItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserDeleteDialog({
  user,
  open,
  onOpenChange,
}: UserDeleteDialogProps) {
  const { t } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const deleteMutation = useDeleteUserMutation()

  const handleConfirm = async () => {
    if (!user) return
    try {
      await deleteMutation.mutateAsync(user.id)
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
        user
          ? t('deleteDialog.description', {
              username: user.username,
            })
          : ''
      }
      cancelBtnText={tCommon('cancel')}
      confirmText={t('deleteDialog.confirm')}
      handleConfirm={() => void handleConfirm()}
    />
  )
}
