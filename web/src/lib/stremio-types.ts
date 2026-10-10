export type StremioResult = {
  name: string
  provider: string
  provider_name?: string
  source_id: string
  title: string
  service: 'direct' | 'alldebrid' | 'torbox'
  cache: 'direct' | 'cached' | 'download' | 'unknown'
  filename: string
  size: number
}
export type StremioRequest = {
  id: number
  created_at: string
  media_type: string
  resource: string
  duration_ms: number
  outcome: 'results' | 'empty' | 'invalid' | 'failed' | 'cancelled' | 'busy'
  matched: number
  eligible: number
  invalid_hash: number
  wrong_episode: number
  private_unavailable: number
  file_unavailable: number
  returned: number
  truncated: boolean
  results?: StremioResult[]
}
export type StremioStatistics = {
  requests: number
  empty: number
  failed: number
  results: number
  average_ms: number
  since: string | null
}
