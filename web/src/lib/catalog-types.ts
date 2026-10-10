import type { Run, CatalogSync, ScheduleSummary } from './types'

export interface CatalogShare {
  id: string
  name: string
  enabled: boolean
  scope: 'selected' | 'all'
  source_ids: string[]
  fields: string[]
  revision: number
  created_at: string
  updated_at: string
  last_access_at?: string
  last_sync_at?: string
  expires_at: string | null
  requests_per_minute: number
  max_concurrent_downloads: number
  url: string
}

export interface CatalogShares {
  items: CatalogShare[] | null
  fields: string[]
  sources: { id: string; name: string }[]
  public_url: string
}

export interface SharePassword {
  share: CatalogShare
  password: string
}

export interface RemoteCatalog {
  id: string
  name: string
  url: string
  enabled: boolean
  valid: boolean
  revision: string
  secret_ref: string
  sync: CatalogSync
  sync_state: ScheduleSummary['state']
  next_sync_at?: string
  sync_error?: string
  last_run?: Run
  has_checkpoint: boolean
  last_synced_at?: string
}
