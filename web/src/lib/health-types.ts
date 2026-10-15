export type SystemHealth = {
  checked_at: string
  next_check_at: string
  status: 'healthy' | 'unavailable'
  database: {
    available: boolean
    measured_at?: string
    total_bytes: number
    relation_bytes: number
    other_database_bytes: number
    categories: StorageCategory[]
  }
  source_storage?: Array<{ provider_id: string; bytes: number }>
  retained_import_count?: number
  cancelled_import_count?: number
  import_storage_available?: boolean
  retained_imports?: Array<{
    run_id: string
    provider_id: string
    provider_name: string
    status: import('./types').Run['status']
    created_at: string
    bytes?: number
  }>

  disk: {
    available: boolean
    scope: string
    measured_at: string
    total_bytes: number
    free_bytes: number
    available_bytes: number
  }
}

export type StorageCategory = {
  key: string
  bytes: number
  table_bytes: number
  index_bytes: number
  details?: StorageCategory[]
}
