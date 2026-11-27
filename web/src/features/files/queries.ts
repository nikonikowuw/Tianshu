import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import {
  createSignedFileUrl,
  deleteFile,
  fetchFiles,
  fetchUploadPolicy,
  uploadFile,
} from './api'
import type { FileDisposition, FilesListParams } from './types'

export const filesKeys = {
  all: ['files'] as const,
  lists: ['files', 'list'] as const,
  list: (params: FilesListParams) => ['files', 'list', params] as const,
  signedUrl: (id: string, disposition: FileDisposition) =>
    ['files', 'signed-url', id, disposition] as const,
  uploadPolicy: ['files', 'upload-policy'] as const,
}

export function filesQueryOptions(params: FilesListParams) {
  return queryOptions({
    queryKey: filesKeys.list(params),
    queryFn: ({ signal }) => fetchFiles(params, signal),
    placeholderData: keepPreviousData,
    meta: { silent: true },
  })
}

export function fileUploadPolicyQueryOptions() {
  return queryOptions({
    queryKey: filesKeys.uploadPolicy,
    queryFn: ({ signal }) => fetchUploadPolicy(signal),
    staleTime: 5 * 60_000,
    meta: { silent: true },
  })
}

export function fileSignedUrlQueryOptions(
  id: string,
  disposition: FileDisposition,
  enabled: boolean
) {
  return queryOptions({
    queryKey: filesKeys.signedUrl(id, disposition),
    queryFn: ({ signal }) => createSignedFileUrl(id, disposition, signal),
    enabled,
    staleTime: 60_000,
    gcTime: 120_000,
    retry: false,
    meta: { silent: true },
  })
}

export function useUploadFileMutation() {
  return useMutation({
    mutationFn: ({
      file,
      onProgress,
      signal,
    }: {
      file: File
      onProgress?: (percent: number) => void
      signal?: AbortSignal
    }) => uploadFile(file, { onProgress, signal }),
  })
}

export function useDeleteFileMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (fileId: string) => deleteFile(fileId),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: filesKeys.lists })
    },
  })
}
