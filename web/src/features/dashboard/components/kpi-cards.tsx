import { useTranslation } from 'react-i18next'
import { Link } from '@tanstack/react-router'
import { Activity, ArrowUpRight, FolderOpen, Shield, Users } from 'lucide-react'
import { usePermission } from '@/lib/auth/use-permission'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent, CardFooter, CardHeader } from '@/components/ui/card'
import type { DashboardSummary } from '../types'
import { formatCount, formatStorageBytes } from './format'

type KpiCardsProps = {
  summary: DashboardSummary
}

function pct(part: number, total: number): number {
  if (total <= 0 || part <= 0) return 0
  return Math.min(100, Math.round((part / total) * 100))
}

function RatioBar({
  primaryPct,
  secondaryPct,
  primaryClassName,
  secondaryClassName,
  label,
}: {
  primaryPct: number
  secondaryPct?: number
  primaryClassName: string
  secondaryClassName?: string
  label: string
}) {
  return (
    <div
      role='img'
      aria-label={label}
      className='flex h-1.5 w-full overflow-hidden rounded-full bg-muted'
    >
      {primaryPct > 0 && (
        <div className={primaryClassName} style={{ width: `${primaryPct}%` }} />
      )}
      {secondaryPct && secondaryPct > 0 && secondaryClassName && (
        <div
          className={secondaryClassName}
          style={{ width: `${secondaryPct}%` }}
        />
      )}
    </div>
  )
}

