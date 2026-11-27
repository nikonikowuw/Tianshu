import { useTranslation } from 'react-i18next'
import { Link } from '@tanstack/react-router'
import { ArrowUpRight, Clock } from 'lucide-react'
import { usePermission } from '@/lib/auth/use-permission'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { ScrollArea } from '@/components/ui/scroll-area'
import { PermissionGuard } from '@/components/shared/permission-guard'
import type { RecentActivityItem } from '../types'
import { formatActivityTimestamp } from './format'

type RecentActivityCardProps = {
  items: RecentActivityItem[]
}

const ACTION_KEY_MAP: Record<string, string> = {
  'setup.init': 'actions.setupInit',
  'auth.login': 'actions.authLogin',
  'auth.logout': 'actions.authLogout',
  'auth.refresh': 'actions.authRefresh',
  'auth.refresh_replay': 'actions.authRefreshReplay',
  'user.create': 'actions.userCreate',
  'user.update': 'actions.userUpdate',
  'user.delete': 'actions.userDelete',
  'role.create': 'actions.roleCreate',
  'role.update': 'actions.roleUpdate',
  'role.delete': 'actions.roleDelete',
  'menu.create': 'actions.menuCreate',
  'menu.update': 'actions.menuUpdate',
  'menu.reorder': 'actions.menuReorder',
  'menu.delete': 'actions.menuDelete',
  'file.upload': 'actions.fileUpload',
  'file.upload_avatar': 'actions.fileUploadAvatar',
  'file.delete': 'actions.fileDelete',
  'profile.update': 'actions.profileUpdate',
  'profile.change_password': 'actions.profileChangePassword',
  'profile.session_revoke': 'actions.profileSessionRevoke',
  'profile.sessions_revoke_others': 'actions.profileSessionsRevokeOthers',
}

export function RecentActivityCard({ items }: RecentActivityCardProps) {
  const { t, i18n } = useTranslation('dashboard')
  const locale = i18n.language
  const canReadAudit = usePermission('audit:read')

  return (
    <Card
      data-testid='dashboard-recent-activity'
      className='flex flex-col gap-0 overflow-hidden py-0 shadow-xs xl:col-span-4'
    >
      <CardHeader className='flex min-h-[84px] flex-row items-center justify-between gap-3 border-b px-6 py-5 [.border-b]:pb-5'>
        <div className='min-w-0 space-y-1'>
          <CardTitle className='text-base'>{t('recent.title')}</CardTitle>
          <CardDescription className='truncate'>
            {t('recent.description')}
          </CardDescription>
        </div>
        <PermissionGuard allowed={canReadAudit}>
          <Link
            to='/audit-logs'
            data-testid='dashboard-view-all-audit'
            className='inline-flex shrink-0 items-center gap-0.5 rounded-md border bg-background px-2.5 py-1 text-xs font-medium text-foreground shadow-2xs transition-colors hover:bg-accent'
          >
            {t('recent.viewAll')}
            <ArrowUpRight className='size-3.5' />
          </Link>
        </PermissionGuard>
      </CardHeader>

      <CardContent className='flex-1 p-0'>
        {items.length === 0 ? (
          <div className='p-6'>
            <div
              data-testid='dashboard-recent-empty'
              className='flex h-[296px] flex-col items-center justify-center rounded-lg border border-dashed bg-muted/15 text-center'
            >
              <p className='text-sm font-medium'>{t('recent.emptyTitle')}</p>
              <p className='mt-1 max-w-xs text-xs text-muted-foreground'>
                {t('recent.emptyDesc')}
              </p>
            </div>
          </div>
        ) : (
          <div className='relative'>
            <ScrollArea className='h-[344px]'>
              <ol className='relative px-6 pt-5 pb-8'>
                {items.map((item, idx) => {
                  const isFailure = item.status === 'failure'
                  const isLast = idx === items.length - 1
                  const actionKey = ACTION_KEY_MAP[item.action]
                  const actionLabel = actionKey ? t(actionKey) : item.action
                  const actorLabel = item.actorUsername || t('recent.anonymous')

                  return (
                    <li
                      key={item.id}
                      data-testid='dashboard-recent-item'
                      className={cn(
                        'relative flex items-start justify-between gap-3 ps-6',
                        !isLast && 'pb-5'
                      )}
                    >
                      {!isLast && (
                        <span
                          aria-hidden='true'
                          className='absolute inset-s-[5px] top-3 bottom-0 w-px bg-border'
                        />
                      )}
                      <span
                        aria-hidden='true'
                        className={cn(
                          'absolute inset-s-0 top-1.5 size-[11px] rounded-full border-2 border-card',
                          isFailure ? 'bg-destructive' : 'bg-success'
                        )}
                      />

                      <div className='min-w-0 flex-1 space-y-1'>
                        <p className='text-sm leading-snug'>
                          <span className='font-medium text-foreground'>
                            {actorLabel}
                          </span>{' '}
                          <span className='text-muted-foreground'>
                            {actionLabel}
                          </span>
                        </p>
                        <div className='flex flex-wrap items-center gap-x-2.5 gap-y-1 text-[11px] text-muted-foreground'>
                          <span className='inline-flex items-center gap-1 font-mono'>
                            <Clock className='size-3 shrink-0' />
                            {formatActivityTimestamp(item.createdAt, locale)}
                          </span>
                          {item.ipAddress && (
                            <span className='rounded bg-muted px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground'>
                              {item.ipAddress}
                            </span>
                          )}
                        </div>
                      </div>

                      <Badge
                        variant={isFailure ? 'destructive' : 'success'}
                        className='mt-0.5 shrink-0 px-2 py-0 text-[10px]'
                      >
                        {isFailure
                          ? t('recent.statusFailure')
                          : t('recent.statusSuccess')}
                      </Badge>
                    </li>
                  )
                })}
              </ol>
            </ScrollArea>
            <div
              aria-hidden='true'
              className='pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t from-card to-transparent'
            />
          </div>
        )}
      </CardContent>
    </Card>
  )
}
