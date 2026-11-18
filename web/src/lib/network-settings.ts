import { useMutation, useQuery } from '@tanstack/react-query'
import {
  api,
  AUTO_REFRESH_INTERVAL,
  commitRevisionQueryData,
  queryClient,
  refreshData,
} from '@/lib/api'
import type { NetworkSettings } from '@/lib/network-types'

export function useNetworkSettings() {
  return useQuery({
    queryKey: ['network-settings'],
    queryFn: ({ signal }) => api<NetworkSettings>('/settings/network', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
}

export function useSaveNetwork(onSaved?: () => void) {
  return useMutation({
    mutationFn: (settings: NetworkSettings) =>
      api<NetworkSettings>('/settings/network', {
        method: 'PUT',
        body: JSON.stringify(settings),
      }),
    onMutate: () => queryClient.cancelQueries({ queryKey: ['network-settings'] }),
    onSuccess: async (settings) => {
      await commitRevisionQueryData(['network-settings'], settings)
      onSaved?.()
      void queryClient.invalidateQueries({ queryKey: ['network-incoming-status'] })
      void refreshData()
    },
    onError: () => void queryClient.invalidateQueries({ queryKey: ['network-settings'] }),
  })
}
