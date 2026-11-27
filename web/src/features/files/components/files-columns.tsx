import { type TFunction } from 'i18next'
import { type ColumnDef } from '@tanstack/react-table'
import { DataTableColumnHeader } from '@/components/data-table'
import { Badge } from '@/components/ui/badge'
import { LongText } from '@/components/shared/long-text'
import type { FileItem } from '../types'
import { FileRowActions } from './file-row-actions'
import { FileTypeIcon } from './file-type-icon'
import {
  formatBytes,
  formatFileTimestamp,
  formatMimeBadge,
} from './format-bytes'

export function createFileColumns(params: {
  t: TFunction<'files'>
  locale: string
  canDelete: boolean
  onInspect: (file: FileItem) => void
  onDownload: (file: FileItem) => void
  onDelete: (file: FileItem) => void
}): ColumnDef<FileItem>[] {
  const { t, locale, canDelete, onInspect, onDownload, onDelete } = params

  return [
    {
      id: 'original_name',
      accessorKey: 'originalName',
      enableHiding: false,
      meta: { label: t('columns.name') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.name')} />
      ),
      cell: ({ row }) => {
        const file = row.original
        return (
          <div className='flex items-center gap-2.5'>
            <FileTypeIcon file={file} />
            <div className='min-w-0'>
              <LongText className='max-w-56 font-medium'>
                {file.originalName}
              </LongText>
              <div className='truncate font-mono text-xs text-muted-foreground'>
                {file.mimeType}
              </div>
            </div>
          </div>
        )
      },
    },
    {
      id: 'category',
      accessorKey: 'category',
      enableSorting: false,
      meta: { label: t('columns.type') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.type')} />
      ),
      cell: ({ row }) => {
        const file = row.original
        return (
          <div className='flex items-center gap-1.5'>
            <Badge
              variant={file.isImage ? 'secondary' : 'outline'}
              className='font-mono text-xs'
            >
              {formatMimeBadge(file.mimeType)}
            </Badge>
            <span className='text-xs text-muted-foreground'>
              {file.isImage ? t('categories.image') : t('categories.document')}
            </span>
          </div>
        )
      },
    },
    {
      id: 'size_bytes',
      accessorKey: 'sizeBytes',
      meta: { label: t('columns.size') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.size')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
          {formatBytes(row.original.sizeBytes, locale)}
        </span>
      ),
    },
    {
      id: 'driver',
      accessorKey: 'storageDriver',
      enableSorting: false,
      meta: { label: t('columns.driver') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.driver')} />
      ),
      cell: ({ row }) => {
        const drv = row.original.storageDriver
        return (
          <Badge variant='outline' className='font-mono text-xs uppercase'>
            {drv === 's3' ? t('drivers.s3') : t('drivers.local')}
          </Badge>
        )
      },
    },
    {
      id: 'uploader',
      enableSorting: false,
      meta: { label: t('columns.uploader') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.uploader')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs text-muted-foreground'>
          {row.original.uploader?.username || t('empty.systemUploader')}
        </span>
      ),
    },
    {
      id: 'created_at',
      accessorKey: 'createdAt',
      meta: { label: t('columns.createdAt') },
      header: ({ column }) => (
        <DataTableColumnHeader column={column} title={t('columns.createdAt')} />
      ),
      cell: ({ row }) => (
        <span className='font-mono text-xs whitespace-nowrap text-muted-foreground'>
          {formatFileTimestamp(row.original.createdAt, locale)}
        </span>
      ),
    },
    {
      id: 'actions',
      enableSorting: false,
      enableHiding: false,
      meta: {
        thClassName: 'sticky end-0 z-10 w-12 bg-background px-2.5 last:pe-2.5',
        tdClassName:
          'sticky end-0 z-10 w-12 bg-background px-2.5 last:pe-2.5 text-center transition-colors group-hover/row:bg-[color-mix(in_srgb,var(--muted)_50%,var(--background))] group-focus-visible/row:bg-[color-mix(in_srgb,var(--muted)_60%,var(--background))] group-data-[state=selected]/row:bg-muted',
      },
      header: () => <span className='sr-only'>{t('rowActions.openMenu')}</span>,
      cell: ({ row }) => (
        <FileRowActions
          file={row.original}
          canDelete={canDelete}
          onInspect={onInspect}
          onDownload={onDownload}
          onDelete={onDelete}
        />
      ),
    },
  ]
}
