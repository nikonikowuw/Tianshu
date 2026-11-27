import { useTranslation } from 'react-i18next'
import { Link } from '@tanstack/react-router'
import {
  ChevronsUpDown,
  KeyRound,
  LogOut,
  MonitorSmartphone,
  Palette,
  UserCog,
} from 'lucide-react'
import { getDisplayNameInitials } from '@/lib/utils'
import useDialogState from '@/hooks/use-dialog-state'
import { Avatar, AvatarFallback, AvatarImage } from '@/components/ui/avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from '@/components/ui/sidebar'
import { ConfirmDialog } from '@/components/shared/confirm-dialog'
import { type LayoutUser } from './types'

type NavUserProps = {
  user: LayoutUser
  onSignOut: () => Promise<void> | void
  onAvatarError?: () => void
}

export function NavUser({ user, onSignOut, onAvatarError }: NavUserProps) {
  const { t } = useTranslation('common')
  const { isMobile, setOpenMobile } = useSidebar()
  const [open, setOpen] = useDialogState()

  const handleConfirmSignOut = () => {
    setOpen(false)
    void onSignOut()
  }

  return (
    <>
      <SidebarMenu>
        <SidebarMenuItem>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <SidebarMenuButton
                size='lg'
                data-testid='nav-user-trigger'
                className='data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground'
              >
                <Avatar className='size-8 rounded-lg'>
                  {user.avatar && (
                    <AvatarImage
                      src={user.avatar}
                      alt={user.name}
                      onLoadingStatusChange={(status) => {
                        if (status === 'error') onAvatarError?.()
                      }}
                    />
                  )}
                  <AvatarFallback className='rounded-lg'>
                    {getDisplayNameInitials(user.name)}
                  </AvatarFallback>
                </Avatar>
                <div className='grid flex-1 text-start text-sm leading-tight'>
                  <span className='truncate font-semibold'>{user.name}</span>
                  <span className='truncate text-xs'>{user.email}</span>
                </div>
                <ChevronsUpDown className='ms-auto size-4' />
              </SidebarMenuButton>
            </DropdownMenuTrigger>
            <DropdownMenuContent
              className='w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg'
              side={isMobile ? 'bottom' : 'right'}
              align='end'
              sideOffset={4}
            >
              <DropdownMenuLabel className='p-0 font-normal'>
                <div className='flex items-center gap-2 px-1 py-1.5 text-start text-sm'>
                  <Avatar className='size-8 rounded-lg'>
                    {user.avatar && (
                      <AvatarImage
                        src={user.avatar}
                        alt={user.name}
                        onLoadingStatusChange={(status) => {
                          if (status === 'error') onAvatarError?.()
                        }}
                      />
                    )}
                    <AvatarFallback className='rounded-lg'>
                      {getDisplayNameInitials(user.name)}
                    </AvatarFallback>
                  </Avatar>
                  <div className='grid flex-1 text-start text-sm leading-tight'>
                    <span className='truncate font-semibold'>{user.name}</span>
                    <span className='truncate text-xs'>{user.email}</span>
                  </div>
                </div>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuGroup>
                <DropdownMenuItem asChild>
                  <Link to='/settings' onClick={() => setOpenMobile(false)}>
                    <UserCog />
                    {t('nav.profile')}
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link
                    to='/settings/security'
                    onClick={() => setOpenMobile(false)}
                  >
                    <KeyRound />
                    {t('nav.security')}
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link
                    to='/settings/sessions'
                    onClick={() => setOpenMobile(false)}
                  >
                    <MonitorSmartphone />
                    {t('nav.sessions')}
                  </Link>
                </DropdownMenuItem>
                <DropdownMenuItem asChild>
                  <Link
                    to='/settings/appearance'
                    onClick={() => setOpenMobile(false)}
                  >
                    <Palette />
                    {t('nav.appearance')}
                  </Link>
                </DropdownMenuItem>
              </DropdownMenuGroup>
              <DropdownMenuSeparator />
              <DropdownMenuItem
                variant='destructive'
                onClick={() => setOpen(true)}
              >
                <LogOut />
                {t('signOut')}
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </SidebarMenuItem>
      </SidebarMenu>

      <ConfirmDialog
        open={Boolean(open)}
        onOpenChange={setOpen}
        title={t('signOutTitle')}
        desc={t('signOutDesc')}
        cancelBtnText={t('cancel')}
        confirmText={t('signOut')}
        destructive
        handleConfirm={handleConfirmSignOut}
      />
    </>
  )
}
