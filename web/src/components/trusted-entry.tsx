import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Navigate, useLocation } from 'react-router-dom'
import { api } from '@/lib/api'
import type { PublicInfo } from './public-connection-fields'
import { PublicSharedCatalogEntry } from './public-shared-catalog-entry'
import { PublicStremioEntry } from './public-entry'
import { PublicTorznabEntry } from './public-torznab-entry'
import { PublicSearchEntry } from './public-search-entry'
import { Empty, ErrorNotice, Loading } from './common'
export function TrustedEntry({ layout }: { layout: (children: ReactNode) => ReactNode }) {
  const location = useLocation()
  const info = useQuery({
    queryKey: ['trusted-config'],
    queryFn: ({ signal }) => api<PublicInfo>('/trusted/config', { signal }),
  })
  if (info.isPending)
    return (
      <div className="page">
        <Loading label="Loading your access" />
      </div>
    )
  if (!info.data)
    return (
      <div className="page">
        <ErrorNotice error={info.error} retry={() => void info.refetch()} />
      </div>
    )
  const access = info.data
  const target = access.stremio_enabled
    ? '/trusted'
    : access.search_enabled
      ? '/trusted/search'
      : access.torznab_enabled
        ? '/trusted/torznab'
        : access.catalog_enabled
          ? '/trusted/sharing'
          : ''
  const path = location.pathname
  const isSearch = path === '/trusted/search' || path.startsWith('/trusted/search/')
  const section = isSearch
    ? 'search'
    : path === '/trusted/torznab'
      ? 'torznab'
      : path === '/trusted/sharing'
        ? 'catalog'
        : 'stremio'
  const available =
    section === 'search'
      ? access.search_enabled
      : section === 'stremio'
        ? access.stremio_enabled
        : section === 'torznab'
          ? access.torznab_enabled
          : access.catalog_enabled
  if (!target)
    return layout(
      <div className="page">
        <Empty
          title="No features enabled"
          description="Ask the administrator to enable the features you need for this account."
        />
      </div>,
    )
  if (!path.startsWith('/trusted') || !available) return <Navigate to={target} replace />
  if (section === 'search') return layout(<PublicSearchEntry info={access} />)
  if (section === 'catalog') return layout(<PublicSharedCatalogEntry trusted info={access} />)
  return layout(
    section === 'torznab' ? (
      <PublicTorznabEntry info={access} />
    ) : (
      <PublicStremioEntry info={access} />
    ),
  )
}
