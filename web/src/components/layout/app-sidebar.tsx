import { Link } from '@tanstack/react-router'
import { Logo } from '@/assets/logo'
import { useLayout } from '@/context/layout-provider'
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from '@/components/ui/sidebar'
import { NavGroup } from './nav-group'
import { NavUser } from './nav-user'
import { type LayoutUser, type NavGroup as NavGroupType } from './types'

type AppSidebarProps = {
  appName: string
  appSubtitle?: string
  navGroups: NavGroupType[]
  user: LayoutUser
  onSignOut: () => Promise<void> | void
}

export function AppSidebar({
  appName,
  appSubtitle,
  navGroups,
  user,
  onSignOut,
}: AppSidebarProps) {
  const { collapsible, variant } = useLayout()

  return (
    <Sidebar collapsible={collapsible} variant={variant}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton size='lg' asChild>
              <Link to='/'>
                <div className='flex aspect-square size-8 shrink-0 items-center justify-center rounded-lg bg-sidebar-primary text-sidebar-primary-foreground'>
                  <Logo className='size-[18px]' />
                </div>
                <div className='grid flex-1 gap-0.5 text-start leading-none group-data-[collapsible=icon]:hidden'>
                  <span className='truncate font-brand text-[15.5px] font-bold tracking-[-0.025em]'>
                    {appName}
                  </span>
                  {appSubtitle && (
                    <span className='truncate text-[11px] font-medium tracking-[0.02em] text-sidebar-foreground/60'>
                      {appSubtitle}
                    </span>
                  )}
                </div>
              </Link>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>
      <SidebarContent>
        {navGroups.map((group) => (
          <NavGroup key={group.title} {...group} />
        ))}
      </SidebarContent>
      <SidebarFooter>
        <NavUser user={user} onSignOut={onSignOut} />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  )
}
