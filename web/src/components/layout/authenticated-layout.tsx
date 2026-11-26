import { Outlet } from '@tanstack/react-router'
import { getCookie } from '@/lib/cookies'
import { cn } from '@/lib/utils'
import { LayoutProvider } from '@/context/layout-provider'
import { SearchProvider } from '@/context/search-provider'
import { SidebarInset, SidebarProvider } from '@/components/ui/sidebar'
import { SkipToMain } from '@/components/shared/skip-to-main'
import { AppSidebar } from './app-sidebar'
import { CommandMenu } from './command-menu'
import { type LayoutUser, type NavGroup } from './types'

type AuthenticatedLayoutProps = {
  appName: string
  navGroups: NavGroup[]
  user: LayoutUser
  onSignOut: () => Promise<void> | void
  children?: React.ReactNode
}

export function AuthenticatedLayout({
  appName,
  navGroups,
  user,
  onSignOut,
  children,
}: AuthenticatedLayoutProps) {
  const defaultOpen = getCookie('sidebar_state') !== 'false'

  return (
    <SearchProvider>
      <LayoutProvider>
        <SidebarProvider defaultOpen={defaultOpen}>
          <SkipToMain />
          <AppSidebar
            appName={appName}
            navGroups={navGroups}
            user={user}
            onSignOut={onSignOut}
          />
          <SidebarInset
            className={cn(
              '@container/content',
              'has-data-[layout=fixed]:h-svh',
              'peer-data-[variant=inset]:has-data-[layout=fixed]:h-[calc(100svh-(var(--spacing)*4))]'
            )}
          >
            {children ?? <Outlet />}
          </SidebarInset>
        </SidebarProvider>
        <CommandMenu navGroups={navGroups} />
      </LayoutProvider>
    </SearchProvider>
  )
}
