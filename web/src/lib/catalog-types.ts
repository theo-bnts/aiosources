import type { Run, CatalogSync, ScheduleSummary } from './types'
import type { InstallationOptions, LanguagePreferences } from './language-preferences'
import type { TorznabOptions } from './torznab-preferences'

export type TorznabPreferences = Omit<TorznabOptions, 'sources' | 'media'>

export type ConnectionFormat = 'catalog' | 'torznab' | 'stremio'
export type StremioPreferences = InstallationOptions & LanguagePreferences

export interface CatalogEntities {
  metadata?: boolean
  availability?: boolean
}

export interface CatalogShare {
  catalog_entities?: CatalogEntities
  torznab_preferences?: TorznabPreferences
  format?: ConnectionFormat
  media_types?: string[]
  stream_preferences?: StremioPreferences
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
  received_entities?: CatalogEntities
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
