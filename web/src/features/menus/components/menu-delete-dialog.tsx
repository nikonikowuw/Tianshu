import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { formatServerErrorMessage } from '@/lib/handle-server-error'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { useDeleteMenuMutation } from '../queries'
import type { MenuItem } from '../types'

type MenuDeleteDialogProps = {
  menu: MenuItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function MenuDeleteDialog({
  menu,
  open,
  onOpenChange,
}: MenuDeleteDialogProps) {
  const { t } = useTranslation('menus')
  const { t: tCommon } = useTranslation('common')
  const deleteMutation = useDeleteMenuMutation()

  const handleConfirm = async () => {
    if (!menu) return
    try {
      await deleteMutation.mutateAsync(menu.id)
      toast.success(t('toast.deleted'))
      onOpenChange(false)
    } catch (err) {
      toast.error(formatServerErrorMessage(err))
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
        menu
          ? t('deleteDialog.description', {
              title: menu.title,
            })
          : ''
      }
      cancelBtnText={tCommon('cancel')}
      confirmText={t('deleteDialog.confirm')}
      handleConfirm={() => void handleConfirm()}
    />
  )
}
