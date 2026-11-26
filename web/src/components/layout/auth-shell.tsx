import { Component, lazy, Suspense, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { Logo } from '@/assets/logo'
import { cn } from '@/lib/utils'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { ThemeSwitch } from '@/components/shared/theme-switch'

const AuthThreeBackdrop = lazy(() => import('./auth-three-backdrop'))

/**
 * Isolates decorative WebGL/chunk load failures so unauthenticated forms remain
 * usable over the CSS gradient fallback even if the lazy chunk fails to load.
 */
class BackdropErrorBoundary extends Component<
  { children: ReactNode },
  { hasError: boolean }
> {
  state = { hasError: false }

  static getDerivedStateFromError() {
    return { hasError: true }
  }

  render() {
    return this.state.hasError ? null : this.props.children
  }
}

type AuthShellProps = {
  /** Optional subtle label rendered above the heading (e.g. setup context). */
  eyebrow?: string
  title: string
  description: string
  /** Header alignment inside the card. */
  headerAlign?: 'center' | 'start'
  /** Max width of the focal column; wider for multi-field wizards. */
  maxWidthClass?: string
  /** Optional secondary note rendered quietly beneath the card. */
  belowCard?: ReactNode
  children: ReactNode
}

/**
 * Shared centered shell for unauthenticated screens (sign-in, setup and future
 * recovery flows).
 *
 * Pairs an Apple/Stripe-inspired frosted-glass card with a lazy-loaded,
 * theme-adaptive Three.js liquid silk aurora mesh so light and dark modes feel
 * fluid and luminous while keeping form controls crisp.
 */
export function AuthShell({
  eyebrow,
  title,
  description,
  headerAlign = 'center',
  maxWidthClass = 'max-w-[400px]',
  belowCard,
  children,
}: AuthShellProps) {
  const { t } = useTranslation(['auth', 'common'])

  return (
    <div className='relative flex min-h-svh w-full flex-col overflow-x-clip bg-muted/55 text-foreground dark:bg-background'>
      {/* ── CSS instant first-paint fallback + lazy Three.js silk aurora ── */}
      <div
        aria-hidden='true'
        className='pointer-events-none fixed inset-0 overflow-hidden select-none'
      >
        <div className='absolute inset-0 bg-[radial-gradient(ellipse_80%_60%_at_25%_20%,oklch(0.92_0.045_245/0.65),transparent_65%),radial-gradient(ellipse_75%_60%_at_80%_75%,oklch(0.91_0.05_285/0.55),transparent_65%)] dark:bg-[radial-gradient(ellipse_80%_60%_at_25%_20%,oklch(0.28_0.09_255/0.55),transparent_65%),radial-gradient(ellipse_75%_60%_at_80%_75%,oklch(0.25_0.1_285/0.5),transparent_65%)]' />

        <BackdropErrorBoundary>
          <Suspense fallback={null}>
            <AuthThreeBackdrop />
          </Suspense>
        </BackdropErrorBoundary>
      </div>

      {/* ── Quiet top chrome ────────────────────────────────────────────── */}
      <header className='relative z-10 flex h-16 w-full shrink-0 items-center justify-end px-5 sm:px-8'>
        <div className='flex items-center gap-0.5 rounded-full border border-border/75 bg-card/75 p-0.5 shadow-2xs backdrop-blur-xl dark:border-foreground/12 dark:bg-card/65'>
          <LanguageSwitch />
          <span className='h-3.5 w-px bg-border/80' aria-hidden='true' />
          <ThemeSwitch />
        </div>
      </header>

      {/* ── Centered focal stage ────────────────────────────────────────── */}
      <main className='relative z-10 flex flex-1 flex-col items-center justify-center px-4 pt-1 pb-10 sm:px-6'>
        <div className={cn('w-full', maxWidthClass)}>
          {/* Brand lockup */}
          <div className='mb-6 flex items-center justify-center gap-2.5'>
            <div className='flex size-9 items-center justify-center rounded-xl border border-background/80 bg-card/85 text-foreground shadow-[0_1px_2px_oklch(0_0_0/0.06),0_6px_16px_-4px_oklch(0.2_0.04_265/0.12)] backdrop-blur-xl dark:border-foreground/15 dark:bg-card/75 dark:shadow-[0_1px_0_oklch(1_0_0/0.1)_inset,0_8px_20px_-4px_oklch(0_0_0/0.55)]'>
              <Logo className='size-[18px]' />
            </div>
            <span className='text-[15px] font-semibold tracking-tight text-foreground'>
              {t('common:appName')}
            </span>
          </div>

          {/* Frosted glass card */}
          <section className='rounded-2xl border border-background/80 bg-card/80 p-6 shadow-[0_1px_2px_oklch(0_0_0/0.05),0_20px_50px_-12px_oklch(0.2_0.04_265/0.14)] ring-1 ring-border/55 backdrop-blur-2xl sm:p-8 dark:border-foreground/14 dark:bg-card/72 dark:shadow-[0_1px_0_oklch(1_0_0/0.1)_inset,0_28px_64px_-12px_oklch(0_0_0/0.65)] dark:ring-0'>
            <div
              className={cn(
                'mb-6 space-y-1.5',
                headerAlign === 'center' ? 'text-center' : 'text-start'
              )}
            >
              {eyebrow && (
                <p className='text-xs font-medium tracking-wide text-muted-foreground'>
                  {eyebrow}
                </p>
              )}
              <h1 className='text-[1.375rem] leading-tight font-semibold tracking-tight text-balance text-foreground'>
                {title}
              </h1>
              <p className='text-sm leading-relaxed text-pretty text-muted-foreground'>
                {description}
              </p>
            </div>

            {children}
          </section>

          {belowCard && (
            <div className='mt-5 text-center text-xs leading-relaxed text-foreground/65'>
              {belowCard}
            </div>
          )}
        </div>
      </main>

      {/* ── Whisper-quiet footer ────────────────────────────────────────── */}
      <footer className='relative z-10 flex shrink-0 items-center justify-center px-6 pb-5 text-[11px] text-foreground/55'>
        {t('auth:brand.footer')}
      </footer>
    </div>
  )
}
