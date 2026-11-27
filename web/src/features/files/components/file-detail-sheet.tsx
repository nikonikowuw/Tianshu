import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import {
  AlertCircle,
  Copy,
  Download,
  FileText,
  RefreshCw,
  Trash2,
} from 'lucide-react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Skeleton } from '@/components/ui/skeleton'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { fileSignedUrlQueryOptions } from '../queries'
import type { FileItem } from '../types'
import {
  formatBytes,
  formatFileTimestamp,
  formatMimeBadge,
} from './format-bytes'

type FileDetailSheetProps = {
  file: FileItem | null
  open: boolean
  canDelete: boolean
  onOpenChange: (open: boolean) => void
  onDownload: (file: FileItem) => void
  onDelete: (file: FileItem) => void
}

export function FileDetailSheet({
  file,
  open,
  canDelete,
  onOpenChange,
  onDownload,
  onDelete,
}: FileDetailSheetProps) {
  const { t, i18n } = useTranslation('files')
  const [imgErrorId, setImgErrorId] = useState<string | null>(null)

  const shouldLoadPreview = Boolean(open && file && file.isImage)
  const {
    data: previewUrlData,
    isLoading: isPreviewLoading,
    isError: isPreviewError,
    refetch: refetchPreview,
  } = useQuery(
    fileSignedUrlQueryOptions(file?.id ?? '', 'inline', shouldLoadPreview)
  )

  const imageBroken = Boolean(file && imgErrorId === file.id)

  const copyText = async (value: string) => {
    if (!value) return
    try {
      await navigator.clipboard.writeText(value)
      toast.success(t('detail.copied'))
    } catch {
      // Clipboard API may be unavailable in restricted browser contexts.
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(nextOpen) => {
        if (!nextOpen) setImgErrorId(null)
        onOpenChange(nextOpen)
      }}
    >
      <SheetContent className='flex h-full w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-lg'>
        <SheetHeader className='border-b px-6 py-4'>
          <SheetTitle data-testid='file-detail-sheet-title'>
            {t('detail.title')}
          </SheetTitle>
          <SheetDescription>{t('detail.description')}</SheetDescription>
        </SheetHeader>

        {file && (
          <>
            <ScrollArea className='min-h-0 flex-1 px-6 py-4'>
              <div className='space-y-5'>
                <div className='space-y-2'>
                  <p className='text-xs font-medium text-muted-foreground'>
                    {t('detail.preview')}
                  </p>
                  {file.isImage ? (
                    <div className='flex min-h-48 items-center justify-center overflow-hidden rounded-lg border bg-muted/30 p-3'>
                      {isPreviewLoading ? (
                        <Skeleton className='h-44 w-full rounded-md' />
                      ) : isPreviewError || imageBroken || !previewUrlData ? (
                        <div className='flex flex-col items-center gap-2 py-6 text-center text-muted-foreground'>
                          <AlertCircle className='size-6' />
                          <p className='max-w-xs text-xs'>
                            {t('error.previewFailed')}
                          </p>
                          <Button
                            type='button'
                            variant='outline'
                            size='sm'
                            onClick={() => {
                              setImgErrorId(null)
                              void refetchPreview()
                            }}
                          >
                            <RefreshCw className='size-3.5' />
                            {t('error.retry')}
                          </Button>
                        </div>
                      ) : (
                        <img
                          src={previewUrlData.url}
                          alt={file.originalName}
                          className='max-h-72 max-w-full rounded object-contain'
                          onError={() => setImgErrorId(file.id)}
                        />
                      )}
                    </div>
                  ) : (
                    <div className='flex items-start gap-3 rounded-lg border bg-muted/30 p-4'>
                      <div className='flex size-10 shrink-0 items-center justify-center rounded-md border bg-background text-muted-foreground'>
                        <FileText className='size-5' />
                      </div>
                      <div className='space-y-1 text-xs text-muted-foreground'>
                        <div className='flex items-center gap-2'>
                          <Badge variant='outline' className='font-mono'>
                            {formatMimeBadge(file.mimeType)}
                          </Badge>
                          <span className='font-medium text-foreground'>
                            {formatBytes(file.sizeBytes, i18n.language)}
                          </span>
                        </div>
                        <p data-testid='file-document-notice'>
                          {t('detail.documentPreviewNotice')}
                        </p>
                      </div>
                    </div>
                  )}
                </div>

                <Separator />

                <dl className='grid grid-cols-1 gap-3 text-sm sm:grid-cols-2'>
                  <div className='space-y-1 sm:col-span-2'>
                    <dt className='text-xs font-medium text-muted-foreground'>
                      {t('detail.originalName')}
                    </dt>
                    <dd className='text-sm font-medium break-all'>
                      {file.originalName}
                    </dd>
                  </div>
                  <DetailField
                    label={t('detail.mimeType')}
                    value={file.mimeType}
                    mono
                  />
                  <DetailField
                    label={t('detail.category')}
                    value={
                      file.category === 'image'
                        ? t('categories.image')
                        : t('categories.document')
                    }
                  />
                  <DetailField
                    label={t('detail.size')}
                    value={`${formatBytes(file.sizeBytes, i18n.language)} (${t('detail.bytesUnit', { bytes: new Intl.NumberFormat(i18n.language).format(file.sizeBytes) })})`}
                  />
                  <DetailField
                    label={t('detail.driver')}
                    value={
                      file.storageDriver === 's3'
                        ? t('drivers.s3')
                        : t('drivers.local')
                    }
                  />
                  <DetailField
                    label={t('detail.uploader')}
                    value={file.uploader?.username || t('empty.systemUploader')}
                    mono
                  />
                  <DetailField
                    label={t('detail.createdAt')}
                    value={formatFileTimestamp(file.createdAt, i18n.language)}
                    mono
                  />
                </dl>

                <div className='space-y-1.5'>
                  <div className='flex items-center justify-between'>
                    <p className='text-xs font-medium text-muted-foreground'>
                      {t('detail.sha256')}
                    </p>
                    <Button
                      type='button'
                      variant='ghost'
                      size='sm'
                      className='h-6 px-2 text-xs'
                      onClick={() => void copyText(file.sha256)}
                    >
                      <Copy className='me-1 size-3' />
                      {t('detail.copySha256')}
                    </Button>
                  </div>
                  <p className='rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs break-all text-foreground'>
                    {file.sha256}
                  </p>
                </div>
              </div>
            </ScrollArea>

            <SheetFooter className='flex-row items-center justify-between gap-2 border-t px-6 py-4'>
              <PermissionGuard allowed={canDelete}>
                <Button
                  type='button'
                  variant='destructive'
                  onClick={() => onDelete(file)}
                >
                  <Trash2 className='size-4' />
                  {t('detail.delete')}
                </Button>
              </PermissionGuard>
              <Button
                type='button'
                data-testid='file-detail-download'
                className='ms-auto'
                onClick={() => onDownload(file)}
              >
                <Download className='size-4' />
                {t('detail.download')}
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  )
}

function DetailField({
  label,
  value,
  mono = false,
}: {
  label: string
  value: string
  mono?: boolean
}) {
  return (
    <div className='space-y-1'>
      <dt className='text-xs font-medium text-muted-foreground'>{label}</dt>
      <dd
        className={mono ? 'font-mono text-xs break-all' : 'text-sm break-words'}
      >
        {value}
      </dd>
    </div>
  )
}
