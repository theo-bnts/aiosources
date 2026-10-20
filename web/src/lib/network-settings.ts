import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { api, refreshData, AUTO_REFRESH_INTERVAL } from '@/lib/api'
import type { NetworkSettings } from '@/lib/network-types'

export function useNetworkSettings() {
  return useQuery({
    queryKey: ['network-settings'],
    queryFn: ({ signal }) => api<NetworkSettings>('/settings/network', { signal }),
    refetchInterval: AUTO_REFRESH_INTERVAL,
  })
}

export function useSaveNetwork(onSaved?: () => void) {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (settings: NetworkSettings) =>
      api<NetworkSettings>('/settings/network', {
        method: 'PUT',
        body: JSON.stringify(settings),
      }),
    onSuccess: async (settings) => {
      await client.cancelQueries({ queryKey: ['network-settings'] })
      client.setQueryData<NetworkSettings>(['network-settings'], (current) =>
        current && current.revision > settings.revision ? current : settings,
      )
      onSaved?.()
      void client.invalidateQueries({ queryKey: ['network-incoming-status'] })
      void refreshData()
    },
    onError: () => void client.invalidateQueries({ queryKey: ['network-settings'] }),
  })
}
