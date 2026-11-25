import type { ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { resolveVisitorSection, useVisitorConfig } from './public-connection-fields'
import { PublicSharedCatalogEntry } from './public-shared-catalog-entry'
import { PublicStremioEntry } from './public-entry'
import { PublicTorznabEntry } from './public-torznab-entry'
import { PublicSearchEntry } from './public-search-entry'
import { Empty, ErrorNotice, Loading } from './common'
export function TrustedEntry({ layout }: { layout: (children: ReactNode) => ReactNode }) {
  const location = useLocation()
  const info = useVisitorConfig(true)
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
  const { section, available, validPath, fallbackTarget } = resolveVisitorSection(
    access,
    location.pathname,
    '/trusted',
  )
  if (!fallbackTarget)
    return layout(
      <div className="page">
        <Empty
          title="No features enabled"
          description="Ask the administrator to enable the features you need for this account."
        />
      </div>,
    )
  if (!validPath || !available) return <Navigate to={fallbackTarget} replace />
  if (section === 'search' || section === 'trending')
    return layout(<PublicSearchEntry info={access} />)
  if (section === 'catalog')
    return layout(<PublicSharedCatalogEntry key={access.catalog_revision} trusted info={access} />)
  return layout(
    section === 'torznab' ? (
      <PublicTorznabEntry info={access} />
    ) : (
      <PublicStremioEntry info={access} />
    ),
  )
}
