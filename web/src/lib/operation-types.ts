export type OperationStatus = 'running' | 'succeeded' | 'failed' | 'cancelled' | 'interrupted'
export type OperationKind = 'metadata' | 'matching' | 'cache' | 'cleanup'
export type OperationRun = {
  id: string
  kind: OperationKind
  service: 'tmdb' | 'local' | 'alldebrid' | 'torbox'
  trigger: 'manual' | 'automatic' | 'playback'
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
