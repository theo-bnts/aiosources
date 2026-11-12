export type TrustedPermissions = {
  search_enabled: boolean
  stremio_enabled: boolean
  torznab_enabled: boolean
  catalog_enabled: boolean
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
    'search_enabled',
    'Allow Search',
    'Search the catalog and download using the permitted debrid credentials.',
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
