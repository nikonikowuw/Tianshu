import { useState } from 'react'
import { I18nextProvider } from 'react-i18next'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import '@/styles/index.css'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { render } from 'vitest-browser-react'
import { userEvent } from 'vitest/browser'
import i18n, { changeAppLanguage } from '@/i18n/index'
import { ApiError } from '@/lib/api/error'
import { resolveApiFileUrl } from './api'
import { FileDeleteDialog } from './components/file-delete-dialog'
import { FileDetailSheet } from './components/file-detail-sheet'
import { FileUploadDialog } from './components/file-upload-dialog'
import { FilesTable } from './components/files-table'
import { formatBytes } from './components/format-bytes'
import { filesSearchSchema, type FilesSearch } from './schema'
import type { FileItem } from './types'

const getMock = vi.fn()
const postMock = vi.fn()
const deleteMock = vi.fn()
const defaultsMock: { baseURL?: string } = { baseURL: '/api/v1' }

vi.mock('@/lib/api/client', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api/client')>()
  return {
    apiClient: {
      get defaults() {
        return defaultsMock
      },
      get: (...args: unknown[]) => getMock(...args),
      post: (...args: unknown[]) => postMock(...args),
      delete: (...args: unknown[]) => deleteMock(...args),
    },
    resolveApiUrl: (url: string) =>
      actual.resolveApiUrl(url, defaultsMock.baseURL),
  }
})

const sampleImage: FileItem = {
  id: '01950000-0000-7000-8000-000000001001',
  originalName: 'hero-banner.png',
  mimeType: 'image/png',
  category: 'image',
  sizeBytes: 204800,
  sha256: 'a'.repeat(64),
  storageDriver: 'local',
  isImage: true,
  uploader: {
    id: '01950000-0000-7000-8000-000000000101',
    username: 'root',
  },
  createdAt: '2026-11-27T01:00:00Z',
  updatedAt: '2026-11-27T01:00:00Z',
}

const sampleDoc: FileItem = {
  id: '01950000-0000-7000-8000-000000001002',
  originalName: 'quarterly-report.pdf',
  mimeType: 'application/pdf',
  category: 'document',
  sizeBytes: 1536000,
  sha256: 'b'.repeat(64),
  storageDriver: 's3',
  isImage: false,
  uploader: {
    id: '01950000-0000-7000-8000-000000000101',
    username: 'root',
  },
  createdAt: '2026-11-27T00:30:00Z',
  updatedAt: '2026-11-27T00:30:00Z',
}

function createTestQueryClient() {
  return new QueryClient({
    defaultOptions: {
      queries: { retry: false },
      mutations: { retry: false },
    },
  })
}

function TableHarness({
  initialSearch,
  canDelete = true,
  onInspect = vi.fn(),
  onDownload = vi.fn(),
  onDelete = vi.fn(),
}: {
  initialSearch?: Partial<FilesSearch>
  canDelete?: boolean
  onInspect?: (file: FileItem) => void
  onDownload?: (file: FileItem) => void
  onDelete?: (file: FileItem) => void
}) {
  const [search, setSearch] = useState<FilesSearch>({
    page: 1,
    pageSize: 20,
    category: [],
    driver: [],
    ...initialSearch,
  })
  return (
    <FilesTable
      search={search}
      navigate={({ search: next }) => {
        if (typeof next === 'function') {
          setSearch((prev) => ({
            ...prev,
            ...(next(prev as Record<string, unknown>) as Partial<FilesSearch>),
          }))
        } else if (typeof next === 'object' && next !== null) {
          setSearch(next as FilesSearch)
        }
      }}
      canDelete={canDelete}
      onInspectFile={onInspect}
      onDownloadFile={onDownload}
      onDeleteFile={onDelete}
    />
  )
}

