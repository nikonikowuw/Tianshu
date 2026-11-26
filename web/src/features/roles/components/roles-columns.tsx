import { type TFunction } from 'i18next'
import { type ColumnDef } from '@tanstack/react-table'
import { DataTableColumnHeader } from '@/components/data-table'
import { ShieldCheck } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { LongText } from '@/components/shared/long-text'
import type { RoleItem } from '../types'
import { RoleRowActions } from './role-row-actions'

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

export function createRoleColumns(params: {
  t: TFunction<'roles'>
  locale: string
  canUpdate: boolean
  canDelete: boolean
  onView: (role: RoleItem) => void
  onEdit: (role: RoleItem) => void
  onDelete: (role: RoleItem) => void
}): ColumnDef<RoleItem>[] {
  const { t, locale, canUpdate, canDelete, onView, onEdit, onDelete } = params

  return [
    {
      id: 'name',
      accessorKey: 'name',
      enableHiding: false,
      meta: { label: t('columns.name') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.name')} />
      ),
      cell: ({ row }) => {
        const role = row.original
        return (
          <div className='flex items-center gap-2'>
            <LongText className='max-w-44 font-medium'>{role.name}</LongText>
            {role.isSystem && (
              <Badge variant='secondary' className='gap-1 font-normal'>
                <ShieldCheck className='size-3' />
                {t('badges.system')}
              </Badge>
            )}
          </div>
        )
      },
    },
    {
      id: 'code',
      accessorKey: 'code',
      meta: { label: t('columns.code') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.code')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs text-muted-foreground'>
          {row.original.code}
        </span>
      ),
    },
    {
      id: 'type',
      accessorFn: (row) => (row.isSystem ? 'system' : 'custom'),
      enableSorting: false,
      enableHiding: false,
      meta: { label: t('filters.type') },
      header: () => null,
      cell: () => null,
    },
    {
      id: 'description',
      accessorKey: 'description',
      enableSorting: false,
      meta: { label: t('columns.description') },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t('columns.description')}
        />
      ),
      cell: ({ row }) => {
        const desc = row.original.description
        if (!desc) {
          return (
            <span className='text-muted-foreground'>
              {t('empty.noDescription')}
            </span>
          )
        }
        return (
          <LongText className='max-w-64 text-muted-foreground'>{desc}</LongText>
        )
      },
    },
    {
      id: 'userCount',
      accessorKey: 'userCount',
      enableSorting: false,
      meta: { label: t('columns.userCount') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.userCount')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-sm tabular-nums'>
          {new Intl.NumberFormat(locale).format(row.original.userCount)}
        </span>
      ),
    },
    {
      id: 'permissionCount',
      accessorKey: 'permissionCount',
      enableSorting: false,
      meta: { label: t('columns.permissionCount') },
      header: ({ column }) => (
        <DataTableColumnHeader
          column={column}
          title={t('columns.permissionCount')}
        />
      ),
      cell: ({ row }) => (
        <Badge variant='outline' className='font-mono tabular-nums'>
          {new Intl.NumberFormat(locale).format(row.original.permissionCount)}
        </Badge>
      ),
    },
    {
      id: 'updated_at',
      accessorKey: 'updatedAt',
      meta: { label: t('columns.updatedAt') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.updatedAt')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
          {formatTimestamp(row.original.updatedAt, locale)}
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
        <RoleRowActions
          role={row.original}
          canUpdate={canUpdate}
          canDelete={canDelete}
          onView={onView}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      ),
    },
  ]
}
