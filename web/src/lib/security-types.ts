export interface AdminSession {
  id: string
  user_agent: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}
