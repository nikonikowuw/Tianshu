export type FileCategory = 'image' | 'document'

export type StorageDriver = 'local' | 's3'

export type FileDisposition = 'inline' | 'attachment'

export type FileUploaderRef = {
  id: string
  username: string
}

export type FileItem = {
  id: string
  originalName: string
  mimeType: string
  category: FileCategory
  sizeBytes: number
  sha256: string
  storageDriver: StorageDriver
  isImage: boolean
  uploader: FileUploaderRef | null
  createdAt: string
  updatedAt: string
}

export type FilesListParams = {
  page: number
  pageSize: number
  q?: string
  category?: FileCategory[]
  driver?: StorageDriver[]
  sort?: 'created_at' | 'original_name' | 'size_bytes'
  order?: 'asc' | 'desc'
}

export type SignedFileUrlResponse = {
  url: string
  expiresAt: string
}

export type FileUploadPolicy = {
  maxFileSizeBytes: number
  maxAvatarSizeBytes: number
  allowedExtensions: string[]
}
