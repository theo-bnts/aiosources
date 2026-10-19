export type ActivityLevel = 'debug' | 'info' | 'warn' | 'error'
export const activityLevels: ActivityLevel[] = ['debug', 'info', 'warn', 'error']

export const activityServices = [
  { id: 'network', name: 'Network', to: '/network' },
  { id: 'tmdb', name: 'TMDB', to: '/metadata' },
  { id: 'alldebrid', name: 'AllDebrid', to: '/debrid' },
  { id: 'torbox', name: 'TorBox', to: '/debrid' },
] as const

export interface LogEntry {
  id: number
  level: ActivityLevel
  kind: string
  title?: string
  message: string
  provider_id?: string
  service?: string
  run_id?: string
  created_at: string
  data?: Record<string, unknown>
}
