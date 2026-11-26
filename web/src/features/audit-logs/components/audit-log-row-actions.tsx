import { useTranslation } from 'react-i18next'
import { Copy, Eye, MoreHorizontal } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { AuditLogItem } from '../types'

type AuditLogRowActionsProps = {
  entry: AuditLogItem
  onViewDetails: (entry: AuditLogItem) => void
}

export function AuditLogRowActions({
  entry,
  onViewDetails,
}: AuditLogRowActionsProps) {
  const { t } = useTranslation('audit')

  const handleCopyRequestId = async () => {
    if (!entry.requestId) return
    try {
      await navigator.clipboard.writeText(entry.requestId)
      toast.success(t('copied'))
    } catch {
      // Clipboard API may be unavailable in restricted contexts.
    }
  }

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant='ghost'
          size='icon'
          className='size-8 data-[state=open]:bg-muted'
          aria-label={t('rowActions.openMenu')}
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <MoreHorizontal className='size-4' />
          <span className='sr-only'>{t('rowActions.openMenu')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align='end'
        className='w-44'
        onClick={(e) => e.stopPropagation()}
      >
        <DropdownMenuItem onSelect={() => onViewDetails(entry)}>
          <Eye className='size-4' />
          <span>{t('rowActions.viewDetails')}</span>
        </DropdownMenuItem>
        {entry.requestId && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => void handleCopyRequestId()}>
              <Copy className='size-4' />
              <span>{t('rowActions.copyRequestId')}</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
