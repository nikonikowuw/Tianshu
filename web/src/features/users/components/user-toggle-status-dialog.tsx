import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { useUpdateUserMutation } from '../queries'
import type { UserItem } from '../types'

type UserToggleStatusDialogProps = {
  user: UserItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function UserToggleStatusDialog({
  user,
  open,
  onOpenChange,
}: UserToggleStatusDialogProps) {
  const { t } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const mutation = useUpdateUserMutation()

  const isDisabling = user?.status === 'active'
  const nextStatus = isDisabling ? 'disabled' : 'active'

  const handleConfirm = async () => {
    if (!user) return
    try {
      await mutation.mutateAsync({
        id: user.id,
        payload: { status: nextStatus },
      })
      toast.success(isDisabling ? t('toast.disabled') : t('toast.enabled'))
      onOpenChange(false)
    } catch (err) {
      toast.error(formatServerErrorMessage(err))
    }
  }

  return (
    <ConfirmDialog
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen && mutation.isPending) return
        onOpenChange(nextOpen)
      }}
      destructive={isDisabling}
      isLoading={mutation.isPending}
      title={
        isDisabling
          ? t('toggleStatusDialog.disableTitle')
          : t('toggleStatusDialog.enableTitle')
      }
      desc={
        user
          ? isDisabling
            ? t('toggleStatusDialog.disableDescription', {
                username: user.username,
              })
            : t('toggleStatusDialog.enableDescription', {
                username: user.username,
              })
          : ''
      }
      cancelBtnText={tCommon('cancel')}
      confirmText={
        isDisabling
          ? t('toggleStatusDialog.disableConfirm')
          : t('toggleStatusDialog.enableConfirm')
      }
      handleConfirm={() => void handleConfirm()}
    />
  )
}
