import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { useDeleteFileMutation } from '../queries'
import type { FileItem } from '../types'

type FileDeleteDialogProps = {
  file: FileItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
  onDeleted?: (fileId: string) => void
}

export function FileDeleteDialog({
  file,
  open,
  onOpenChange,
  onDeleted,
}: FileDeleteDialogProps) {
  const { t } = useTranslation('files')
  const { t: tCommon } = useTranslation('common')
  const deleteMutation = useDeleteFileMutation()

  const handleConfirm = async () => {
    if (!file) return
    try {
      await deleteMutation.mutateAsync(file.id)
      toast.success(t('toast.deleted'))
      onDeleted?.(file.id)
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
        file
          ? t('deleteDialog.description', {
              name: file.originalName,
            })
          : ''
      }
      cancelBtnText={tCommon('cancel')}
      confirmText={t('deleteDialog.confirm')}
      confirmTestId='file-delete-confirm'
      handleConfirm={() => void handleConfirm()}
    />
  )
}
