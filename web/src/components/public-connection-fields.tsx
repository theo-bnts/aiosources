import { TrashIcon } from '@phosphor-icons/react'
import { useQuery } from '@tanstack/react-query'
import { api, LIVE_REFRESH_INTERVAL } from '@/lib/api'
import type { CatalogEntities } from '@/lib/catalog-types'
import { visitorRoutes, type VisitorPrefix } from '@/lib/routes'
import { Disclosure } from '@/components/ui/disclosure'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { useEffect, useRef, useState } from 'react'
import { usePublicCredential } from '@/lib/public-credentials'

export function useSessionExpired(onExpired?: () => void) {
  const handler = useRef(onExpired)
  handler.current = onExpired
  useEffect(() => {
    if (!handler.current) return
    const listener = () => handler.current?.()
    window.addEventListener('session-expired', listener)
    return () => window.removeEventListener('session-expired', listener)
  }, [])
}

export function useRequestController() {
  const request = useRef<AbortController | null>(null)
  useEffect(() => () => request.current?.abort(), [])
  return request
}

export type VisitorSection = 'stremio' | 'trending' | 'search' | 'torznab' | 'catalog' | 'alerts'

export function resolveVisitorSection(
  info: PublicInfo,
  pathname: string,
  prefix: VisitorPrefix,
): {
  section: VisitorSection
  available: boolean
  validPath: boolean
  fallbackTarget: string
  canonicalPath?: string
} {
  const r = visitorRoutes(prefix)
  const titleMatch = new RegExp(`^${prefix}/(?:browse/)?search/([0-9]+)$`).exec(pathname)
  const isTitleDetail = Boolean(titleMatch)
  const isTrending = pathname === r.trending || pathname === `${prefix}/trending`
  const isSearchTorrents = pathname === r.searchTorrents || pathname === `${prefix}/search/torrents`
  const isSearch = pathname === r.search || pathname === `${prefix}/search` || isSearchTorrents
  const isTorznab = pathname === r.torznab || pathname === `${prefix}/torznab`
  const isCatalog = pathname === r.sharedCatalogs || pathname === `${prefix}/sharing`
  const isAlerts =
    prefix === '/trusted' && (pathname === r.alerts || pathname === `${prefix}/alerts`)
  const isStremio = pathname === r.stremio || pathname === prefix
  const validPath =
    isStremio || isTorznab || isCatalog || isTrending || isTitleDetail || isSearch || isAlerts
  const section: VisitorSection = isTrending
    ? 'trending'
    : isTitleDetail
      ? info.search_enabled
        ? 'search'
        : 'trending'
      : isSearch
        ? 'search'
        : isTorznab
          ? 'torznab'
          : isCatalog
            ? 'catalog'
            : isAlerts
              ? 'alerts'
              : 'stremio'
  const available = Boolean(
    isTitleDetail
      ? info.search_enabled || info.trending_enabled
      : section === 'trending'
        ? info.trending_enabled
        : section === 'search'
          ? info.search_enabled
          : section === 'stremio'
            ? info.stremio_enabled
            : section === 'torznab'
              ? info.torznab_enabled
              : section === 'alerts'
                ? prefix === '/trusted' && info.alerts_enabled
                : info.catalog_enabled,
  )
  const fallbackTarget = info.stremio_enabled
    ? r.stremio
    : info.trending_enabled
      ? r.trending
      : info.search_enabled
        ? r.search
        : info.torznab_enabled
          ? r.torznab
          : info.catalog_enabled
            ? r.sharedCatalogs
            : prefix === '/trusted' && info.alerts_enabled
              ? r.alerts
              : ''
  const canonicalPath = isTrending
    ? r.trending
    : titleMatch
      ? `${r.search}/${titleMatch[1]}`
      : isSearchTorrents
        ? r.searchTorrents
        : isSearch
          ? r.search
          : isTorznab
            ? r.torznab
            : isCatalog
              ? r.sharedCatalogs
              : isAlerts
                ? r.alerts
                : isStremio
                  ? r.stremio
                  : undefined
  return { section, available, validPath, fallbackTarget, canonicalPath }
}

export function pruneVisitorOptions<
  T extends { sources?: string[]; sorting?: Array<{ field: string; direction: 'asc' | 'desc' }> },
