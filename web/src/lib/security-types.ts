export interface SecurityStatus {
  mfa_enabled: boolean
  recovery_codes_remaining: number
  pending_expires_at?: string
}

export interface AdminSession {
  id: string
  user_agent: string
  created_at: string
  last_seen_at: string
  expires_at: string
  current: boolean
}

export interface MFAEnrollment {
  secret: string
  otpauth_url: string
  expires_at: string
}

export interface RecoveryCodes {
  recovery_codes: string[]
}
