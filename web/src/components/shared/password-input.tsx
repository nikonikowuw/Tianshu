import * as React from 'react'
import { useTranslation } from 'react-i18next'
import { Eye, EyeOff } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Button } from '@/components/ui/button'

type PasswordInputProps = Omit<
  React.InputHTMLAttributes<HTMLInputElement>,
  'type'
> & {
  ref?: React.Ref<HTMLInputElement>
  /** Extra classes applied directly to the inner <input> element. */
  inputClassName?: string
}

export function PasswordInput({
  className,
  inputClassName,
  disabled,
  ref,
  ...props
}: PasswordInputProps) {
  const { t } = useTranslation('common')
  const [showPassword, setShowPassword] = React.useState(false)

  return (
    <div className={cn('relative', className)}>
      <input
        data-slot='password-input'
        type={showPassword ? 'text' : 'password'}
        className={cn(
          'flex h-9 w-full min-w-0 rounded-md border border-input bg-transparent px-3 py-1 pe-10 text-base shadow-xs transition-[color,box-shadow] outline-none selection:bg-primary selection:text-primary-foreground file:inline-flex file:h-7 file:border-0 file:bg-transparent file:text-sm file:font-medium file:text-foreground placeholder:text-muted-foreground disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 md:text-sm dark:bg-input/30',
          'focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50',
          'aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40',
          inputClassName
        )}
        ref={ref}
        disabled={disabled}
        {...props}
      />
      <Button
        type='button'
        size='icon'
        variant='ghost'
        disabled={disabled}
        aria-label={showPassword ? t('password.hide') : t('password.show')}
        aria-pressed={showPassword}
        title={showPassword ? t('password.hide') : t('password.show')}
        className='absolute inset-e-1.5 top-1/2 size-7 -translate-y-1/2 rounded-md text-muted-foreground hover:bg-transparent hover:text-foreground'
        onClick={() => setShowPassword((prev) => !prev)}
      >
        {showPassword ? <Eye size={16} /> : <EyeOff size={16} />}
      </Button>
    </div>
  )
}
