import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { Check, Languages, Laptop, Moon, RotateCcw, Sun } from 'lucide-react'
import { toast } from 'sonner'
import {
  changeAppLanguage,
  getCurrentLanguage,
  type SupportedLanguage,
} from '@/i18n/index'
import { cn } from '@/lib/utils'
import { useLayout } from '@/context/layout-provider'
import { useTheme } from '@/context/theme-provider'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { ContentSection } from './content-section'

const THEME_OPTIONS = ['light', 'dark', 'system'] as const
const LANGUAGE_OPTIONS: SupportedLanguage[] = ['zh-CN', 'en-US']
const SIDEBAR_VARIANTS = ['inset', 'sidebar', 'floating'] as const
const SIDEBAR_COLLAPSIBLES = ['icon', 'offcanvas', 'none'] as const

type SidebarVariant = (typeof SIDEBAR_VARIANTS)[number]
type SidebarCollapsible = (typeof SIDEBAR_COLLAPSIBLES)[number]

function MiniAppWindow({
  tone,
  testId,
}: {
  tone: 'light' | 'dark'
  testId?: string
}) {
  return (
    <div
      data-testid={testId}
      className={cn(
        tone,
        'flex h-full w-full overflow-hidden bg-background text-foreground'
      )}
    >
      {/* Miniature Sidebar */}
      <div className='flex w-[26%] shrink-0 flex-col gap-1.5 border-e border-border bg-muted/50 p-2'>
        <div className='flex items-center gap-1'>
          <div className='size-2.5 rounded-[3px] bg-primary' />
          <div className='h-1.5 w-7 rounded-full bg-foreground/25' />
        </div>
        <div className='mt-1 space-y-1'>
          <div className='h-2 w-full rounded-[3px] bg-primary/20' />
          <div className='h-1.5 w-4/5 rounded-full bg-muted-foreground/25' />
          <div className='h-1.5 w-3/4 rounded-full bg-muted-foreground/20' />
          <div className='h-1.5 w-5/6 rounded-full bg-muted-foreground/20' />
        </div>
      </div>

      {/* Miniature Main Area */}
      <div className='flex flex-1 flex-col bg-background'>
        <div className='flex items-center justify-between border-b border-border px-2.5 py-1.5'>
          <div className='h-2 w-12 rounded-full bg-muted' />
          <div className='flex items-center gap-1'>
            <div className='size-2 rounded-full bg-muted' />
            <div className='size-2.5 rounded-full bg-primary/70' />
          </div>
        </div>
        <div className='grid flex-1 grid-cols-2 gap-1.5 p-2'>
          <div className='space-y-1.5 rounded-[4px] border border-border bg-card p-1.5 shadow-2xs'>
            <div className='h-1.5 w-7 rounded-full bg-muted-foreground/30' />
            <div className='h-2.5 w-10 rounded-[2px] bg-primary/80' />
            <div className='h-1.5 w-full rounded-full bg-muted' />
          </div>
          <div className='space-y-1.5 rounded-[4px] border border-border bg-card p-1.5 shadow-2xs'>
            <div className='h-1.5 w-8 rounded-full bg-muted-foreground/30' />
            <div className='h-2.5 w-8 rounded-[2px] bg-muted-foreground/30' />
            <div className='h-1.5 w-4/5 rounded-full bg-muted' />
          </div>
          <div className='col-span-2 flex items-center gap-1.5 rounded-[4px] border border-border bg-card px-1.5 py-1'>
            <div className='h-1.5 w-6 rounded-full bg-primary/50' />
            <div className='h-1.5 flex-1 rounded-full bg-muted' />
          </div>
        </div>
      </div>
    </div>
  )
}

