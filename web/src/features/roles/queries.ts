import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import {
  createRole,
  deleteRole,
  fetchPermissionsCatalog,
  fetchRoles,
  updateRole,
} from './api'
import type {
  CreateRolePayload,
  RolesListParams,
  UpdateRolePayload,
} from './types'

export const rolesKeys = {
  all: ['roles'] as const,
  list: (params: RolesListParams, lang: string) =>
    ['roles', 'list', lang, params] as const,
  permissions: (lang: string) => ['permissions', 'catalog', lang] as const,
}

export function permissionsCatalogQueryOptions(lang: string) {
  return queryOptions({
    queryKey: rolesKeys.permissions(lang),
    queryFn: ({ signal }) => fetchPermissionsCatalog(signal),
    staleTime: 5 * 60 * 1000,
    meta: { silent: true },
  })
}

export function rolesQueryOptions(params: RolesListParams, lang: string) {
  return queryOptions({
    queryKey: rolesKeys.list(params, lang),
    queryFn: ({ signal }) => fetchRoles(params, signal),
    placeholderData: keepPreviousData,
    meta: { silent: true },
  })
}

export function useCreateRoleMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateRolePayload) => createRole(payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: rolesKeys.all })
    },
  })
}

export function useUpdateRoleMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateRolePayload }) =>
      updateRole(id, payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: rolesKeys.all })
    },
  })
}

export function useDeleteRoleMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => deleteRole(id),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: rolesKeys.all })
    },
  })
}
