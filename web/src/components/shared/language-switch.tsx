import { useTranslation } from 'react-i18next'
import { Languages } from 'lucide-react'
import {
  changeAppLanguage,
  getCurrentLanguage,
  type SupportedLanguage,
} from '@/i18n/index'
import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

type LanguageSwitchProps = {
  onLanguageChange?: (lng: SupportedLanguage) => void
}

export function LanguageSwitch({ onLanguageChange }: LanguageSwitchProps) {
  const { t } = useTranslation('common')
  const current = getCurrentLanguage()

  const handleSelect = async (lng: SupportedLanguage) => {
    if (lng === current) return
    await changeAppLanguage(lng)
    onLanguageChange?.(lng)
  }

  return (
    <DropdownMenu modal={false}>
      <DropdownMenuTrigger asChild>
        <Button
          variant='ghost'
          size='icon'
          className='size-9 rounded-full'
          aria-label={t('language.label')}
        >
          <Languages className='size-4' />
          <span className='sr-only'>{t('language.label')}</span>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align='end'>
        <DropdownMenuItem
          onClick={() => void handleSelect('zh-CN')}
          className={current === 'zh-CN' ? 'bg-accent' : undefined}
        >
          {t('language.zhCN')}
        </DropdownMenuItem>
        <DropdownMenuItem
          onClick={() => void handleSelect('en-US')}
          className={current === 'en-US' ? 'bg-accent' : undefined}
        >
          {t('language.enUS')}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
