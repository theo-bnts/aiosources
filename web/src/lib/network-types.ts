export type NetworkProxy = {
  id: string
  name: string
  url: string
  enabled: boolean
  username_ref?: string
  password_ref?: string
}

export type NetworkSettings = {
  revision: number
  default: string
  proxies: NetworkProxy[]
  overrides: Record<string, string>
}
