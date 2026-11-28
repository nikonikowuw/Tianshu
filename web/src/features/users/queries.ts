import {
  keepPreviousData,
  queryOptions,
  useMutation,
  useQueryClient,
} from '@tanstack/react-query'
import { syncCurrentProfileIfUserId } from '@/lib/auth/gateway'
import {
  createUser,
  deleteUser,
  fetchRoleOptions,
  fetchUsers,
  updateUser,
} from './api'
import type {
  CreateUserPayload,
  UpdateUserPayload,
  UsersListParams,
} from './types'

export const usersKeys = {
  all: ['users'] as const,
  list: (params: UsersListParams, lang: string) =>
    ['users', 'list', lang, params] as const,
  roleOptions: (lang: string) => ['roles', 'options', lang] as const,
}

export function usersQueryOptions(params: UsersListParams, lang: string) {
  return queryOptions({
    queryKey: usersKeys.list(params, lang),
    queryFn: ({ signal }) => fetchUsers(params, signal),
    placeholderData: keepPreviousData,
    meta: { silent: true },
  })
}

export function roleOptionsQueryOptions(lang: string) {
  return queryOptions({
    queryKey: usersKeys.roleOptions(lang),
    queryFn: ({ signal }) => fetchRoleOptions(signal),
    staleTime: 5 * 60 * 1000,
    meta: { silent: true },
  })
}

export function useCreateUserMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (payload: CreateUserPayload) => createUser(payload),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersKeys.all })
      void queryClient.invalidateQueries({ queryKey: ['roles'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
  })
}

export function useUpdateUserMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: ({ id, payload }: { id: string; payload: UpdateUserPayload }) =>
      updateUser(id, payload),
    meta: { silent: true },
    onSuccess: (updated) => {
      void queryClient.invalidateQueries({ queryKey: usersKeys.all })
      void queryClient.invalidateQueries({ queryKey: ['roles'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
      void syncCurrentProfileIfUserId(updated.id)
        .then((synced) => {
          if (synced) {
            void queryClient.invalidateQueries({ queryKey: ['profile', 'me'] })
            void queryClient.invalidateQueries({ queryKey: ['menus', 'user'] })
          }
        })
        .catch(() => {
          // Non-fatal: subsequent protected requests or 403 refresh will still converge.
        })
    },
  })
}

export function useDeleteUserMutation() {
  const queryClient = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => deleteUser(id),
    meta: { silent: true },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: usersKeys.all })
      void queryClient.invalidateQueries({ queryKey: ['roles'] })
      void queryClient.invalidateQueries({ queryKey: ['dashboard'] })
    },
  })
}
