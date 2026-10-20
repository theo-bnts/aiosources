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

export type NetworkSettings = {
  revision: number
  incoming: IncomingNetwork
  default: string
  proxies: NetworkProxy[]
  overrides: Record<string, string>
}
