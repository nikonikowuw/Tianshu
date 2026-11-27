import { useRef, useState, type ChangeEvent, type DragEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  CheckCircle2,
  FileUp,
  Loader2,
  Upload,
  X,
} from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { cn } from '@/lib/utils'
import { useRetryCountdown } from '@/hooks/use-retry-countdown'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  fileUploadPolicyQueryOptions,
  filesKeys,
  useUploadFileMutation,
} from '../queries'
import { formatBytes } from './format-bytes'

const FALLBACK_ACCEPTED_EXTENSIONS =
  '.jpg,.jpeg,.png,.webp,.gif,.pdf,.txt,.log,.md,.csv,.docx,.xlsx,.pptx'

type QueueItemStatus = 'pending' | 'uploading' | 'success' | 'error'

type UploadQueueItem = {
  id: string
  file: File
  status: QueueItemStatus
  progress: number
  errorMessage?: string
}

type FileUploadDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function FileUploadDialog({
  open,
  onOpenChange,
}: FileUploadDialogProps) {
  const { t, i18n } = useTranslation('files')
  const { t: tCommon } = useTranslation('common')
  const queryClient = useQueryClient()
  const fileInputRef = useRef<HTMLInputElement | null>(null)
  const abortRef = useRef<AbortController | null>(null)
  const uploadMutation = useUploadFileMutation()
  const { data: uploadPolicy } = useQuery(fileUploadPolicyQueryOptions())
  const {
    remaining: retrySeconds,
    isCoolingDown,
    start: startCooldown,
  } = useRetryCountdown()

  const [queue, setQueue] = useState<UploadQueueItem[]>([])
  const [isDragging, setIsDragging] = useState(false)
  const [isUploading, setIsUploading] = useState(false)
  const [bannerError, setBannerError] = useState<string | null>(null)

  const resetState = () => {
    setQueue([])
    setIsDragging(false)
    setBannerError(null)
  }

  const handleOpenChange = (nextOpen: boolean) => {
    if (!nextOpen) {
      abortRef.current?.abort()
      abortRef.current = null
      setIsUploading(false)
      resetState()
    }
    onOpenChange(nextOpen)
  }

  const limitLabel = uploadPolicy
    ? formatBytes(uploadPolicy.maxFileSizeBytes, i18n.language)
    : ''
  const acceptedExtensions =
    uploadPolicy && uploadPolicy.allowedExtensions.length > 0
      ? uploadPolicy.allowedExtensions.join(',')
      : FALLBACK_ACCEPTED_EXTENSIONS

  const enqueueFiles = (fileList: FileList | File[]) => {
    const incoming = Array.from(fileList)
    if (incoming.length === 0) return

    setBannerError(null)
    const nextItems: UploadQueueItem[] = incoming.map((file, idx) => {
      const oversized = uploadPolicy
        ? file.size > uploadPolicy.maxFileSizeBytes
        : false
      return {
        id: `${Date.now()}-${idx}-${file.name}-${file.size}`,
        file,
        status: oversized ? 'error' : 'pending',
        progress: 0,
        errorMessage: oversized
          ? t('uploadDialog.tooLargeHint', { size: limitLabel })
          : undefined,
      }
    })
    setQueue((prev) => [...prev, ...nextItems])
  }

  const handleFileChange = (e: ChangeEvent<HTMLInputElement>) => {
    if (e.target.files) {
      enqueueFiles(e.target.files)
    }
    e.target.value = ''
  }

  const handleDragOver = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    if (!isUploading) {
      setIsDragging(true)
    }
  }

  const handleDragLeave = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setIsDragging(false)
  }

  const handleDrop = (e: DragEvent<HTMLDivElement>) => {
    e.preventDefault()
    setIsDragging(false)
    if (isUploading) return
    if (e.dataTransfer.files?.length) {
      enqueueFiles(e.dataTransfer.files)
    }
  }

  const handleRemoveItem = (id: string) => {
    if (isUploading) return
    setQueue((prev) => prev.filter((item) => item.id !== id))
  }

  const handleClearFinished = () => {
    if (isUploading) return
    setQueue((prev) => prev.filter((item) => item.status !== 'success'))
  }

  const pendingItems = queue.filter((item) => item.status === 'pending')
  const hasFinishedItems = queue.some((item) => item.status === 'success')

  const handleStartUpload = async () => {
    if (isUploading || isCoolingDown || pendingItems.length === 0) return

    const controller = new AbortController()
    abortRef.current = controller
    setIsUploading(true)
    setBannerError(null)
    let uploadedCount = 0

    for (const item of pendingItems) {
      if (controller.signal.aborted) break
      setQueue((prev) =>
        prev.map((q) =>
          q.id === item.id
            ? {
                ...q,
                status: 'uploading',
                progress: 0,
                errorMessage: undefined,
              }
            : q
        )
      )

      try {
        await uploadMutation.mutateAsync({
          file: item.file,
          signal: controller.signal,
          onProgress: (percent) => {
            setQueue((prev) =>
              prev.map((q) =>
                q.id === item.id ? { ...q, progress: percent } : q
              )
            )
          },
        })
        uploadedCount += 1
        setQueue((prev) =>
          prev.map((q) =>
            q.id === item.id ? { ...q, status: 'success', progress: 100 } : q
          )
        )
      } catch (err) {
        if (controller.signal.aborted) break
        const apiErr = normalizeApiError(err)
        if (apiErr.status === 429) {
          const wait = apiErr.retryAfter > 0 ? apiErr.retryAfter : 15
          startCooldown(wait)
          setBannerError(apiErr.message)
          setQueue((prev) =>
            prev.map((q) =>
              q.id === item.id
                ? {
                    ...q,
                    status: 'pending',
                    progress: 0,
                    errorMessage: undefined,
                  }
                : q
            )
          )
          break
        }
        setQueue((prev) =>
          prev.map((q) =>
            q.id === item.id
              ? {
                  ...q,
                  status: 'error',
                  progress: 0,
                  errorMessage: apiErr.message,
                }
              : q
          )
        )
      }
    }

    abortRef.current = null
    setIsUploading(false)

    if (uploadedCount > 0) {
      await queryClient.invalidateQueries({ queryKey: filesKeys.lists })
      toast.success(t('toast.uploaded', { count: uploadedCount }))
    }
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className='sm:max-w-xl'>
        <DialogHeader>
          <DialogTitle>{t('uploadDialog.title')}</DialogTitle>
          <DialogDescription>{t('uploadDialog.description')}</DialogDescription>
        </DialogHeader>

        <div className='space-y-4'>
          {(bannerError || isCoolingDown) && (
            <Alert variant='destructive' role='alert'>
              <AlertCircle className='size-4' />
              <AlertDescription className='flex flex-col gap-1'>
                {bannerError && <span>{bannerError}</span>}
                {isCoolingDown && (
                  <span aria-live='off' className='text-xs font-medium'>
                    {t('uploadDialog.rateLimitBanner', {
                      seconds: retrySeconds,
                    })}
                  </span>
                )}
              </AlertDescription>
            </Alert>
          )}

          <div
            role='button'
            tabIndex={0}
            onClick={() => !isUploading && fileInputRef.current?.click()}
            onKeyDown={(e) => {
              if ((e.key === 'Enter' || e.key === ' ') && !isUploading) {
                e.preventDefault()
                fileInputRef.current?.click()
              }
            }}
            onDragOver={handleDragOver}
            onDragLeave={handleDragLeave}
            onDrop={handleDrop}
            className={cn(
              'flex cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border-2 border-dashed px-6 py-8 text-center transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none',
              isDragging
                ? 'border-primary bg-primary/5'
                : 'border-muted-foreground/25 hover:border-primary/60 hover:bg-muted/30',
              isUploading && 'pointer-events-none opacity-60'
            )}
          >
            <div className='flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground'>
              <FileUp className='size-5' />
            </div>
            <div className='space-y-1'>
              <p className='text-sm font-medium'>
                {t('uploadDialog.dropzoneTitle')}
              </p>
              <p className='max-w-md text-xs text-muted-foreground'>
                {t('uploadDialog.dropzoneHint')}
              </p>
            </div>
          </div>

          <input
            ref={fileInputRef}
            type='file'
            multiple
            tabIndex={-1}
            accept={acceptedExtensions}
            aria-label={t('uploadDialog.selectFiles')}
            data-testid='files-upload-input'
            className='sr-only'
            onChange={handleFileChange}
          />

          {queue.length > 0 && (
            <div className='space-y-2'>
              <div className='flex items-center justify-between text-xs'>
                <span className='font-medium text-muted-foreground'>
                  {t('uploadDialog.queueTitle', { count: queue.length })}
                </span>
                {hasFinishedItems && !isUploading && (
                  <Button
                    type='button'
                    variant='ghost'
                    size='sm'
                    className='h-6 px-2 text-xs'
                    onClick={handleClearFinished}
                  >
                    {t('uploadDialog.clearFinished')}
                  </Button>
                )}
              </div>

              <ScrollArea className='max-h-56 rounded-md border'>
                <ul className='divide-y'>
                  {queue.map((item) => (
                    <li
                      key={item.id}
                      className='flex items-start justify-between gap-3 px-3 py-2.5 text-sm'
                    >
                      <div className='min-w-0 flex-1 space-y-1'>
                        <div className='flex items-center gap-2'>
                          <span className='truncate font-medium'>
                            {item.file.name}
                          </span>
                          <span className='shrink-0 font-mono text-xs text-muted-foreground'>
                            {formatBytes(item.file.size, i18n.language)}
                          </span>
                        </div>

                        {item.status === 'uploading' && (
                          <div className='space-y-1'>
                            <div className='h-1.5 w-full overflow-hidden rounded-full bg-muted'>
                              <div
                                className='h-full bg-primary transition-all'
                                style={{ width: `${item.progress}%` }}
                              />
                            </div>
                            <p className='text-xs text-muted-foreground'>
                              {t('uploadDialog.status.uploading', {
                                progress: item.progress,
                              })}
                            </p>
                          </div>
                        )}

                        {item.status === 'pending' && (
                          <p className='text-xs text-muted-foreground'>
                            {t('uploadDialog.status.pending')}
                          </p>
                        )}

                        {item.status === 'success' && (
                          <Badge
                            data-testid='files-upload-success'
                            variant='success'
                            className='gap-1 font-normal'
                          >
                            <CheckCircle2 className='size-3' />
                            {t('uploadDialog.status.success')}
                          </Badge>
                        )}

                        {item.status === 'error' && (
                          <p className='flex items-center gap-1 text-xs text-destructive'>
                            <AlertCircle className='size-3.5 shrink-0' />
                            <span>
                              {item.errorMessage ||
                                t('uploadDialog.status.error')}
                            </span>
                          </p>
                        )}
                      </div>

                      {!isUploading && (
                        <Button
                          type='button'
                          variant='ghost'
                          size='icon'
                          className='size-7 shrink-0 text-muted-foreground'
                          aria-label={t('uploadDialog.removeFile', {
                            name: item.file.name,
                          })}
                          onClick={() => handleRemoveItem(item.id)}
                        >
                          <X className='size-3.5' />
                        </Button>
                      )}
                    </li>
                  ))}
                </ul>
              </ScrollArea>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button
            type='button'
            data-testid='files-upload-close'
            variant='outline'
            onClick={() => handleOpenChange(false)}
          >
            {isUploading
              ? tCommon('cancel')
              : hasFinishedItems && pendingItems.length === 0
                ? t('uploadDialog.done')
                : tCommon('cancel')}
          </Button>
          <Button
            type='button'
            data-testid='files-upload-start'
            disabled={isUploading || isCoolingDown || pendingItems.length === 0}
            onClick={() => void handleStartUpload()}
          >
            {isUploading ? (
              <>
                <Loader2 className='size-4 animate-spin' />
                {t('uploadDialog.uploading')}
              </>
            ) : (
              <>
                <Upload className='size-4' />
                {t('uploadDialog.startUpload')}
              </>
            )}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
