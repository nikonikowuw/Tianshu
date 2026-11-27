import { type TFunction } from 'i18next'
import { type ColumnDef } from '@tanstack/react-table'
import { DataTableColumnHeader } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { LongText } from '@/components/shared/long-text'
import type { AuditLogItem } from '../types'
import { AuditLogRowActions } from './audit-log-row-actions'

const ACTION_LABEL_KEY: Record<string, string> = {
  'setup.init': 'actions.setupInit',
  'auth.login': 'actions.authLogin',
  'auth.logout': 'actions.authLogout',
  'auth.refresh': 'actions.authRefresh',
  'auth.refresh_replay': 'actions.authRefreshReplay',
  'role.create': 'actions.roleCreate',
  'role.update': 'actions.roleUpdate',
  'role.delete': 'actions.roleDelete',
  'file.upload': 'actions.fileUpload',
  'file.upload_avatar': 'actions.fileUploadAvatar',
  'file.delete': 'actions.fileDelete',
  'profile.update': 'actions.profileUpdate',
  'profile.change_password': 'actions.profileChangePassword',
  'profile.session_revoke': 'actions.profileSessionRevoke',
  'profile.sessions_revoke_others': 'actions.profileSessionsRevokeOthers',
}

const RESOURCE_LABEL_KEY: Record<string, string> = {
  system: 'resources.system',
  session: 'resources.session',
  user: 'resources.user',
  role: 'resources.role',
  file: 'resources.file',
}

export function formatAuditTimestamp(iso: string, locale: string): string {
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
    second: '2-digit',
    hour12: false,
  }).format(parsed)
}

export function formatAuditActionLabel(
  action: string,
  t: TFunction<'audit'>
): string {
  const key = ACTION_LABEL_KEY[action]
  return key ? t(key) : action
}

export function formatAuditResourceLabel(
  resource: string,
  t: TFunction<'audit'>
): string {
  const key = RESOURCE_LABEL_KEY[resource]
  return key ? t(key) : resource
}

export function createAuditLogColumns(params: {
  t: TFunction<'audit'>
  locale: string
  onViewDetails: (entry: AuditLogItem) => void
}): ColumnDef<AuditLogItem>[] {
  const { t, locale, onViewDetails } = params

  return [
    {
      id: 'created_at',
      accessorKey: 'createdAt',
      meta: { label: t('columns.createdAt') },
      enableHiding: false,
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.createdAt')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
          {formatAuditTimestamp(row.original.createdAt, locale)}
        </span>
      ),
    },
    {
      id: 'actor',
      accessorKey: 'actorUsername',
      enableSorting: false,
      meta: { label: t('columns.actor') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.actor')} />
      ),
      cell: ({ row }) => {
        const username = row.original.actorUsername
        if (!username) {
          return (
            <span className='text-muted-foreground italic'>
              {t('detail.anonymous')}
            </span>
          )
        }
        return <LongText className='max-w-36 font-medium'>{username}</LongText>
      },
    },
    {
      id: 'action',
      accessorKey: 'action',
      meta: { label: t('columns.action') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.action')} />
      ),
      cell: ({ row }) => (
        <LongText className='max-w-52 font-mono text-xs'>
          {formatAuditActionLabel(row.original.action, t)}
        </LongText>
      ),
    },
    {
      id: 'resource',
      accessorKey: 'resource',
      meta: { label: t('columns.resource') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.resource')} />
      ),
      cell: ({ row }) => {
        const { resource, resourceId } = row.original
        return (
          <div className='flex max-w-48 flex-col'>
            <span className='truncate text-sm'>
              {formatAuditResourceLabel(resource, t)}
            </span>
            {resourceId && (
              <span className='truncate font-mono text-xs text-muted-foreground'>
                {resourceId}
              </span>
            )}
          </div>
        )
      },
    },
    {
      id: 'status',
      accessorKey: 'status',
      meta: { label: t('columns.status') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.status')} />
      ),
      cell: ({ row }) => {
        const status = row.original.status
        const isFailure = status === 'failure'
        return (
          <Badge variant={isFailure ? 'destructive' : 'success'}>
            {isFailure ? t('statuses.failure') : t('statuses.success')}
          </Badge>
        )
      },
    },
    {
      id: 'ipAddress',
      accessorKey: 'ipAddress',
      enableSorting: false,
      meta: { label: t('columns.ipAddress') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.ipAddress')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs text-muted-foreground'>
          {row.original.ipAddress || t('detail.emptyValue')}
        </span>
      ),
    },
    {
      id: 'requestId',
      accessorKey: 'requestId',
      enableSorting: false,
      meta: { label: t('columns.requestId') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.requestId')} />
      ),
      cell: ({ row }) => (
        <LongText className='max-w-36 font-mono text-xs text-muted-foreground'>
          {row.original.requestId || t('detail.emptyValue')}
        </LongText>
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
        <AuditLogRowActions
          entry={row.original}
          onViewDetails={onViewDetails}
        />
      ),
    },
  ]
}
