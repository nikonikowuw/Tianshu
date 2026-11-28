import { useTranslation } from 'react-i18next'
import { Copy } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { ScrollArea } from '@/components/ui/scroll-area'
import { Separator } from '@/components/ui/separator'
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { copyAuditText } from '../clipboard'
import type { AuditLogItem } from '../types'
import {
  formatAuditActionLabel,
  formatAuditResourceLabel,
  formatAuditTimestamp,
} from './audit-logs-columns'

type AuditLogDetailSheetProps = {
  entry: AuditLogItem | null
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AuditLogDetailSheet({
  entry,
  open,
  onOpenChange,
}: AuditLogDetailSheetProps) {
  const { t, i18n } = useTranslation('audit')

  const formattedMetadata = entry
    ? JSON.stringify(entry.metadata ?? {}, null, 2)
    : '{}'

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent className='flex w-full flex-col gap-0 overflow-hidden p-0 sm:max-w-lg'>
        <SheetHeader className='border-b px-6 py-4'>
          <SheetTitle>{t('detail.title')}</SheetTitle>
          <SheetDescription>{t('detail.description')}</SheetDescription>
        </SheetHeader>

        {entry && (
          <ScrollArea className='min-h-0 flex-1 px-6 py-4'>
            <div className='space-y-5'>
              <div className='flex flex-wrap items-center justify-between gap-2'>
                <Badge
                  variant={
                    entry.status === 'failure' ? 'destructive' : 'success'
                  }
                >
                  {entry.status === 'failure'
                    ? t('statuses.failure')
                    : t('statuses.success')}
                </Badge>
                <span className='font-mono text-xs text-muted-foreground'>
                  {formatAuditTimestamp(entry.createdAt, i18n.language)}
                </span>
              </div>

              <dl className='grid grid-cols-1 gap-3 text-sm sm:grid-cols-2'>
                <DetailField
                  label={t('detail.eventId')}
                  value={entry.id}
                  mono
                />
                <DetailField
                  label={t('detail.actorUsername')}
                  value={entry.actorUsername || t('detail.anonymous')}
                />
                <DetailField
                  label={t('detail.actorId')}
                  value={entry.actorId || t('detail.emptyValue')}
                  mono
                />
                <DetailField
                  label={t('detail.action')}
                  value={formatAuditActionLabel(entry.action, t)}
                />
                <DetailField
                  label={t('detail.resource')}
                  value={formatAuditResourceLabel(entry.resource, t)}
                />
                <DetailField
                  label={t('detail.resourceId')}
                  value={entry.resourceId || t('detail.emptyValue')}
                  mono
                />
                <DetailField
                  label={t('detail.ipAddress')}
                  value={entry.ipAddress || t('detail.emptyValue')}
                  mono
                />
                <div className='space-y-1'>
                  <dt className='text-xs font-medium text-muted-foreground'>
                    {t('detail.requestId')}
                  </dt>
                  <dd className='flex items-center gap-1.5 font-mono text-xs break-all'>
                    <span>{entry.requestId || t('detail.emptyValue')}</span>
                    {entry.requestId && (
                      <Button
                        type='button'
                        variant='ghost'
                        size='icon'
                        className='size-6 shrink-0'
                        aria-label={t('rowActions.copyRequestId')}
                        onClick={() => void copyAuditText(entry.requestId, t)}
                      >
                        <Copy className='size-3.5' />
                      </Button>
                    )}
                  </dd>
                </div>
              </dl>

              <Separator />

              <div className='space-y-1.5'>
                <p className='text-xs font-medium text-muted-foreground'>
                  {t('detail.userAgent')}
                </p>
                <p className='rounded-md border bg-muted/40 px-3 py-2 font-mono text-xs break-all text-foreground'>
                  {entry.userAgent || t('detail.emptyValue')}
                </p>
              </div>

              <div className='space-y-2'>
                <div className='flex items-center justify-between'>
                  <p className='text-xs font-medium text-muted-foreground'>
                    {t('detail.metadata')}
                  </p>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    className='h-7 px-2 text-xs'
                    onClick={() => void copyAuditText(formattedMetadata, t)}
                  >
                    <Copy className='me-1.5 size-3' />
                    {t('detail.copyJson')}
                  </Button>
                </div>
                <pre className='max-h-72 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs leading-relaxed text-foreground'>
                  {formattedMetadata}
                </pre>
              </div>
            </div>
          </ScrollArea>
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
