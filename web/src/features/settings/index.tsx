import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { Outlet } from '@tanstack/react-router'
import { KeyRound, MonitorSmartphone, Palette, UserCog } from 'lucide-react'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { SidebarNav, type SettingsNavItem } from './components/sidebar-nav'

export function Settings() {
  const { t } = useTranslation('settings')
  const queryClient = useQueryClient()

  const navItems: SettingsNavItem[] = useMemo(
    () => [
      {
        title: t('nav.profile'),
        href: '/settings',
        icon: <UserCog className='size-4' />,
      },
      {
        title: t('nav.security'),
        href: '/settings/security',
        icon: <KeyRound className='size-4' />,
      },
      {
        title: t('nav.sessions'),
        href: '/settings/sessions',
        icon: <MonitorSmartphone className='size-4' />,
      },
      {
        title: t('nav.appearance'),
        href: '/settings/appearance',
        icon: <Palette className='size-4' />,
      },
    ],
    [t]
  )

  return (
    <>
      <Header fixed>
        <Search />
        <div className='ms-auto flex items-center space-x-2'>
          <LanguageSwitch
            onLanguageChange={() => {
              void queryClient.invalidateQueries()
            }}
          />
          <ThemeSwitch />
        </div>
      </Header>

      <Main fixed fluid className='p-0'>
        <div className='min-h-0 flex-1 overflow-y-auto px-4 py-6 [scrollbar-gutter:stable] sm:px-6'>
          <div className='mx-auto w-full max-w-4xl space-y-6 pb-10'>
            <div className='space-y-1'>
              <h1
                data-testid='settings-page-title'
                className='text-2xl font-bold tracking-tight'
              >
                {t('title')}
              </h1>
              <p className='text-sm text-muted-foreground'>
                {t('description')}
              </p>
            </div>

            <SidebarNav items={navItems} selectLabel={t('title')} />

            <div className='min-w-0 pt-1'>
              <Outlet />
            </div>
          </div>
        </div>
      </Main>
    </>
  )
}

export { AppearanceSection } from './components/appearance-section'
export { ProfileSection } from './components/profile-section'
export { SecuritySection } from './components/security-section'
export { SessionsSection } from './components/sessions-section'
