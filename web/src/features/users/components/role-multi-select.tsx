import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Check, ChevronsUpDown, ShieldCheck, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command'
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover'
import type { RoleOption } from '../types'

type RoleMultiSelectProps = {
  id?: string
  'aria-describedby'?: string
  'aria-invalid'?: boolean
  options: RoleOption[]
  value: string[]
  onChange: (next: string[]) => void
  disabled?: boolean
  isLoading?: boolean
  isError?: boolean
  onRetry?: () => void
  actorIsSuperAdmin: boolean
  isSelf?: boolean
}

export function RoleMultiSelect({
  id,
  'aria-describedby': ariaDescribedBy,
  'aria-invalid': ariaInvalid,
  options,
  value,
  onChange,
  disabled,
  isLoading = false,
  isError = false,
  onRetry,
  actorIsSuperAdmin,
  isSelf = false,
}: RoleMultiSelectProps) {
  const { t } = useTranslation('users')
  const { t: tCommon } = useTranslation('common')
  const [open, setOpen] = useState(false)

  const selectedOptions = useMemo(() => {
    const set = new Set(value)
    return options.filter((o) => set.has(o.id))
  }, [options, value])

  const isRoleLocked = (role: RoleOption): boolean => {
    if (role.code !== 'super_admin') return false
    if (!actorIsSuperAdmin) return true
    if (isSelf && value.includes(role.id)) return true
    return false
  }

  const toggleRole = (role: RoleOption) => {
    if (isRoleLocked(role)) return
    const exists = value.includes(role.id)
    if (exists) {
      onChange(value.filter((id) => id !== role.id))
    } else {
      onChange([...value, role.id])
    }
  }

  return (
    <div className='space-y-2'>
      <Popover open={open} onOpenChange={setOpen}>
        <PopoverTrigger asChild>
          <Button
            id={id}
            type='button'
            variant='outline'
            role='combobox'
            aria-expanded={open}
            aria-describedby={ariaDescribedBy}
            aria-invalid={ariaInvalid}
            disabled={disabled}
            className='w-full justify-between font-normal'
          >
            <span className='truncate text-muted-foreground'>
              {selectedOptions.length > 0
                ? t('form.rolesSelectedCount', {
                    count: selectedOptions.length,
                  })
                : isLoading
                  ? tCommon('loading')
                  : t('form.rolesPlaceholder')}
            </span>
            <ChevronsUpDown className='ms-2 size-4 shrink-0 opacity-50' />
          </Button>
        </PopoverTrigger>
        <PopoverContent
          className='w-(--radix-popover-trigger-width) p-0'
          align='start'
        >
          <Command>
            <CommandInput placeholder={t('form.rolesSearchPlaceholder')} />
            <CommandList>
              <CommandEmpty>
                {isLoading ? (
                  tCommon('loading')
                ) : isError ? (
                  <span className='inline-flex items-center gap-2 text-destructive'>
                    <span>{t('form.rolesLoadError')}</span>
                    {onRetry && (
                      <button
                        type='button'
                        className='underline hover:opacity-80'
                        onClick={onRetry}
                      >
                        {tCommon('retry')}
                      </button>
                    )}
                  </span>
                ) : (
                  t('form.rolesEmpty')
                )}
              </CommandEmpty>
              <CommandGroup>
                {options.map((role) => {
                  const selected = value.includes(role.id)
                  const locked = isRoleLocked(role)
                  return (
                    <CommandItem
                      key={role.id}
                      value={`${role.name} ${role.code}`}
                      disabled={locked}
                      onSelect={() => toggleRole(role)}
                    >
                      <div
                        className={cn(
                          'me-2 flex size-4 items-center justify-center rounded-sm border border-primary',
                          selected
                            ? 'bg-primary text-primary-foreground'
                            : 'opacity-50 [&_svg]:invisible'
                        )}
                      >
                        <Check className='size-3.5' />
                      </div>
                      <span className='flex-1 truncate'>{role.name}</span>
                      {role.isSystem && (
                        <ShieldCheck className='ms-1 size-3.5 text-muted-foreground' />
                      )}
                    </CommandItem>
                  )
                })}
              </CommandGroup>
            </CommandList>
          </Command>
        </PopoverContent>
      </Popover>

      {isError && (
        <div className='flex items-center justify-between gap-2 text-xs text-destructive'>
          <span>{t('form.rolesLoadError')}</span>
          {onRetry && (
            <Button
              type='button'
              variant='ghost'
              size='sm'
              className='h-6 px-2 text-xs'
              onClick={onRetry}
            >
              {tCommon('retry')}
            </Button>
          )}
        </div>
      )}

      {selectedOptions.length > 0 && (
        <div className='flex flex-wrap gap-1.5'>
          {selectedOptions.map((role) => {
            const locked = isRoleLocked(role)
            return (
              <Badge
                key={role.id}
                variant={role.isSystem ? 'secondary' : 'outline'}
                className='gap-1 font-normal'
              >
                {role.name}
                {!locked && !disabled && (
                  <button
                    type='button'
                    onClick={() => toggleRole(role)}
                    className='ms-0.5 rounded-full hover:bg-muted-foreground/20'
                    aria-label={role.name}
                  >
                    <X className='size-3' />
                  </button>
                )}
              </Badge>
            )
          })}
        </div>
      )}
    </div>
  )
}
