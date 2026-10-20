import type { CatalogSelection } from '@/lib/catalog-search-types'
export type OperationStatus = 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'
export type OperationKind = 'metadata' | 'matching' | 'cache' | 'cleanup' | 'download'
export type OperationRun = {
  download?: {
    work_id: number
    input: CatalogSelection & {
      occurrence_id: string
      fingerprint: string
      service: 'alldebrid' | 'torbox'
    }
  }
  id: string
  kind: OperationKind
  service: 'tmdb' | 'local' | 'alldebrid' | 'torbox'
  trigger: 'manual' | 'automatic' | 'playback' | 'public'
  resource?: string
  status: OperationStatus
  phase: string
  processed: number
  errors: number
  results: Record<string, number>
  created_at: string
  updated_at: string
  finished_at: string | null
  error?: string
}
export type OperationEvent = {
  duration_ms?: number
  http_status?: number
  error_code?: string
  id: number
  run_id: string
  phase: string
  processed: number
  errors: number
  created_at: string
}
