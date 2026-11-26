import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { type NavigateFn } from '@/hooks/use-table-url-state'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { AuditLogDetailSheet } from './components/audit-log-detail-sheet'
import { AuditLogsTable } from './components/audit-logs-table'
import { type AuditLogsSearch } from './schema'
import type { AuditLogItem } from './types'

const route = getRouteApi('/_authenticated/audit-logs')

export function AuditLogs() {
  const { t } = useTranslation('audit')
  const queryClient = useQueryClient()
  const search = route.useSearch()
  const routeNavigate = route.useNavigate()
  const [selectedEntry, setSelectedEntry] = useState<AuditLogItem | null>(null)

  const navigate: NavigateFn = useCallback(
    (opts) => {
      void routeNavigate({
        replace: opts.replace,
        search:
          opts.search === true
            ? true
            : typeof opts.search === 'function'
              ? (prev) =>
                  (
                    opts.search as (
                      p: Record<string, unknown>
                    ) => Partial<AuditLogsSearch>
                  )(prev as Record<string, unknown>) as AuditLogsSearch
              : (opts.search as AuditLogsSearch),
      })
    },
    [routeNavigate]
  )

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

      <Main fluid className='flex flex-1 flex-col gap-4 sm:gap-6'>
        <div className='flex flex-wrap items-end justify-between gap-2'>
          <div>
            <h1 className='text-2xl font-bold tracking-tight'>{t('title')}</h1>
            <p className='text-muted-foreground'>{t('description')}</p>
          </div>
        </div>

        <AuditLogsTable
          search={search}
          navigate={navigate}
          onSelectEntry={setSelectedEntry}
        />
      </Main>

      <AuditLogDetailSheet
        entry={selectedEntry}
        open={selectedEntry !== null}
        onOpenChange={(open) => {
          if (!open) {
            setSelectedEntry(null)
          }
        }}
      />
    </>
  )
}
