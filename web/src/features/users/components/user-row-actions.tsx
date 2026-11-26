import { useTranslation } from 'react-i18next'
import {
  Ban,
  CheckCircle2,
  KeyRound,
  MoreHorizontal,
  Pencil,
  Trash2,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  Tooltip,
  TooltipContent,
  TooltipTrigger,
} from '@/components/ui/tooltip'
import type { UserItem } from '../types'

type UserRowActionsProps = {
  user: UserItem
  currentUserId: string | null
  actorIsSuperAdmin: boolean
  canUpdate: boolean
  canDelete: boolean
  onEdit: (user: UserItem) => void
  onResetPassword: (user: UserItem) => void
  onToggleStatus: (user: UserItem) => void
  onDelete: (user: UserItem) => void
}

export function UserRowActions({
  user,
  currentUserId,
  actorIsSuperAdmin,
  canUpdate,
  canDelete,
  onEdit,
  onResetPassword,
  onToggleStatus,
  onDelete,
}: UserRowActionsProps) {
  const { t } = useTranslation('users')

  const isSelf = currentUserId !== null && currentUserId === user.id
  const targetIsSuperAdmin = user.roles.some((r) => r.code === 'super_admin')
  const protectedSuperAdmin = targetIsSuperAdmin && !actorIsSuperAdmin

  if (!canUpdate && !canDelete) {
    return null
  }

  const editDisabled = protectedSuperAdmin
  const toggleDisabled = isSelf || protectedSuperAdmin
  const deleteDisabled = isSelf || protectedSuperAdmin

  const disabledHint = isSelf
    ? t('rowActions.selfDisabledHint')
    : protectedSuperAdmin
      ? t('rowActions.superAdminProtectedHint')
      : null

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          type='button'
          variant='ghost'
          size='icon'
          className='size-7 text-muted-foreground hover:bg-muted hover:text-foreground data-[state=open]:bg-muted data-[state=open]:text-foreground'
          onClick={(e) => e.stopPropagation()}
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
        {canUpdate && (
          <>
            <MenuItemWithHint
              disabled={editDisabled}
              hint={
                protectedSuperAdmin
                  ? t('rowActions.superAdminProtectedHint')
                  : null
              }
              onSelect={() => onEdit(user)}
            >
              <Pencil className='size-4' />
              {t('rowActions.edit')}
            </MenuItemWithHint>
            <MenuItemWithHint
              disabled={editDisabled}
              hint={
                protectedSuperAdmin
                  ? t('rowActions.superAdminProtectedHint')
                  : null
              }
              onSelect={() => onResetPassword(user)}
            >
              <KeyRound className='size-4' />
              {t('rowActions.resetPassword')}
            </MenuItemWithHint>
            <MenuItemWithHint
              disabled={toggleDisabled}
              hint={disabledHint}
              onSelect={() => onToggleStatus(user)}
            >
              {user.status === 'active' ? (
                <>
                  <Ban className='size-4' />
                  {t('rowActions.disable')}
                </>
              ) : (
                <>
                  <CheckCircle2 className='size-4' />
                  {t('rowActions.enable')}
                </>
              )}
            </MenuItemWithHint>
          </>
        )}
        {canUpdate && canDelete && <DropdownMenuSeparator />}
        {canDelete && (
          <MenuItemWithHint
            disabled={deleteDisabled}
            hint={disabledHint}
            destructive
            onSelect={() => onDelete(user)}
          >
            <Trash2 className='size-4' />
            {t('rowActions.delete')}
          </MenuItemWithHint>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  )
}

function MenuItemWithHint({
  disabled,
  hint,
  destructive,
  onSelect,
  children,
}: {
  disabled: boolean
  hint: string | null
  destructive?: boolean
  onSelect: () => void
  children: React.ReactNode
}) {
  const item = (
    <DropdownMenuItem
      disabled={disabled}
      onSelect={onSelect}
      className={
        destructive ? 'text-destructive focus:text-destructive' : undefined
      }
    >
      {children}
    </DropdownMenuItem>
  )

  if (disabled && hint) {
    return (
      <Tooltip>
        <TooltipTrigger asChild>
          <div>{item}</div>
        </TooltipTrigger>
        <TooltipContent side='left'>{hint}</TooltipContent>
      </Tooltip>
    )
  }
  return item
}
