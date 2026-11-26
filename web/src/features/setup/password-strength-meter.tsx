import { useTranslation } from 'react-i18next'
import { cn } from '@/lib/utils'
import { passwordLevel, type PasswordStrengthLevel } from './password-strength'

const LEVEL_BARS: Record<PasswordStrengthLevel, number> = {
  weak: 1,
  fair: 2,
  good: 3,
  strong: 4,
}

const BAR_COLOR: Record<PasswordStrengthLevel, string> = {
  weak: 'bg-destructive',
  fair: 'bg-warning',
  good: 'bg-info',
  strong: 'bg-success',
}

type PasswordStrengthProps = {
  value: string
  className?: string
}

/**
 * Four-segment strength meter with an accessible text label.
 *
 * Rendered as a decorative meter plus an `aria-live="polite"` text node so the
 * level is announced without being read on every keystroke.
 */
export function PasswordStrength({ value, className }: PasswordStrengthProps) {
  const { t } = useTranslation('auth')
  const level = passwordLevel(value)

  if (!level) return null

  const filled = LEVEL_BARS[level]

  return (
    <div className={cn('grid gap-1.5', className)} aria-live='polite'>
      <div className='flex gap-1' aria-hidden='true'>
        {[1, 2, 3, 4].map((step) => (
          <span
            key={step}
            className={cn(
              'h-1 flex-1 rounded-full transition-colors duration-300 motion-reduce:transition-none',
              step <= filled ? BAR_COLOR[level] : 'bg-muted'
            )}
          />
        ))}
      </div>
      <p className='text-[11px] text-muted-foreground'>
        {t(`setup.passwordStrength.${level}`)}
      </p>
    </div>
  )
}
