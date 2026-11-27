import { useCallback, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import { Upload } from 'lucide-react'
import { toast } from 'sonner'
import { normalizeApiError } from '@/lib/api/error'
import { usePermission } from '@/lib/auth/use-permission'
import { type NavigateFn } from '@/hooks/use-table-url-state'
import { Button } from '@/components/ui/button'
import { Header } from '@/components/layout/header'
import { Main } from '@/components/layout/main'
import { LanguageSwitch } from '@/components/shared/language-switch'
import { PermissionGuard } from '@/components/shared/permission-guard'
import { Search } from '@/components/shared/search'
import { ThemeSwitch } from '@/components/shared/theme-switch'
import { createSignedFileUrl } from './api'
import { FileDeleteDialog } from './components/file-delete-dialog'
import { FileDetailSheet } from './components/file-detail-sheet'
import { FileUploadDialog } from './components/file-upload-dialog'
import { FilesTable } from './components/files-table'
import type { FilesSearch } from './schema'
import type { FileItem } from './types'

const route = getRouteApi('/_authenticated/files')

export function Files() {
  const { t } = useTranslation('files')
  const queryClient = useQueryClient()
  const search = route.useSearch()
  const routeNavigate = route.useNavigate()

  const canUpload = usePermission('files:upload')
  const canDelete = usePermission('files:delete')

  const [uploadOpen, setUploadOpen] = useState(false)
  const [selectedFile, setSelectedFile] = useState<FileItem | null>(null)
  const [deletingFile, setDeletingFile] = useState<FileItem | null>(null)

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
                    ) => Partial<FilesSearch>
                  )(prev as Record<string, unknown>) as FilesSearch
              : (opts.search as FilesSearch),
      })
    },
    [routeNavigate]
  )

  const handleDownloadFile = useCallback(
    async (file: FileItem) => {
      try {
        const { url } = await createSignedFileUrl(file.id, 'attachment')
        const anchor = document.createElement('a')
        anchor.href = url
        anchor.download = file.originalName
        anchor.rel = 'noopener noreferrer'
        document.body.appendChild(anchor)
        anchor.click()
        anchor.remove()
        toast.success(t('toast.downloadStarted'))
      } catch (err) {
        toast.error(normalizeApiError(err).message)
      }
    },
    [t]
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
            <h1
              data-testid='files-page-title'
              className='text-2xl font-bold tracking-tight'
            >
              {t('title')}
            </h1>
            <p className='text-muted-foreground'>{t('description')}</p>
          </div>
          <PermissionGuard allowed={canUpload}>
            <Button
              type='button'
              data-testid='files-upload-open'
              onClick={() => setUploadOpen(true)}
            >
              <Upload className='size-4' />
              {t('uploadFile')}
            </Button>
          </PermissionGuard>
        </div>

        <FilesTable
          search={search}
          navigate={navigate}
          canDelete={canDelete}
          onInspectFile={setSelectedFile}
          onDownloadFile={(file) => void handleDownloadFile(file)}
          onDeleteFile={setDeletingFile}
        />
      </Main>

      <FileUploadDialog open={uploadOpen} onOpenChange={setUploadOpen} />

      <FileDetailSheet
        file={selectedFile}
        open={selectedFile !== null}
        canDelete={canDelete}
        onOpenChange={(open) => {
          if (!open) setSelectedFile(null)
        }}
        onDownload={(file) => void handleDownloadFile(file)}
        onDelete={setDeletingFile}
      />

      <FileDeleteDialog
        file={deletingFile}
        open={deletingFile !== null}
        onOpenChange={(open) => {
          if (!open) setDeletingFile(null)
        }}
        onDeleted={(fileId) => {
          if (selectedFile?.id === fileId) {
            setSelectedFile(null)
          }
        }}
      />
    </>
  )
}
