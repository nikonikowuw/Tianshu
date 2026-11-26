import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'

export function Dashboard() {
  const { t } = useTranslation('common')
  const queryClient = useQueryClient()

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

      <Main>
        <div className='mb-6 flex flex-wrap items-center justify-between gap-2'>
          <div>
            <h1 className='text-2xl font-bold tracking-tight'>
              {t('dashboard.title')}
            </h1>
            <p className='text-muted-foreground'>
              {t('dashboard.description')}
            </p>
          </div>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>{t('dashboard.readyCardTitle')}</CardTitle>
            <CardDescription>{t('dashboard.readyCardDesc')}</CardDescription>
          </CardHeader>
          <CardContent />
        </Card>
      </Main>
    </>
  )
}
