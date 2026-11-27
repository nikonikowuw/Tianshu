import { type ComponentProps, type JSX } from 'react'
import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { cn } from '@/lib/utils'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

export type SettingsNavItem = {
  href:
    | '/settings'
    | '/settings/security'
    | '/settings/sessions'
    | '/settings/appearance'
  title: string
  icon: JSX.Element
}

type SidebarNavProps = ComponentProps<'nav'> & {
  items: SettingsNavItem[]
  selectLabel?: string
}

function normalizePathname(pathname: string): string {
  if (pathname.length <= 1) return pathname
  return pathname.replace(/\/+$/, '')
}

export function SidebarNav({
  className,
  items,
  selectLabel,
  ...props
}: SidebarNavProps) {
  const { pathname } = useLocation()
  const navigate = useNavigate()
  const normalizedPath = normalizePathname(pathname)

  const currentValue =
    items.find((item) => item.href === normalizedPath)?.href ??
    items[0]?.href ??
    '/settings'

  const handleSelect = (value: string) => {
    void navigate({ to: value as SettingsNavItem['href'] })
  }

  return (
    <>
      <div className='sm:hidden'>
        <Select value={currentValue} onValueChange={handleSelect}>
          <SelectTrigger className='h-10 w-full' aria-label={selectLabel}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {items.map((item) => (
              <SelectItem key={item.href} value={item.href}>
                <div className='flex items-center gap-2.5'>
                  {item.icon}
                  <span>{item.title}</span>
                </div>
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <nav
        aria-label={selectLabel}
        className={cn(
          'hidden items-center gap-1 border-b border-border sm:flex',
          className
        )}
        {...props}
      >
        {items.map((item) => {
          const active = normalizedPath === item.href
          return (
            <Link
              key={item.href}
              to={item.href}
              aria-current={active ? 'page' : undefined}
              className={cn(
                'relative inline-flex items-center gap-2 rounded-t-md px-3.5 py-2.5 text-sm font-medium whitespace-nowrap transition-colors focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-hidden',
                active
                  ? 'text-foreground after:absolute after:inset-x-1 after:-bottom-px after:h-0.5 after:rounded-full after:bg-primary'
                  : 'text-muted-foreground hover:bg-muted/50 hover:text-foreground'
              )}
            >
              <span
                className={cn(
                  'transition-colors',
                  active ? 'text-primary' : 'text-muted-foreground'
                )}
              >
                {item.icon}
              </span>
              {item.title}
            </Link>
          )
        })}
      </nav>
    </>
  )
}
