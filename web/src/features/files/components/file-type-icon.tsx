import { FileImage, FileSpreadsheet, FileText } from 'lucide-react'
import type { FileItem } from '../types'

export function FileTypeIcon({ file }: { file: FileItem }) {
  if (file.isImage) {
    return (
      <div className='flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted/50 text-primary'>
        <FileImage className='size-4' />
      </div>
    )
  }
  if (file.mimeType === 'text/csv' || file.mimeType.includes('spreadsheetml')) {
    return (
      <div className='flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted/50 text-muted-foreground'>
        <FileSpreadsheet className='size-4' />
      </div>
    )
  }
  return (
    <div className='flex size-8 shrink-0 items-center justify-center rounded-md border bg-muted/50 text-muted-foreground'>
      <FileText className='size-4' />
    </div>
  )
}
