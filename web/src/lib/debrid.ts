export const debridProviders = [
  {
    id: 'alldebrid',
    name: 'AllDebrid',
    secret: 'alldebrid_secret',
    credentialUrl: 'https://alldebrid.com/apikeys',
  },
  {
    id: 'debridlink',
    name: 'Debrid-Link',
    secret: 'debridlink_secret',
    credentialUrl: 'https://debrid-link.com/webapp/apikey',
  },
  {
    id: 'deepbrid',
    name: 'Deepbrid',
    secret: 'deepbrid_secret',
    credentialUrl: 'https://www.deepbrid.com/devices',
    hint: 'Cache status is known after download preparation. Other torrents remain unknown.',
  },
  {
    id: 'premiumize',
    name: 'Premiumize',
    secret: 'premiumize_secret',
    credentialUrl: 'https://www.premiumize.me/account',
  },
  {
    id: 'realdebrid',
    name: 'Real-Debrid',
    secret: 'realdebrid_secret',
    credentialUrl: 'https://real-debrid.com/apitoken',
    hint: 'Checks ready files in your account. Other torrents stay unknown; no global cache check is available.',
  },
  {
    id: 'torbox',
    name: 'TorBox',
    secret: 'torbox_secret',
    credentialUrl: 'https://torbox.app/settings',
  },
] as const

export type DebridService = (typeof debridProviders)[number]['id']
export type DebridSecret = (typeof debridProviders)[number]['secret']
export const debridServices = debridProviders.map(({ id, name }) => ({ id, name }))
export const serviceNames: Record<string, string> = {
  direct: 'Direct torrents',
  ...Object.fromEntries(debridProviders.map(({ id, name }) => [id, name])),
}
export function isDebridService(value: unknown): value is DebridService {
  return debridProviders.some((provider) => provider.id === value)
}
