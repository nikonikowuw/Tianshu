import { useTranslation } from 'react-i18next'
import { Eye, MoreHorizontal, Pencil, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import type { RoleItem } from '../types'

type RoleRowActionsProps = {
  role: RoleItem
  canUpdate: boolean
  canDelete: boolean
  onView: (role: RoleItem) => void
  onEdit: (role: RoleItem) => void
  onDelete: (role: RoleItem) => void
}

export function RoleRowActions({
  role,
  canUpdate,
  canDelete,
  onView,
  onEdit,
  onDelete,
}: RoleRowActionsProps) {
  const { t } = useTranslation('roles')
  const showEdit = canUpdate && !role.isSystem
  const showDelete = canDelete && !role.isSystem
  const deleteDisabled = role.userCount > 0

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant='ghost'
          size='icon'
          className='size-7 rounded-md align-middle text-muted-foreground hover:bg-foreground/10 hover:text-foreground focus-visible:bg-foreground/10 focus-visible:text-foreground data-[state=open]:bg-foreground/10 data-[state=open]:text-foreground dark:hover:bg-foreground/15 dark:focus-visible:bg-foreground/15 dark:data-[state=open]:bg-foreground/15'
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
        <DropdownMenuItem onSelect={() => onView(role)}>
          <Eye className='size-4' />
          <span>{t('rowActions.view')}</span>
        </DropdownMenuItem>
        {showEdit && (
          <DropdownMenuItem onSelect={() => onEdit(role)}>
            <Pencil className='size-4' />
            <span>{t('rowActions.edit')}</span>
          </DropdownMenuItem>
        )}
        {showDelete && (
          <>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              disabled={deleteDisabled}
              className='text-destructive focus:text-destructive'
              onSelect={() => onDelete(role)}
            >
              <Trash2 className='size-4' />
              <span>{t('rowActions.delete')}</span>
            </DropdownMenuItem>
          </>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
