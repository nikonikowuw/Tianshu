import { useTranslation } from 'react-i18next'
import { Download, Eye, MoreHorizontal, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { FileItem } from '../types'

type FileRowActionsProps = {
  file: FileItem
  canDelete: boolean
  onInspect: (file: FileItem) => void
  onDownload: (file: FileItem) => void
  onDelete: (file: FileItem) => void
}

export function FileRowActions({
  file,
  canDelete,
  onInspect,
  onDownload,
  onDelete,
}: FileRowActionsProps) {
  const { t } = useTranslation('files')

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          data-testid='file-row-actions'
          className='size-7 text-muted-foreground hover:bg-muted hover:text-foreground data-[state=open]:bg-muted data-[state=open]:text-foreground'
          onClick={(e) => e.stopPropagation()}
          onKeyDown={(e) => e.stopPropagation()}
        >
          <MoreHorizontal className='size-4' />
          <span className='sr-only'>{t('rowActions.openMenu')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent
        align='end'
        className='w-40'
        onClick={(e) => e.stopPropagation()}
        onKeyDown={(e) => e.stopPropagation()}
      >
        <DropdownMenuItem onSelect={() => onInspect(file)}>
          <Eye className='size-4' />
          {t('rowActions.preview')}
        </DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onDownload(file)}>
          <Download className='size-4' />
          {t('rowActions.download')}
        </DropdownMenuItem>
        {canDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              data-testid='file-delete-action'
              onSelect={() => onDelete(file)}
              className='text-destructive focus:text-destructive'
            >
              <Trash2 className='size-4' />
              {t('rowActions.delete')}
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
