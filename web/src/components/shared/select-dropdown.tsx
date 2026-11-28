import { useTranslation } from 'react-i18next'
import { Loader } from 'lucide-react'
import { cn } from '@/lib/utils'
import { FormControl } from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

type SelectDropdownProps = {
  onValueChange?: (value: string) => void
  defaultValue: string | undefined
  placeholder?: string
  isPending?: boolean
  items: { label: string; value: string }[] | undefined
  disabled?: boolean
  className?: string
  isControlled?: boolean
  withFormControl?: boolean
}

export function SelectDropdown({
  defaultValue,
  onValueChange,
  isPending,
  items,
  placeholder,
  disabled,
  className = '',
  isControlled = false,
  withFormControl = false,
}: SelectDropdownProps) {
  const { t } = useTranslation('common')
  const defaultState = isControlled
    ? { value: defaultValue, onValueChange }
    : { defaultValue, onValueChange }
  const trigger = (
    <SelectTrigger disabled={disabled} className={cn(className)}>
      <SelectValue placeholder={placeholder ?? t('selectPlaceholder')} />
    </SelectTrigger>
  )

  return (
    <Select {...defaultState}>
      {withFormControl ? <FormControl>{trigger}</FormControl> : trigger}
      <SelectContent>
        {isPending ? (
          <SelectItem disabled value='loading' className='h-14'>
            <div className='flex items-center justify-center gap-2'>
              <Loader className='size-5 animate-spin' />
              <span>{t('loading')}</span>
            </div>
          </SelectItem>
        ) : (
          items?.map(({ label, value }) => (
            <SelectItem key={value} value={value}>
              {label}
            </SelectItem>
          ))
        )}
      </SelectContent>
    </Select>
  )
}
