import { apiClient, resolveApiUrl } from '@/lib/api/client'
import type { Paginated } from '@/lib/api/types'
import type {
  FileDisposition,
  FileItem,
  FileUploadPolicy,
  FilesListParams,
  SignedFileUrlResponse,
} from './types'

const UPLOAD_TIMEOUT_MS = 60_000

export const resolveApiFileUrl = resolveApiUrl

function buildFilesSearchParams(params: FilesListParams): URLSearchParams {
  const sp = new URLSearchParams()
  sp.set('page', String(params.page))
  sp.set('pageSize', String(params.pageSize))
  if (params.q) sp.set('q', params.q)
  if (params.sort) sp.set('sort', params.sort)
  if (params.order) sp.set('order', params.order)
  for (const cat of params.category ?? []) {
    sp.append('category', cat)
  }
  for (const drv of params.driver ?? []) {
    sp.append('driver', drv)
  }
  return sp
}

export async function fetchFiles(
  params: FilesListParams,
  signal?: AbortSignal
): Promise<Paginated<FileItem>> {
  const { data } = await apiClient.get<Paginated<FileItem>>('/files', {
    params: buildFilesSearchParams(params),
    signal,
  })
  return data
}

export async function fetchUploadPolicy(
  signal?: AbortSignal
): Promise<FileUploadPolicy> {
  const { data } = await apiClient.get<FileUploadPolicy>('/files/config', {
    signal,
  })
  return data
}

export async function uploadFile(
  file: File,
  options?: {
    onProgress?: (percent: number) => void
    signal?: AbortSignal
  }
): Promise<FileItem> {
  const form = new FormData()
  form.append('file', file)

  const { data } = await apiClient.post<FileItem>('/files', form, {
    timeout: UPLOAD_TIMEOUT_MS,
    signal: options?.signal,
    onUploadProgress: (event) => {
      if (!options?.onProgress || !event.total || event.total <= 0) return
      const percent = Math.min(
        100,
        Math.round((event.loaded * 100) / event.total)
      )
      options.onProgress(percent)
    },
  })
  return data
}

export async function createSignedFileUrl(
  fileId: string,
  disposition: FileDisposition,
  signal?: AbortSignal
): Promise<SignedFileUrlResponse> {
  const { data } = await apiClient.post<SignedFileUrlResponse>(
    `/files/${fileId}/signed-url`,
    { disposition },
    { signal }
  )
  return {
    ...data,
    url: resolveApiFileUrl(data.url),
  }
}

export async function deleteFile(fileId: string): Promise<void> {
  await apiClient.delete(`/files/${fileId}`)
}
