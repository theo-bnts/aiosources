export type TrustedPermissions = {
  trending_enabled: boolean
  search_enabled: boolean
  stremio_enabled: boolean
  torznab_enabled: boolean
  catalog_enabled: boolean
  alerts_enabled: boolean
  use_instance_debrid: boolean
  use_instance_announces: boolean
}
export type TrustedUser = TrustedPermissions & {
  id: string
  email: string
  enabled: boolean
  revision: number
  created_at: string
  updated_at: string
}
export const trustedPermissionControls = [
  [
    'trending_enabled',
    'Allow Trending',
    'Browse trending titles across categories and download using the permitted debrid credentials.',
  ],
  [
    'search_enabled',
    'Allow Search',
    'Search the catalog and download using the permitted debrid credentials.',
  ],
  [
    'alerts_enabled',
    'Allow release alerts',
    'Set availability alerts for titles without releases using notification channels enabled by the administrator.',
  ],
  [
    'torznab_enabled',
    'Allow Torznab connections',
    'Create read-only Torznab connections. The selected announce credential policy applies to private sources.',
  ],
  [
    'stremio_enabled',
    'Allow Stremio connections',
    'Create installation links for Stremio and AIOStreams using the permitted debrid credentials.',
  ],
  [
    'catalog_enabled',
    'Allow shared catalogs',
    'Export read-only catalog data within the shared catalog data permissions. Source definitions and credentials remain private.',
  ],
] as const satisfies readonly (readonly [keyof TrustedPermissions, string, string])[]
