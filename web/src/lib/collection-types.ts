import type { Provider } from './types'

export function supportsMetadata(provider: Provider | undefined): boolean {
  return Boolean(
    provider?.adapter === 'http_json' &&
    !provider.http.catalog &&
    provider.traversal?.id_recovery &&
    provider.traversal.enrich_fields?.length,
  )
}

export interface CollectionSettings {
  workers: number
  max_quota_retries: number
  max_quota_wait_seconds: number
  no_progress_requests: number
  no_progress_action: 'warn' | 'pause'
  revision: number
}

export interface CollectionOverview {
  settings: CollectionSettings
  running: number
  queued: number
  capacity: number
}