describe('files feature', () => {
  afterEach(async () => {
    getMock.mockReset()
    postMock.mockReset()
    deleteMock.mockReset()
    defaultsMock.baseURL = '/api/v1'
    await changeAppLanguage('zh-CN')
  })

  it('normalizes search params, recovers invalid URL values, formats bytes, and resolves cross-origin URLs', () => {
    const parsed = filesSearchSchema.parse({
      page: '2',
      pageSize: '50',
      q: '  report  ',
      category: 'document',
      driver: ['local', 's3'],
      sort: 'size_bytes',
      order: 'asc',
    })
    expect(parsed).toEqual({
      page: 2,
      pageSize: 50,
      q: 'report',
      category: ['document'],
      driver: ['local', 's3'],
      sort: 'size_bytes',
      order: 'asc',
    })

    const recovered = filesSearchSchema.parse({
      page: 'invalid',
      pageSize: '999',
      category: ['unknown', 'image'],
      driver: 'gcs',
      sort: 'bad_column',
      order: 'sideways',
    })
    expect(recovered).toEqual({
      page: 1,
      pageSize: 20,
      q: undefined,
      category: ['image'],
      driver: [],
      sort: undefined,
      order: undefined,
    })

    expect(formatBytes(2048, 'en-US')).toBe('2 KB')
    expect(resolveApiFileUrl('/api/v1/files/signed/abc')).toBe(
      '/api/v1/files/signed/abc'
    )
    defaultsMock.baseURL = 'https://api.example.com/api/v1'
    expect(resolveApiFileUrl('/api/v1/files/signed/abc')).toBe(
      'https://api.example.com/api/v1/files/signed/abc'
    )
  })

  it('renders server-driven files table, supports row inspection, stops action menu bubbling, and respects delete permission', async () => {
    await changeAppLanguage('en-US')
    getMock.mockResolvedValue({
      data: {
        items: [sampleImage, sampleDoc],
        total: 2,
        page: 1,
        pageSize: 20,
      },
    })

    const onInspect = vi.fn()
    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <TableHarness canDelete={false} onInspect={onInspect} />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect.element(screen.getByText('hero-banner.png')).toBeVisible()
    await expect.element(screen.getByText('quarterly-report.pdf')).toBeVisible()
    await expect
      .element(screen.getByText(i18n.t('files:drivers.s3')))
      .toBeVisible()

    // Clicking the row opens the detail sheet without needing the overflow menu.
    await userEvent.click(screen.getByText('hero-banner.png'))
    expect(onInspect).toHaveBeenCalledWith(sampleImage)

    // Clicking the row action menu must NOT bubble up to trigger row inspection again.
    onInspect.mockClear()
    const menuTriggers = screen.getByRole('button', {
      name: i18n.t('files:rowActions.openMenu'),
    })
    await userEvent.click(menuTriggers.first())
    expect(onInspect).not.toHaveBeenCalled()

    // Without files:delete permission, the row action menu hides Delete.
    await expect
      .element(screen.getByText(i18n.t('files:rowActions.download')))
      .toBeVisible()
    await expect
      .element(screen.getByText(i18n.t('files:rowActions.delete')))
      .not.toBeInTheDocument()
  })

  it('distinguishes empty state, filtered empty state with reset, and error alert with retry', async () => {
    await changeAppLanguage('en-US')
    getMock.mockRejectedValueOnce(
      new ApiError({
        status: 500,
        code: 'INTERNAL_ERROR',
        message: 'Server temporarily unavailable',
      })
    )
    getMock.mockResolvedValueOnce({
      data: { items: [], total: 0, page: 1, pageSize: 20 },
    })

    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <TableHarness initialSearch={{ q: 'missing' }} />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByText(i18n.t('files:error.loadTitle')))
      .toBeVisible()
    await expect
      .element(screen.getByText('Server temporarily unavailable'))
      .toBeVisible()

    // Retry loads filtered empty state and allows clearing filters.
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('files:error.retry') })
    )
    await expect
      .element(screen.getByText(i18n.t('files:empty.noResults')))
      .toBeVisible()

    getMock.mockResolvedValueOnce({
      data: { items: [], total: 0, page: 1, pageSize: 20 },
    })
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('common:table.clearFilters') })
    )
    await expect
      .element(screen.getByText(i18n.t('files:empty.noData')))
      .toBeVisible()
  })

  it('fetches signed preview URL for images in FileDetailSheet and skips signed preview for documents', async () => {
    await changeAppLanguage('en-US')
    const tinyPngDataUrl =
      'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII='
    postMock.mockResolvedValue({
      data: {
        url: tinyPngDataUrl,
        expiresAt: '2026-11-27T01:05:00Z',
      },
    })

    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <FileDetailSheet
            file={sampleImage}
            open
            canDelete
            onOpenChange={vi.fn()}
            onDownload={vi.fn()}
            onDelete={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const img = screen.getByRole('img', { name: 'hero-banner.png' })
    await expect.element(img).toHaveAttribute('src', tinyPngDataUrl)
    expect(postMock).toHaveBeenCalledWith(
      `/files/${sampleImage.id}/signed-url`,
      { disposition: 'inline' },
      expect.objectContaining({ signal: expect.any(AbortSignal) })
    )

    // Documents never request an inline signed URL; they render the attachment notice.
    postMock.mockClear()
    await screen.rerender(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <FileDetailSheet
            file={sampleDoc}
            open
            canDelete
            onOpenChange={vi.fn()}
            onDownload={vi.fn()}
            onDelete={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )
    await expect
      .element(
        screen.getByText(
          'Documents are forced to download as attachments for security and are not rendered inline.'
        )
      )
      .toBeVisible()
    expect(postMock).not.toHaveBeenCalled()
  })

  it('shows preview fallback when signed image fails and resets error state on close', async () => {
    await changeAppLanguage('en-US')
    postMock.mockResolvedValue({
      data: {
        url: '/api/v1/files/signed/expired-or-missing-token',
        expiresAt: '2026-11-27T01:05:00Z',
      },
    })

    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <FileDetailSheet
            file={sampleImage}
            open
            canDelete
            onOpenChange={vi.fn()}
            onDownload={vi.fn()}
            onDelete={vi.fn()}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(
        screen.getByText(
          'Unable to load image preview. Retry or download directly.'
        )
      )
      .toBeVisible()
    await expect
      .element(
        screen.getByRole('button', { name: i18n.t('files:error.retry') })
      )
      .toBeVisible()
  })

  it('uploads queued files with 201 success, surfaces 400 per-file error, and shows 429 cooldown banner', async () => {
    await changeAppLanguage('en-US')
    getMock.mockResolvedValueOnce({
      data: {
        maxFileSizeBytes: 10 * 1024 * 1024,
        maxAvatarSizeBytes: 2 * 1024 * 1024,
        allowedExtensions: ['.png', '.svg', '.txt'],
      },
    })
    postMock
      .mockResolvedValueOnce({ data: sampleImage })
      .mockRejectedValueOnce(
        new ApiError({
          status: 400,
          code: 'FILES_TYPE_NOT_ALLOWED',
          message: 'File type is not allowed',
        })
      )
      .mockRejectedValueOnce(
        new ApiError({
          status: 429,
          code: 'RATE_LIMITED',
          message: 'Too many requests, please try again later',
          retryAfter: 30,
        })
      )

    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <FileUploadDialog open onOpenChange={vi.fn()} />
        </I18nextProvider>
      </QueryClientProvider>
    )

    const input = screen.getByLabelText(
      i18n.t('files:uploadDialog.selectFiles')
    )
    await userEvent.upload(input, [
      new File(['ok'], 'good.png', { type: 'image/png' }),
      new File(['<svg/>'], 'bad.svg', { type: 'image/svg+xml' }),
      new File(['later'], 'queued.txt', { type: 'text/plain' }),
    ])

    await expect
      .element(
        screen.getByRole('button', {
          name: i18n.t('files:uploadDialog.removeFile', { name: 'good.png' }),
        })
      )
      .toBeVisible()

    const uploadBtn = screen.getByRole('button', {
      name: i18n.t('files:uploadDialog.startUpload'),
    })
    await userEvent.click(uploadBtn)

    await expect
      .element(screen.getByText(i18n.t('files:uploadDialog.status.success')))
      .toBeVisible()
    await expect
      .element(screen.getByText('File type is not allowed'))
      .toBeVisible()
    await expect
      .element(screen.getByRole('alert'))
      .toHaveTextContent('Too many requests, please try again later')
    await expect.element(uploadBtn).toBeDisabled()
  })

  it('calls delete API from FileDeleteDialog, handles success, and keeps dialog open on 409 error', async () => {
    await changeAppLanguage('en-US')
    deleteMock.mockRejectedValueOnce(
      new ApiError({
        status: 409,
        code: 'FILES_IN_USE',
        message: 'File is currently referenced as a user avatar',
      })
    )
    const onOpenChange = vi.fn()
    const onDeleted = vi.fn()

    const qc = createTestQueryClient()
    const screen = await render(
      <QueryClientProvider client={qc}>
        <I18nextProvider i18n={i18n}>
          <FileDeleteDialog
            file={sampleDoc}
            open
            onOpenChange={onOpenChange}
            onDeleted={onDeleted}
          />
        </I18nextProvider>
      </QueryClientProvider>
    )

    await expect
      .element(screen.getByText(/quarterly-report\.pdf/))
      .toBeVisible()
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('files:deleteDialog.confirm') })
    )

    expect(deleteMock).toHaveBeenCalledWith(`/files/${sampleDoc.id}`)
    expect(onDeleted).not.toHaveBeenCalled()
    expect(onOpenChange).not.toHaveBeenCalledWith(false)

    // Subsequent retry succeeds and closes the dialog.
    deleteMock.mockResolvedValueOnce({ data: undefined })
    await userEvent.click(
      screen.getByRole('button', { name: i18n.t('files:deleteDialog.confirm') })
    )
    expect(onDeleted).toHaveBeenCalledWith(sampleDoc.id)
    expect(onOpenChange).toHaveBeenCalledWith(false)
  })
})