>(
  options: T,
  sources: { id: string; name: string }[] | undefined,
  allowSourceSort = true,
): T | null {
  const allowed = new Set((sources ?? []).map((source) => source.id))
  const selected = options.sources ?? []
  const nextSources = selected.filter((id) => allowed.has(id))
  const nextSorting = options.sorting?.filter((rule) => allowSourceSort || rule.field !== 'source')
  if (selected.length === nextSources.length && nextSorting?.length === options.sorting?.length) {
    return null
  }
  return {
    ...options,
    sources: nextSources,
    ...(options.sorting ? { sorting: nextSorting } : {}),
  }
}

export function useVisitorConfig(trusted = false, enabled = true) {
  return useQuery({
    queryKey: trusted ? ['trusted-config'] : ['public-config'],
    queryFn: ({ signal }) =>
      api<PublicInfo>(trusted ? '/trusted/config' : '/public/config', {
        signal,
        cache: 'no-store',
      }),
    enabled,
    refetchInterval: LIVE_REFRESH_INTERVAL,
    retry: false,
  })
}

// Clear personal inputs without resetting the controlled, non-secret filters.
export function clearPublicCredentials(form: HTMLFormElement) {
  for (const input of form.querySelectorAll<HTMLInputElement>('input[data-sensitive][name]')) {
    input.value = ''
    input.dispatchEvent(new Event('reset-sensitive'))
  }
}
export type PublicInfo = {
  trusted?: boolean
  credential_scope?: string
  use_instance_debrid?: boolean
  use_instance_announces?: boolean
  stremio_enabled?: boolean
  torznab_enabled?: boolean
  trending_enabled?: boolean
  search_enabled?: boolean
  alerts_enabled?: boolean
  catalog_enabled?: boolean
  catalog_revision?: number
  catalog_fields?: string[]
  catalog_entities?: CatalogEntities
  catalog_sources?: { id: string; name: string }[]
  sources?: { id: string; name: string }[]
  enabled: boolean
  providers: { id: string; name: string }[]
  private_sources?: { id: string; name: string }[]
}
export function PublicAnnounceFields({
  sources,
  pending,
  torznab = false,
  scope,
}: {
  sources: { id: string; name: string }[]
  pending: boolean
  torznab?: boolean
  scope?: string
}) {
  const saved = usePublicCredential(scope)
  const [removed, setRemoved] = useState<string[]>([])
  useEffect(() => setRemoved([]), [saved])
  if (!sources.length) return null
  return (
    <Disclosure
      id="public-announce-settings"
      title="Private source playback"
      help="Announce URLs are remembered in your encrypted browser configuration and included in your connection links. Without one, private sources are limited to known cached results."
    >
      <p className="help">
        Your announce URL unlocks uncached private-source downloads.{' '}
        {torznab
          ? 'Your client receives your announce in its magnets.'
          : 'These URLs are sent only to your debrid account when needed.'}{' '}
      </p>
      {sources.map((source) => {
        const retained = saved?.announces?.includes(source.id) && !removed.includes(source.id)
        return (
          <div className="field" key={source.id}>
            <Label htmlFor={`public-announce-${source.id}`}>{source.name} announce URL</Label>
            <div className="flex min-w-0 flex-col gap-2 sm:flex-row">
              <Input
                key={retained ? 'remembered' : 'new'}
                id={`public-announce-${source.id}`}
                name={`announce:${source.id}`}
                type="password"
                revealLabel={`${source.name} announce URL`}
                autoComplete="off"
                spellCheck={false}
                autoCapitalize="off"
                placeholder={
                  retained
                    ? 'Saved on this browser · enter a URL to replace it'
                    : 'Complete announce URL'
                }
                maxLength={8192}
                disabled={pending}
                autoFocus={removed.includes(source.id)}
              />
              {retained && (
                <Button
                  type="button"
                  variant="warning"
                  size="sm"
                  className="self-start sm:self-auto"
                  disabled={pending}
                  onClick={() =>
                    setRemoved((ids) => (ids.includes(source.id) ? ids : [...ids, source.id]))
                  }
                >
                  <TrashIcon aria-hidden="true" />
                  Remove announce
                </Button>
              )}
            </div>
          </div>
        )
      })}
      {removed.map((id) => (
        <input key={id} type="hidden" name="remove_announce" value={id} />
      ))}
      {removed.length > 0 && (
        <p className="help">Apply your changes to remove these saved announces.</p>
      )}
    </Disclosure>
  )
}
