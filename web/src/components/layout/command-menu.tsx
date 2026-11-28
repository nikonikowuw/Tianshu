import { useCallback } from 'react'
import { useTranslation } from 'react-i18next'
import { useNavigate } from '@tanstack/react-router'
import {
  ArrowRight,
  KeyRound,
  Laptop,
  MonitorSmartphone,
  Moon,
  Palette,
  Sun,
  UserCog,
} from 'lucide-react'
import { useSearch } from '@/context/search-provider'
import { useTheme } from '@/context/theme-provider'
import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
  CommandSeparator,
} from '@/components/ui/command'
import { ScrollArea } from '@/components/ui/scroll-area'
import { type NavGroup } from './types'

type CommandMenuProps = {
  navGroups: NavGroup[]
}

export function CommandMenu({ navGroups }: CommandMenuProps) {
  const { t } = useTranslation('common')
  const navigate = useNavigate()
  const { setTheme } = useTheme()
  const { open, setOpen } = useSearch()

  const runCommand = useCallback(
    (command: () => unknown) => {
      setOpen(false)
      command()
    },
    [setOpen]
  )

  return (
    <CommandDialog
      modal
      open={open}
      onOpenChange={setOpen}
      title={t('command.title')}
      description={t('command.description')}
    >
      <CommandInput placeholder={t('command.placeholder')} />
      <CommandList>
        <ScrollArea type='hover' className='h-72 pe-1'>
          <CommandEmpty>{t('command.noResults')}</CommandEmpty>
          {navGroups.map((group) => (
            <CommandGroup key={group.title} heading={group.title}>
              {group.items.map((navItem, i) => {
                if (navItem.url) {
                  return (
                    <CommandItem
                      key={`${navItem.url}-${i}`}
                      value={navItem.title}
                      onSelect={() => {
                        runCommand(() => navigate({ to: navItem.url }))
                      }}
                    >
                      <div className='flex size-4 items-center justify-center'>
                        <ArrowRight className='size-2 text-muted-foreground/80' />
                      </div>
                      {navItem.title}
                    </CommandItem>
                  )
                }

                return navItem.items?.map((subItem, j) => (
                  <CommandItem
                    key={`${navItem.title}-${subItem.url}-${j}`}
                    value={`${navItem.title} ${subItem.title}`}
                    onSelect={() => {
                      runCommand(() => navigate({ to: subItem.url }))
                    }}
                  >
                    <div className='flex size-4 items-center justify-center'>
                      <ArrowRight className='size-2 text-muted-foreground/80' />
                    </div>
                    {navItem.title} / {subItem.title}
                  </CommandItem>
                ))
              })}
            </CommandGroup>
          ))}
          <CommandSeparator />
          <CommandGroup heading={t('nav.settings')}>
            <CommandItem
              value={`${t('nav.settings')} ${t('nav.profile')}`}
              onSelect={() => runCommand(() => navigate({ to: '/settings' }))}
            >
              <UserCog />
              <span>{t('nav.profile')}</span>
            </CommandItem>
            <CommandItem
              value={`${t('nav.settings')} ${t('nav.security')}`}
              onSelect={() =>
                runCommand(() => navigate({ to: '/settings/security' }))
              }
            >
              <KeyRound />
              <span>{t('nav.security')}</span>
            </CommandItem>
            <CommandItem
              value={`${t('nav.settings')} ${t('nav.sessions')}`}
              onSelect={() =>
                runCommand(() => navigate({ to: '/settings/sessions' }))
              }
            >
              <MonitorSmartphone />
              <span>{t('nav.sessions')}</span>
            </CommandItem>
            <CommandItem
              value={`${t('nav.settings')} ${t('nav.appearance')}`}
              onSelect={() =>
                runCommand(() => navigate({ to: '/settings/appearance' }))
              }
            >
              <Palette />
              <span>{t('nav.appearance')}</span>
            </CommandItem>
          </CommandGroup>
          <CommandSeparator />
          <CommandGroup heading={t('command.theme')}>
            <CommandItem onSelect={() => runCommand(() => setTheme('light'))}>
              <Sun /> <span>{t('command.light')}</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('dark'))}>
              <Moon className='scale-90' />
              <span>{t('command.dark')}</span>
            </CommandItem>
            <CommandItem onSelect={() => runCommand(() => setTheme('system'))}>
              <Laptop />
              <span>{t('command.system')}</span>
            </CommandItem>
          </CommandGroup>
        </ScrollArea>
      </CommandList>
    </CommandDialog>
  )
}
