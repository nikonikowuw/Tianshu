import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  Clock,
  Globe,
  LogOut,
  Monitor,
  RefreshCw,
  Smartphone,
  Tablet,
} from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import {
  formatServerErrorMessage,
  handleServerError,
} from '@/lib/handle-server-error'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { LongText } from '@/components/shared/long-text'
import {
  activeSessionsQueryOptions,
  profileKeys,
  useRevokeOtherSessionsMutation,
  useRevokeSessionMutation,
} from '../queries'
import type { ActiveSessionItem } from '../types'
import { ContentSection } from './content-section'
import {
  formatSessionDeviceLabel,
  formatSettingsTimestamp,
  parseSessionUserAgent,
  type ParsedSessionDevice,
} from './session-device'

function DeviceIcon({ type }: { type: ParsedSessionDevice['deviceType'] }) {
  switch (type) {
    case 'mobile':
      return <Smartphone className='size-5' />
    case 'tablet':
      return <Tablet className='size-5' />
    case 'desktop':
      return <Monitor className='size-5' />
    default:
      return <Globe className='size-5' />
  }
}

export function SessionsSection() {
  const { t, i18n } = useTranslation('settings')
  const { t: tCommon } = useTranslation('common')
  const queryClient = useQueryClient()
  const [revokingSession, setRevokingSession] =
    useState<ActiveSessionItem | null>(null)
  const [revokeAllOpen, setRevokeAllOpen] = useState(false)

  const { data, isLoading, isFetching, isError, error, refetch } = useQuery(
    activeSessionsQueryOptions()
  )
  const revokeMutation = useRevokeSessionMutation()
  const revokeAllMutation = useRevokeOtherSessionsMutation()

  const sessions = data?.items ?? []
  const otherSessionsCount = sessions.filter((s) => !s.isCurrent).length

  const handleConfirmRevoke = async () => {
    if (!revokingSession) return
    try {
      await revokeMutation.mutateAsync(revokingSession.id)
      toast.success(t('sessions.revokedSuccess'))
      setRevokingSession(null)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        setRevokingSession(null)
        return
      }
      handleServerError(err)
      if (apiErr.code === 'PROFILE_SESSION_NOT_FOUND') {
        setRevokingSession(null)
        void queryClient.invalidateQueries({ queryKey: profileKeys.sessions })
      }
    }
  }

  const handleConfirmRevokeAll = async () => {
    try {
      const res = await revokeAllMutation.mutateAsync()
      if (res.revokedSessions > 0) {
        toast.success(
          t('sessions.revokedAllSuccess', { count: res.revokedSessions })
        )
      } else {
        toast.info(t('sessions.revokedAllNone'))
      }
      setRevokeAllOpen(false)
    } catch (err) {
      const apiErr = normalizeApiError(err)
      if (apiErr.status === 401 || apiErr.code === 'AUTH_ACCOUNT_DISABLED') {
        setRevokeAllOpen(false)
        return
      }
      handleServerError(err)
    }
  }

  const revokingDeviceLabel = revokingSession
    ? formatSessionDeviceLabel(
        parseSessionUserAgent(revokingSession.userAgent),
        t('sessions.unknownDevice')
      )
    : ''

  return (
    <ContentSection>
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='flex flex-col gap-3 border-b p-6 sm:flex-row sm:items-center sm:justify-between'>
          <div className='space-y-1'>
            <div className='flex flex-wrap items-center gap-2'>
              <CardTitle className='text-base'>{t('sessions.title')}</CardTitle>
              {!isLoading && !isError && (
                <Badge
                  variant='secondary'
                  className='px-2 py-0 text-xs font-normal'
                >
                  {t('sessions.activeCount', { count: sessions.length })}
                </Badge>
              )}
            </div>
            <CardDescription>{t('sessions.description')}</CardDescription>
          </div>

          <div className='flex shrink-0 items-center gap-2'>
            <Button
              type='button'
              variant='outline'
              size='sm'
              data-testid='sessions-refresh-btn'
              disabled={isFetching}
              onClick={() => void refetch()}
            >
              <RefreshCw
                className={isFetching ? 'size-3.5 animate-spin' : 'size-3.5'}
              />
              {t('sessions.refresh')}
            </Button>
            {otherSessionsCount > 0 && (
              <Button
                type='button'
                variant='destructive'
                size='sm'
                data-testid='sessions-revoke-all-btn'
                onClick={() => setRevokeAllOpen(true)}
              >
                <LogOut className='size-3.5' />
                {t('sessions.revokeAllOthers')}
              </Button>
            )}
          </div>
        </CardHeader>

        <CardContent className='p-0'>
          {isLoading ? (
            <div className='divide-y divide-border'>
              {[1, 2].map((idx) => (
                <div key={idx} className='flex items-start gap-4 px-6 py-5'>
                  <Skeleton className='size-10 shrink-0 rounded-lg' />
                  <div className='flex-1 space-y-2'>
                    <Skeleton className='h-4 w-44' />
                    <Skeleton className='h-3.5 w-80' />
                    <Skeleton className='h-3 w-56' />
                  </div>
                </div>
              ))}
            </div>
          ) : isError ? (
            <div className='p-6'>
              <Alert variant='destructive'>
                <AlertCircle className='size-4' />
                <AlertTitle>{t('sessions.loadFailed')}</AlertTitle>
                <AlertDescription className='flex flex-wrap items-center justify-between gap-2'>
                  <span>{formatServerErrorMessage(error)}</span>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() => void refetch()}
                  >
                    {tCommon('retry')}
                  </Button>
                </AlertDescription>
              </Alert>
            </div>
          ) : sessions.length === 0 ? (
            <div className='px-6 py-12 text-center'>
              <p className='text-sm font-medium'>{t('sessions.emptyTitle')}</p>
              <p className='mt-1 text-xs text-muted-foreground'>
                {t('sessions.emptyDesc')}
              </p>
            </div>
          ) : (
            <div className='divide-y divide-border'>
              {sessions.map((session) => {
                const parsed = parseSessionUserAgent(session.userAgent)
                const deviceLabel = formatSessionDeviceLabel(
                  parsed,
                  t('sessions.unknownDevice')
                )
                return (
                  <div
                    key={session.id}
                    data-testid='session-card'
                    className={cn(
                      'flex flex-col gap-4 px-6 py-5 transition-colors sm:flex-row sm:items-center sm:justify-between',
                      session.isCurrent
                        ? 'bg-primary/[0.03]'
                        : 'hover:bg-muted/25'
                    )}
                  >
                    <div className='flex min-w-0 items-start gap-3.5'>
                      <div
                        className={cn(
                          'mt-0.5 flex size-10 shrink-0 items-center justify-center rounded-lg border',
                          session.isCurrent
                            ? 'border-primary/25 bg-primary/10 text-primary'
                            : 'border-border bg-muted/50 text-muted-foreground'
                        )}
                      >
                        <DeviceIcon type={parsed.deviceType} />
                      </div>

                      <div className='min-w-0 space-y-1.5'>
                        <div className='flex flex-wrap items-center gap-2'>
                          <span className='text-sm font-medium'>
                            {deviceLabel}
                          </span>
                          {session.isCurrent && (
                            <Badge
                              variant='secondary'
                              data-testid='session-current-badge'
                              className='gap-1.5 px-2 py-0 text-[11px] font-normal'
                            >
                              <span className='size-1.5 rounded-full bg-success' />
                              {t('sessions.currentBadge')}
                            </Badge>
                          )}
                          <span className='rounded bg-muted px-1.5 py-0.5 font-mono text-[11px] text-foreground'>
                            {session.ipAddress || t('sessions.unknownIp')}
                          </span>
                        </div>

                        <div className='flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-muted-foreground'>
                          <span className='inline-flex items-center gap-1'>
                            <Clock className='size-3 shrink-0' />
                            {t('sessions.lastUsedAt')}:{' '}
                            <span className='font-mono text-foreground/80'>
                              {formatSettingsTimestamp(
                                session.lastUsedAt,
                                i18n.language
                              )}
                            </span>
                          </span>
                          <span>
                            {t('sessions.createdAt')}:{' '}
                            <span className='font-mono'>
                              {formatSettingsTimestamp(
                                session.createdAt,
                                i18n.language
                              )}
                            </span>
                          </span>
                          <span>
                            {t('sessions.expiresAt')}:{' '}
                            <span className='font-mono'>
                              {formatSettingsTimestamp(
                                session.expiresAt,
                                i18n.language
                              )}
                            </span>
                          </span>
                        </div>

                        {session.userAgent && (
                          <LongText className='max-w-xl font-mono text-[11px] text-muted-foreground/70'>
                            {session.userAgent}
                          </LongText>
                        )}
                      </div>
                    </div>

                    <div className='flex shrink-0 items-center sm:justify-end'>
                      {session.isCurrent ? (
                        <span className='text-xs text-muted-foreground'>
                          {t('sessions.currentDeviceNote')}
                        </span>
                      ) : (
                        <Button
                          type='button'
                          variant='outline'
                          size='sm'
                          data-testid='session-revoke-btn'
                          className='text-destructive hover:border-destructive/40 hover:bg-destructive/10 hover:text-destructive'
                          onClick={() => setRevokingSession(session)}
                        >
                          <LogOut className='size-3.5' />
                          {t('sessions.revoke')}
                        </Button>
                      )}
                    </div>
                  </div>
                )
              })}
            </div>
          )}
        </CardContent>
      </Card>

      <ConfirmDialog
        open={revokingSession !== null}
        onOpenChange={(open) => {
          if (!open && !revokeMutation.isPending) {
            setRevokingSession(null)
          }
        }}
        title={t('sessions.revokeTitle')}
        desc={t('sessions.revokeDesc', {
          device: revokingDeviceLabel,
          ip: revokingSession?.ipAddress || t('sessions.unknownIp'),
        })}
        cancelBtnText={tCommon('cancel')}
        confirmText={t('sessions.revokeConfirm')}
        confirmTestId='session-revoke-confirm'
        destructive
        isLoading={revokeMutation.isPending}
        handleConfirm={() => void handleConfirmRevoke()}
      />

      <ConfirmDialog
        open={revokeAllOpen}
        onOpenChange={(open) => {
          if (!open && !revokeAllMutation.isPending) {
            setRevokeAllOpen(false)
          }
        }}
        title={t('sessions.revokeAllTitle')}
        desc={t('sessions.revokeAllDesc', { count: otherSessionsCount })}
        cancelBtnText={tCommon('cancel')}
        confirmText={t('sessions.revokeAllConfirm')}
        confirmTestId='sessions-revoke-all-confirm'
        destructive
        isLoading={revokeAllMutation.isPending}
        handleConfirm={() => void handleConfirmRevokeAll()}
      />
    </ContentSection>
  )
}
