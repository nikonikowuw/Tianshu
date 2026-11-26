import { type TFunction } from 'i18next'
import { type ColumnDef } from '@tanstack/react-table'
import { DataTableColumnHeader } from '@/components/data-table'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import { Badge } from '@/components/ui/badge'
import { LongText } from '@/components/shared/long-text'
import type { UserItem } from '../types'
import { UserRowActions } from './user-row-actions'

function formatTimestamp(iso: string, locale: string): string {
  const parsed = new Date(iso)
  if (Number.isNaN(parsed.getTime())) {
    return iso
  }
  return new Intl.DateTimeFormat(locale, {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  }).format(parsed)
}

function initialsOf(name: string): string {
  const trimmed = name.trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

export function createUserColumns(params: {
  t: TFunction<'users'>
  locale: string
  currentUserId: string | null
  actorIsSuperAdmin: boolean
  canUpdate: boolean
  canDelete: boolean
  onEdit: (user: UserItem) => void
  onResetPassword: (user: UserItem) => void
  onToggleStatus: (user: UserItem) => void
  onDelete: (user: UserItem) => void
}): ColumnDef<UserItem>[] {
  const {
    t,
    locale,
    currentUserId,
    actorIsSuperAdmin,
    canUpdate,
    canDelete,
    onEdit,
    onResetPassword,
    onToggleStatus,
    onDelete,
  } = params

  return [
    {
      id: 'username',
      accessorKey: 'username',
      enableHiding: false,
      meta: { label: t('columns.user') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.user')} />
      ),
      cell: ({ row }) => {
        const user = row.original
        const label = user.displayName || user.username
        return (
          <div className='flex items-center gap-2.5'>
            <Avatar className='size-8 border'>
              <AvatarFallback className='text-xs font-medium'>
                {initialsOf(label)}
              </AvatarFallback>
            </Avatar>
            <div className='min-w-0'>
              <LongText className='max-w-40 font-medium'>{label}</LongText>
              <div className='truncate font-mono text-xs text-muted-foreground'>
                @{user.username}
              </div>
            </div>
          </div>
        )
      },
    },
    {
      id: 'email',
      accessorKey: 'email',
      enableSorting: false,
      meta: { label: t('columns.email') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.email')} />
      ),
      cell: ({ row }) => (
        <LongText className='max-w-52 text-muted-foreground'>
          {row.original.email}
        </LongText>
      ),
    },
    {
      id: 'roles',
      enableSorting: false,
      meta: { label: t('columns.roles') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.roles')} />
      ),
      cell: ({ row }) => {
        const roles = row.original.roles
        if (roles.length === 0) {
          return (
            <span className='text-xs text-muted-foreground'>
              {t('empty.noRoles')}
            </span>
          )
        }
        const visible = roles.slice(0, 2)
        const extra = roles.length - visible.length
        return (
          <div className='flex flex-wrap items-center gap-1'>
            {visible.map((r) => (
              <Badge
                key={r.id}
                variant={r.isSystem ? 'secondary' : 'outline'}
                className='font-normal'
              >
                {r.name}
              </Badge>
            ))}
            {extra > 0 && (
              <Badge variant='outline' className='font-mono text-xs'>
                +{extra}
              </Badge>
            )}
          </div>
        )
      },
    },
    {
      id: 'status',
      accessorKey: 'status',
      enableSorting: false,
      meta: { label: t('columns.status') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.status')} />
      ),
      cell: ({ row }) => {
        const active = row.original.status === 'active'
        return (
          <Badge variant={active ? 'success' : 'secondary'}>
            {active ? t('status.active') : t('status.disabled')}
          </Badge>
        )
      },
    },
    {
      id: 'roleId',
      accessorFn: (row) => row.roles.map((r) => r.id),
      enableSorting: false,
      enableHiding: false,
      meta: { label: t('filters.role') },
      header: () => null,
      cell: () => null,
    },
    {
      id: 'last_login_at',
      accessorKey: 'lastLoginAt',
      meta: { label: t('columns.lastLoginAt') },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t('columns.lastLoginAt')}
        />
      ),
      cell: ({ row }) => {
        const val = row.original.lastLoginAt
        if (!val) {
          return (
            <span className='text-xs text-muted-foreground'>
              {t('empty.neverLoggedIn')}
            </span>
          )
        }
        return (
          <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
            {formatTimestamp(val, locale)}
          </span>
        )
      },
    },
    {
      id: 'createdBy',
      accessorKey: 'createdBy',
      enableSorting: false,
      meta: { label: t('columns.createdBy') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.createdBy')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs text-muted-foreground'>
          {row.original.createdBy?.username || t('empty.systemActor')}
        </span>
      ),
    },
    {
      id: 'created_at',
      accessorKey: 'createdAt',
      meta: { label: t('columns.createdAt') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.createdAt')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
          {formatTimestamp(row.original.createdAt, locale)}
        </span>
      ),
    },
    {
      id: 'actions',
      enableSorting: false,
      enableHiding: false,
      meta: {
        thClassName: 'sticky end-0 z-10 w-12 bg-background px-2.5 last:pe-2.5',
        tdClassName:
          'sticky end-0 z-10 w-12 bg-background px-2.5 last:pe-2.5 text-center transition-colors group-hover/row:bg-[color-mix(in_srgb,var(--muted)_50%,var(--background))] group-focus-visible/row:bg-[color-mix(in_srgb,var(--muted)_60%,var(--background))] group-data-[state=selected]/row:bg-muted',
      },
      header: () => <span className='sr-only'>{t('rowActions.openMenu')}</span>,
      cell: ({ row }) => (
        <UserRowActions
          user={row.original}
          currentUserId={currentUserId}
          actorIsSuperAdmin={actorIsSuperAdmin}
          canUpdate={canUpdate}
          canDelete={canDelete}
          onEdit={onEdit}
          onResetPassword={onResetPassword}
          onToggleStatus={onToggleStatus}
          onDelete={onDelete}
        />
      ),
    },
  ]
}
