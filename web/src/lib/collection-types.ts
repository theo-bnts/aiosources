import type { Provider } from './types'
import { isJSONAdapter } from './source-adapters'

export function supportsMetadata(provider: Provider | undefined): boolean {
  return Boolean(
    provider &&
    isJSONAdapter(provider.adapter) &&
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
  source_running: number
  source_queued: number
  settings: CollectionSettings
  running: number
  queued: number
  capacity: number
}
