import { useMutation, useQueryClient } from '@tanstack/react-query'
import { apiClient } from '@/lib/api/client'
import { setupQueryKeys, type SetupStatus } from '@/lib/api/setup'

export type InitSetupPayload = {
  username: string
  email: string
  displayName: string
  password: string
}

export type InitSetupResponse = {
  user: {
    id: string
    username: string
    email: string
    displayName: string
  }
}

export function useInitSetupMutation() {
  const queryClient = useQueryClient()

  return useMutation({
    mutationFn: async (payload: InitSetupPayload) => {
      const res = await apiClient.post<InitSetupResponse>(
        '/setup/init',
        payload
      )
      return res.data
    },
    onSuccess: () => {
      queryClient.setQueryData<SetupStatus>(setupQueryKeys.status, {
        initialized: true,
      })
      void queryClient.invalidateQueries({ queryKey: setupQueryKeys.status })
    },
  })
}
