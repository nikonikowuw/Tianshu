import { useTranslation } from 'react-i18next'
import {
  AlertCircle,
  Clock,
  TriangleAlert,
  type LucideIcon,
} from 'lucide-react'
import { cn } from '@/lib/utils'

/** Semantic tone of the banner; maps to a fixed icon + colour pairing. */
export type AuthAlertTone = 'destructive' | 'warning' | 'rate-limited'

const TONE_MAP: Record<
  AuthAlertTone,
  { icon: LucideIcon; container: string; iconColor: string }
> = {
  destructive: {
    icon: AlertCircle,
    container:
      'border-destructive/30 bg-destructive/[0.06] text-destructive dark:border-destructive/40 dark:bg-destructive/12',
    iconColor: 'text-destructive',
  },
  warning: {
    icon: TriangleAlert,
    container:
      'border-warning/30 bg-warning/10 text-warning-foreground dark:border-warning/25 dark:bg-warning/10',
    iconColor: 'text-warning',
  },
  'rate-limited': {
    icon: Clock,
    container:
      'border-destructive/30 bg-destructive/[0.06] text-destructive dark:border-destructive/40 dark:bg-destructive/12',
    iconColor: 'text-destructive',
  },
}

type AuthAlertProps = {
  tone: AuthAlertTone
  /** Short, single-line headline. */
  title: string
  /** Optional supporting copy (translated by the caller). */
  description?: string
  /**
   * Live-region announcement mode. Defaults to `'assertive'` for dynamic
   * submission failures; pass `'off'` for static informational notices so they
   * do not interrupt screen readers on mount or duplicate `role="alert"`.
   */
  live?: 'assertive' | 'polite' | 'off'
  /**
   * Set to `'off'` when `description` changes frequently (e.g. a countdown) so
   * screen readers announce the banner once on insertion instead of re-reading
   * the whole alert on every tick.
   */
  descriptionLive?: 'off'
  className?: string
}

/**
 * Persistent status banner for auth screens.
 *
 * Unlike a toast, this stays mounted while the condition holds, so a blocked
 * submission always has a visible reason. Dynamic errors default to
 * `role="alert"` (`aria-live="assertive"`), while static notices can opt out
 * via `live="off"`.
 */
export function AuthAlert({
  tone,
  title,
  description,
  live = 'assertive',
  descriptionLive,
  className,
}: AuthAlertProps) {
  const { icon: Icon, container, iconColor } = TONE_MAP[tone]
  const role =
    live === 'assertive' ? 'alert' : live === 'polite' ? 'status' : 'note'

  return (
    <div
      role={role}
      aria-live={live === 'off' ? undefined : live}
      className={cn(
        'flex items-start gap-2.5 rounded-lg border px-3.5 py-2.5 text-[13px]',
        container,
        className
      )}
    >
      <Icon className={cn('mt-0.5 size-4 shrink-0', iconColor)} aria-hidden />
      <div className='grid gap-0.5'>
        <p className='leading-snug font-medium'>{title}</p>
        {description && (
          <p
            aria-live={descriptionLive}
            className='text-xs leading-relaxed text-current/80'
          >
            {description}
          </p>
        )}
      </div>
    </div>
  )
}

/** Convenience wrapper for rate-limit banners with a live countdown. */
export function RateLimitAlert({ seconds }: { seconds: number }) {
  const { t } = useTranslation('errors')
  return (
    <AuthAlert
      tone='rate-limited'
      title={t('rateLimitedTitle')}
      description={t('rateLimitWait', { seconds })}
      // The countdown ticks every second: keep it silent for AT and let the
      // initial insertion announce the remaining wait once.
      descriptionLive='off'
    />
  )
}
