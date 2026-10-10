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
    categories: Array<{
      key: 'sources' | 'metadata' | 'debrid' | 'stremio' | 'sharing' | 'access' | 'logs' | 'other'
      bytes: number
      table_bytes: number
      index_bytes: number
    }>
  }
  disk: {
    available: boolean
    scope: string
    measured_at: string
    total_bytes: number
    free_bytes: number
    available_bytes: number
  }
}