function ThemePreviewCard({ mode }: { mode: 'light' | 'dark' | 'system' }) {
  if (mode === 'system') {
    return (
      <div className='relative grid h-28 w-full grid-cols-2 overflow-hidden rounded-lg border border-border shadow-2xs'>
        <div className='overflow-hidden border-e border-border'>
          <div className='h-full w-[200%]'>
            <MiniAppWindow tone='light' />
          </div>
        </div>
        <div className='overflow-hidden'>
          <div className='h-full w-[200%] -translate-x-1/2'>
            <MiniAppWindow tone='dark' />
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className='h-28 w-full overflow-hidden rounded-lg border border-border shadow-2xs'>
      <MiniAppWindow tone={mode} testId={`appearance-preview-${mode}`} />
    </div>
  )
}

function VariantPreview({ variant }: { variant: SidebarVariant }) {
  if (variant === 'inset') {
    return (
      <div className='flex h-14 w-full gap-1.5 rounded-md border border-border bg-muted/60 p-1.5'>
        <div className='flex w-6 shrink-0 flex-col gap-1 p-0.5'>
          <div className='h-1.5 w-3.5 rounded-full bg-primary/70' />
          <div className='h-1 w-full rounded-full bg-muted-foreground/30' />
          <div className='h-1 w-4/5 rounded-full bg-muted-foreground/25' />
        </div>
        <div className='flex-1 rounded-[4px] border border-border bg-background shadow-2xs' />
      </div>
    )
  }

  if (variant === 'floating') {
    return (
      <div className='flex h-14 w-full gap-1.5 rounded-md border border-border bg-background p-1.5'>
        <div className='flex w-6 shrink-0 flex-col gap-1 rounded-[4px] border border-border bg-muted/60 p-1 shadow-2xs'>
          <div className='h-1.5 w-3 rounded-full bg-primary/70' />
          <div className='h-1 w-full rounded-full bg-muted-foreground/30' />
          <div className='h-1 w-3/4 rounded-full bg-muted-foreground/25' />
        </div>
        <div className='flex-1 rounded-[4px] bg-muted/25' />
      </div>
    )
  }

  return (
    <div className='flex h-14 w-full overflow-hidden rounded-md border border-border bg-background'>
      <div className='flex w-7 shrink-0 flex-col gap-1 border-e border-border bg-muted/50 p-1.5'>
        <div className='h-1.5 w-3.5 rounded-full bg-primary/70' />
        <div className='h-1 w-full rounded-full bg-muted-foreground/30' />
        <div className='h-1 w-4/5 rounded-full bg-muted-foreground/25' />
      </div>
      <div className='flex-1 p-1.5'>
        <div className='h-full rounded-[3px] bg-muted/25' />
      </div>
    </div>
  )
}

function CollapsiblePreview({ mode }: { mode: SidebarCollapsible }) {
  if (mode === 'icon') {
    return (
      <div className='flex h-14 w-full overflow-hidden rounded-md border border-border bg-background'>
        <div className='flex w-5 shrink-0 flex-col items-center gap-1.5 border-e border-border bg-muted/50 py-2'>
          <div className='size-2 rounded-[2px] bg-primary/75' />
          <div className='size-1.5 rounded-full bg-muted-foreground/40' />
          <div className='size-1.5 rounded-full bg-muted-foreground/30' />
        </div>
        <div className='flex-1 p-1.5'>
          <div className='h-full rounded-[3px] bg-muted/25' />
        </div>
      </div>
    )
  }

  if (mode === 'offcanvas') {
    return (
      <div className='flex h-14 w-full flex-col overflow-hidden rounded-md border border-border bg-background p-1.5'>
        <div className='mb-1 flex items-center gap-1'>
          <div className='h-1.5 w-2.5 rounded-[2px] bg-primary/75' />
          <div className='h-1.5 w-8 rounded-full bg-muted' />
        </div>
        <div className='flex-1 rounded-[3px] bg-muted/25' />
      </div>
    )
  }

  return (
    <div className='flex h-14 w-full overflow-hidden rounded-md border border-border bg-background'>
      <div className='flex w-9 shrink-0 flex-col gap-1 border-e border-border bg-muted/50 p-1.5'>
        <div className='h-1.5 w-5 rounded-full bg-primary/75' />
        <div className='h-1 w-full rounded-full bg-muted-foreground/35' />
        <div className='h-1 w-4/5 rounded-full bg-muted-foreground/25' />
      </div>
      <div className='flex-1 p-1.5'>
        <div className='h-full rounded-[3px] bg-muted/25' />
      </div>
    </div>
  )
}

export function AppearanceSection() {
  const { t, i18n } = useTranslation('settings')
  const { t: tCommon } = useTranslation('common')
  const queryClient = useQueryClient()
  const { theme, setTheme } = useTheme()
  const { variant, setVariant, collapsible, setCollapsible, resetLayout } =
    useLayout()
  const currentLang = getCurrentLanguage()

  const handleLanguageSelect = async (lng: SupportedLanguage) => {
    if (lng === currentLang) return
    await changeAppLanguage(lng)
    void queryClient.invalidateQueries()
    toast.success(i18n.getFixedT(lng, 'settings')('appearance.languageChanged'))
  }

  const handleResetLayout = () => {
    resetLayout()
    toast.success(t('appearance.layoutReset'))
  }

  return (
    <ContentSection>
      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-6 pb-4'>
          <CardTitle className='text-base'>
            {t('appearance.themeTitle')}
          </CardTitle>
          <CardDescription>{t('appearance.themeDescription')}</CardDescription>
        </CardHeader>
        <CardContent className='px-6 pb-6'>
          <div
            role='group'
            aria-label={t('appearance.themeTitle')}
            className='grid grid-cols-1 gap-4 sm:grid-cols-3'
          >
            {THEME_OPTIONS.map((option) => {
              const selected = theme === option
              const Icon =
                option === 'light' ? Sun : option === 'dark' ? Moon : Laptop
              const label =
                option === 'light'
                  ? t('appearance.themeLight')
                  : option === 'dark'
                    ? t('appearance.themeDark')
                    : t('appearance.themeSystem')
              return (
                <button
                  key={option}
                  type='button'
                  aria-pressed={selected}
                  data-testid={`appearance-theme-${option}`}
                  onClick={() => setTheme(option)}
                  className={cn(
                    'group flex flex-col gap-2.5 rounded-xl border p-2.5 text-start transition-all focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                    selected
                      ? 'border-primary bg-accent/30 ring-2 ring-primary/20'
                      : 'border-border hover:border-muted-foreground/50 hover:bg-muted/30'
                  )}
                >
                  <ThemePreviewCard mode={option} />
                  <div className='flex items-center justify-between px-1 text-sm font-medium'>
                    <span className='flex items-center gap-1.5'>
                      <Icon className='size-3.5 text-muted-foreground' />
                      {label}
                    </span>
                    {selected && (
                      <span className='flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground'>
                        <Check className='size-2.5' />
                      </span>
                    )}
                  </div>
                </button>
              )
            })}
          </div>
        </CardContent>
      </Card>

      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='p-6 pb-4'>
          <CardTitle className='text-base'>
            {t('appearance.languageTitle')}
          </CardTitle>
          <CardDescription>
            {t('appearance.languageDescription')}
          </CardDescription>
        </CardHeader>
        <CardContent className='px-6 pb-6'>
          <div
            role='group'
            aria-label={t('appearance.languageTitle')}
            className='grid grid-cols-1 gap-3 sm:grid-cols-2'
          >
            {LANGUAGE_OPTIONS.map((lng) => {
              const selected = currentLang === lng
              const label =
                lng === 'zh-CN'
                  ? tCommon('language.zhCN')
                  : tCommon('language.enUS')
              return (
                <button
                  key={lng}
                  type='button'
                  aria-pressed={selected}
                  data-testid={`appearance-lang-${lng}`}
                  onClick={() => void handleLanguageSelect(lng)}
                  className={cn(
                    'flex items-center justify-between rounded-xl border px-4 py-3.5 text-start transition-all focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                    selected
                      ? 'border-primary bg-accent/30 ring-2 ring-primary/20'
                      : 'border-border hover:border-muted-foreground/50 hover:bg-muted/30'
                  )}
                >
                  <span className='flex items-center gap-3'>
                    <span className='flex size-9 items-center justify-center rounded-lg border bg-muted/50 text-muted-foreground'>
                      <Languages className='size-4' />
                    </span>
                    <span className='flex flex-col'>
                      <span className='text-sm font-medium'>{label}</span>
                      <span className='font-mono text-xs text-muted-foreground'>
                        {lng}
                      </span>
                    </span>
                  </span>
                  {selected && (
                    <span className='flex size-5 items-center justify-center rounded-full bg-primary text-primary-foreground'>
                      <Check className='size-3' />
                    </span>
                  )}
                </button>
              )
            })}
          </div>
        </CardContent>
      </Card>

      <Card className='gap-0 overflow-hidden py-0'>
        <CardHeader className='flex flex-col gap-3 p-6 pb-4 sm:flex-row sm:items-start sm:justify-between'>
          <div className='space-y-1'>
            <CardTitle className='text-base'>
              {t('appearance.layoutTitle')}
            </CardTitle>
            <CardDescription>
              {t('appearance.layoutDescription')}
            </CardDescription>
          </div>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={handleResetLayout}
          >
            <RotateCcw className='size-3.5' />
            {t('appearance.resetLayout')}
          </Button>
        </CardHeader>

        <CardContent className='space-y-6 px-6 pb-6'>
          <div className='space-y-2.5'>
            <p className='text-xs font-medium text-muted-foreground'>
              {t('appearance.sidebarVariant')}
            </p>
            <div
              role='group'
              aria-label={t('appearance.sidebarVariant')}
              className='grid grid-cols-1 gap-3 sm:grid-cols-3'
            >
              {SIDEBAR_VARIANTS.map((item) => {
                const selected = variant === item
                const label =
                  item === 'inset'
                    ? t('appearance.variantInset')
                    : item === 'sidebar'
                      ? t('appearance.variantSidebar')
                      : t('appearance.variantFloating')
                return (
                  <button
                    key={item}
                    type='button'
                    aria-pressed={selected}
                    onClick={() => setVariant(item)}
                    className={cn(
                      'flex flex-col gap-2 rounded-xl border p-2.5 text-start transition-all focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                      selected
                        ? 'border-primary bg-accent/30 ring-2 ring-primary/20'
                        : 'border-border hover:border-muted-foreground/50 hover:bg-muted/30'
                    )}
                  >
                    <VariantPreview variant={item} />
                    <div className='flex items-center justify-between px-1 text-xs font-medium'>
                      <span>{label}</span>
                      {selected && (
                        <span className='flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground'>
                          <Check className='size-2.5' />
                        </span>
                      )}
                    </div>
                  </button>
                )
              })}
            </div>
          </div>

          <div className='space-y-2.5'>
            <p className='text-xs font-medium text-muted-foreground'>
              {t('appearance.sidebarCollapsible')}
            </p>
            <div
              role='group'
              aria-label={t('appearance.sidebarCollapsible')}
              className='grid grid-cols-1 gap-3 sm:grid-cols-3'
            >
              {SIDEBAR_COLLAPSIBLES.map((item) => {
                const selected = collapsible === item
                const label =
                  item === 'icon'
                    ? t('appearance.collapsibleIcon')
                    : item === 'offcanvas'
                      ? t('appearance.collapsibleOffcanvas')
                      : t('appearance.collapsibleNone')
                return (
                  <button
                    key={item}
                    type='button'
                    aria-pressed={selected}
                    onClick={() => setCollapsible(item)}
                    className={cn(
                      'flex flex-col gap-2 rounded-xl border p-2.5 text-start transition-all focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                      selected
                        ? 'border-primary bg-accent/30 ring-2 ring-primary/20'
                        : 'border-border hover:border-muted-foreground/50 hover:bg-muted/30'
                    )}
                  >
                    <CollapsiblePreview mode={item} />
                    <div className='flex items-center justify-between px-1 text-xs font-medium'>
                      <span>{label}</span>
                      {selected && (
                        <span className='flex size-4 items-center justify-center rounded-full bg-primary text-primary-foreground'>
                          <Check className='size-2.5' />
                        </span>
                      )}
                    </div>
                  </button>
                )
              })}
            </div>
          </div>
        </CardContent>
      </Card>
    </ContentSection>
  )
}
