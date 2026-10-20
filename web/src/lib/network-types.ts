export type NetworkProxy = {
  id: string
  name: string
  url: string
  enabled: boolean
  username_ref?: string
  password_ref?: string
}

export type IncomingNetwork = {
  enabled: boolean
  ip_header: string
  format: 'single' | 'chain'
  trusted_proxies: string[]
}

export type PublicLimits = {
  setup_per_minute: number
  requests_per_minute: number
  concurrent: number
}

export type NetworkSettings = {
  revision: number
  incoming: IncomingNetwork
  public_limits: PublicLimits
  default: string
  proxies: NetworkProxy[]
  overrides: Record<string, string>
}
