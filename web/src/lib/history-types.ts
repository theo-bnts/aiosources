import type { CatalogOrigin, Page } from './types'

export interface TorrentChange {
  id: string
  run_id?: string
  kind: 'added' | 'updated' | 'deleted'
  occurred_at: string
  before: Record<string, unknown> | null
  after: Record<string, unknown> | null
  origin?: CatalogOrigin
  before_origin?: CatalogOrigin
}

export interface OccurrenceHistory extends Page<TorrentChange> {
  occurrence_id: string
  provider_id: string
  source_id: string
  origin?: CatalogOrigin
  history_since: string
  baseline?: {
    fields: Record<string, unknown> | null
    origin?: CatalogOrigin
    recorded_at: string
    deleted: boolean
  }
}

export interface RunChange extends TorrentChange {
  occurrence_id: string
  provider_id: string
  source_id: string
}

export interface RunChanges extends Page<RunChange> {
  counts: { added: number; updated: number; deleted: number }
  history_since: string
  history_complete: boolean
}