export function KpiCards({ summary }: KpiCardsProps) {
  const { t, i18n } = useTranslation('dashboard')
  const locale = i18n.language

  const canReadUsers = usePermission('users:read')
  const canReadRoles = usePermission('roles:read')
  const canReadFiles = usePermission('files:read')

  const activeUsersPct = pct(summary.users.active, summary.users.total)
  const systemRolesPct = pct(summary.roles.system, summary.roles.total)
  const customRolesPct = pct(summary.roles.custom, summary.roles.total)
  const imageFilesPct = pct(summary.files.images, summary.files.total)
  const docFilesPct = pct(summary.files.documents, summary.files.total)
  const successEvents = Math.max(
    0,
    summary.audit.totalInRange - summary.audit.failuresInRange
  )
  const successEventsPct = pct(successEvents, summary.audit.totalInRange)
  const failureEventsPct = pct(
    summary.audit.failuresInRange,
    summary.audit.totalInRange
  )

  return (
    <div className='grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4'>
      {/* Users */}
      <Card
        data-testid='dashboard-kpi-users'
        className='gap-0 overflow-hidden bg-gradient-to-t from-muted/25 to-card py-0 shadow-xs transition-colors hover:border-foreground/20'
      >
        <CardHeader className='flex flex-row items-center justify-between space-y-0 px-5 pt-5 pb-2'>
          <span className='flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase'>
            <Users className='size-3.5 text-chart-1' />
            {t('kpi.users.title')}
          </span>
          {summary.users.newLast7Days > 0 ? (
            <Badge
              variant='secondary'
              className='h-5 rounded-full px-2 font-mono text-[11px] font-medium'
            >
              {t('kpi.users.newLast7Days', {
                count: formatCount(summary.users.newLast7Days, locale),
              })}
            </Badge>
          ) : (
            <Badge
              variant='outline'
              className='h-5 rounded-full px-2 font-mono text-[11px] font-normal text-muted-foreground tabular-nums'
            >
              {activeUsersPct}%
            </Badge>
          )}
        </CardHeader>
        <CardContent className='space-y-3 px-5 pb-4'>
          <div className='flex items-baseline gap-2'>
            <span
              data-testid='dashboard-kpi-users-value'
              className='text-3xl font-semibold tracking-tight tabular-nums'
            >
              {formatCount(summary.users.total, locale)}
            </span>
            <span className='text-xs text-muted-foreground'>
              {t('kpi.users.activeRate', { percent: activeUsersPct })}
            </span>
          </div>
          <RatioBar
            primaryPct={activeUsersPct}
            primaryClassName='bg-success transition-all'
            label={t('kpi.users.breakdown', {
              active: formatCount(summary.users.active, locale),
              disabled: formatCount(summary.users.disabled, locale),
            })}
          />
        </CardContent>
        <CardFooter className='flex items-center justify-between gap-2 border-t bg-muted/20 px-5 py-2.5 text-xs text-muted-foreground [.border-t]:pt-2.5'>
          <span className='truncate'>
            {t('kpi.users.breakdown', {
              active: formatCount(summary.users.active, locale),
              disabled: formatCount(summary.users.disabled, locale),
            })}
          </span>
          {canReadUsers && (
            <Link
              to='/users'
              className='inline-flex shrink-0 items-center gap-0.5 font-medium text-foreground transition-colors hover:text-primary'
            >
              {t('kpi.manage')}
              <ArrowUpRight className='size-3.5' />
            </Link>
          )}
        </CardFooter>
      </Card>

      {/* Roles */}
      <Card
        data-testid='dashboard-kpi-roles'
        className='gap-0 overflow-hidden bg-gradient-to-t from-muted/25 to-card py-0 shadow-xs transition-colors hover:border-foreground/20'
      >
        <CardHeader className='flex flex-row items-center justify-between space-y-0 px-5 pt-5 pb-2'>
          <span className='flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase'>
            <Shield className='size-3.5 text-chart-2' />
            {t('kpi.roles.title')}
          </span>
          <Badge
            variant='outline'
            className='h-5 rounded-full px-2 font-mono text-[11px] font-normal text-muted-foreground tabular-nums'
          >
            {t('kpi.roles.customRatio', {
              count: formatCount(summary.roles.custom, locale),
            })}
          </Badge>
        </CardHeader>
        <CardContent className='space-y-3 px-5 pb-4'>
          <div className='flex items-baseline gap-2'>
            <span
              data-testid='dashboard-kpi-roles-value'
              className='text-3xl font-semibold tracking-tight tabular-nums'
            >
              {formatCount(summary.roles.total, locale)}
            </span>
            <span className='text-xs text-muted-foreground'>
              {t('kpi.roles.unit')}
            </span>
          </div>
          <RatioBar
            primaryPct={systemRolesPct}
            secondaryPct={customRolesPct}
            primaryClassName='bg-chart-1 transition-all'
            secondaryClassName='bg-chart-2 transition-all'
            label={t('kpi.roles.breakdown', {
              system: formatCount(summary.roles.system, locale),
              custom: formatCount(summary.roles.custom, locale),
            })}
          />
        </CardContent>
        <CardFooter className='flex items-center justify-between gap-2 border-t bg-muted/20 px-5 py-2.5 text-xs text-muted-foreground [.border-t]:pt-2.5'>
          <span className='truncate'>
            {t('kpi.roles.breakdown', {
              system: formatCount(summary.roles.system, locale),
              custom: formatCount(summary.roles.custom, locale),
            })}
          </span>
          {canReadRoles && (
            <Link
              to='/roles'
              className='inline-flex shrink-0 items-center gap-0.5 font-medium text-foreground transition-colors hover:text-primary'
            >
              {t('kpi.manage')}
              <ArrowUpRight className='size-3.5' />
            </Link>
          )}
        </CardFooter>
      </Card>

      {/* Files & Storage */}
      <Card
        data-testid='dashboard-kpi-files'
        className='gap-0 overflow-hidden bg-gradient-to-t from-muted/25 to-card py-0 shadow-xs transition-colors hover:border-foreground/20'
      >
        <CardHeader className='flex flex-row items-center justify-between space-y-0 px-5 pt-5 pb-2'>
          <span className='flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase'>
            <FolderOpen className='size-3.5 text-chart-3' />
            {t('kpi.files.title')}
          </span>
          <Badge
            variant='outline'
            data-testid='dashboard-kpi-files-size'
            className='h-5 rounded-full px-2 font-mono text-[11px] font-normal'
          >
            {formatStorageBytes(summary.files.totalSizeBytes, locale)}
          </Badge>
        </CardHeader>
        <CardContent className='space-y-3 px-5 pb-4'>
          <div className='flex items-baseline gap-2'>
            <span
              data-testid='dashboard-kpi-files-value'
              className='text-3xl font-semibold tracking-tight tabular-nums'
            >
              {formatCount(summary.files.total, locale)}
            </span>
            <span className='text-xs text-muted-foreground'>
              {t('kpi.files.unit')}
            </span>
          </div>
          <RatioBar
            primaryPct={imageFilesPct}
            secondaryPct={docFilesPct}
            primaryClassName='bg-chart-2 transition-all'
            secondaryClassName='bg-chart-4 transition-all'
            label={t('kpi.files.breakdown', {
              images: formatCount(summary.files.images, locale),
              documents: formatCount(summary.files.documents, locale),
            })}
          />
        </CardContent>
        <CardFooter className='flex items-center justify-between gap-2 border-t bg-muted/20 px-5 py-2.5 text-xs text-muted-foreground [.border-t]:pt-2.5'>
          <span className='truncate'>
            {t('kpi.files.breakdown', {
              images: formatCount(summary.files.images, locale),
              documents: formatCount(summary.files.documents, locale),
            })}
          </span>
          {canReadFiles && (
            <Link
              to='/files'
              className='inline-flex shrink-0 items-center gap-0.5 font-medium text-foreground transition-colors hover:text-primary'
            >
              {t('kpi.manage')}
              <ArrowUpRight className='size-3.5' />
            </Link>
          )}
        </CardFooter>
      </Card>

      {/* Active Sessions & Security */}
      <Card
        data-testid='dashboard-kpi-sessions'
        className='gap-0 overflow-hidden bg-gradient-to-t from-muted/25 to-card py-0 shadow-xs transition-colors hover:border-foreground/20'
      >
        <CardHeader className='flex flex-row items-center justify-between space-y-0 px-5 pt-5 pb-2'>
          <span className='flex items-center gap-2 text-xs font-medium tracking-wide text-muted-foreground uppercase'>
            <Activity className='size-3.5 text-success' />
            {t('kpi.sessions.title')}
          </span>
          {summary.audit.failuresInRange > 0 ? (
            <Badge
              variant='destructive'
              className='h-5 rounded-full px-2 font-mono text-[11px]'
            >
              {t('kpi.sessions.failuresBadge', {
                count: formatCount(summary.audit.failuresInRange, locale),
              })}
            </Badge>
          ) : (
            <Badge
              variant='success'
              className='h-5 rounded-full px-2 text-[11px] font-normal'
            >
              {t('kpi.sessions.healthyBadge')}
            </Badge>
          )}
        </CardHeader>
        <CardContent className='space-y-3 px-5 pb-4'>
          <div className='flex items-baseline gap-2'>
            <span
              data-testid='dashboard-kpi-sessions-value'
              className='text-3xl font-semibold tracking-tight tabular-nums'
            >
              {formatCount(summary.sessions.active, locale)}
            </span>
            <span className='text-xs text-muted-foreground'>
              {t('kpi.sessions.unit')}
            </span>
          </div>
          <RatioBar
            primaryPct={successEventsPct}
            secondaryPct={failureEventsPct}
            primaryClassName='bg-success transition-all'
            secondaryClassName='bg-destructive transition-all'
            label={t('kpi.sessions.breakdown', {
              logins: formatCount(summary.audit.loginsInRange, locale),
              days: summary.rangeDays,
            })}
          />
        </CardContent>
        <CardFooter className='flex items-center justify-between gap-2 border-t bg-muted/20 px-5 py-2.5 text-xs text-muted-foreground [.border-t]:pt-2.5'>
          <span className='truncate'>
            {t('kpi.sessions.breakdown', {
              logins: formatCount(summary.audit.loginsInRange, locale),
              days: summary.rangeDays,
            })}
          </span>
          <Link
            to='/settings/sessions'
            className='inline-flex shrink-0 items-center gap-0.5 font-medium text-foreground transition-colors hover:text-primary'
          >
            {t('kpi.viewSessions')}
            <ArrowUpRight className='size-3.5' />
          </Link>
        </CardFooter>
      </Card>
    </div>
  )
}
