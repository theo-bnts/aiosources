import {
  AppWindowIcon,
  ArrowClockwiseIcon,
  BellIcon,
  BookOpenIcon,
  BroadcastIcon,
  CaretDownIcon,
  CloudArrowDownIcon,
  CloudCheckIcon,
  CompassIcon,
  DatabaseIcon,
  DevicesIcon,
  DownloadSimpleIcon,
  FilmStripIcon,
  GameControllerIcon,
  GaugeIcon,
  GearSixIcon,
  GlobeIcon,
  KeyIcon,
  ListChecksIcon,
  ListIcon,
  ListMagnifyingGlassIcon,
  MagnifyingGlassIcon,
  MusicNotesIcon,
  PlayCircleIcon,
  PlugsConnectedIcon,
  ShareNetworkIcon,
  ShieldCheckIcon,
  SignInIcon,
  SignOutIcon,
  StackIcon,
  TerminalWindowIcon,
  TrendUpIcon,
  UsersThreeIcon,
  XIcon,
} from '@phosphor-icons/react'
import { readNavigationPreferences } from '@/lib/navigation-preferences'
import { SettingsSearch } from '@/components/settings-search'
import { SettingsDestination } from '@/components/settings-destination'
import {
  Component,
  Suspense,
  lazy,
  useEffect,
  useId,
  useRef,
  useState,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, Outlet, useLocation, Navigate } from 'react-router-dom'
import { UnsavedChangesScope, useDiscardChanges } from '@/lib/unsaved-changes'
import { isPageTabNavigation } from '@/lib/page-tabs'
import { prefetchRoute, schedulePrimaryRoutePrefetch } from '@/lib/route-prefetch'
import { routes, visitorRoutes, type VisitorPrefix } from '@/lib/routes'
import type { Icon } from '@phosphor-icons/react'

import {
  api,
  ApiError,
  expireSession,
  queryClient,
  refreshLiveData,
  setCSRF,
  AUTO_REFRESH_INTERVAL,
  LIVE_REFRESH_INTERVAL,
} from '@/lib/api'
import type { Session } from '@/lib/types'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { ErrorNotice, Loading, PageHeadingLevel, PageHeaderVisibility } from '@/components/common'
import { Login } from '@/components/login'
import { useSessionExpired, useVisitorConfig } from '@/components/public-connection-fields'

const PublicEntry = lazy(() =>
  import('@/components/public-entry').then((module) => ({ default: module.PublicEntry })),
)
const TrustedEntry = lazy(() =>
  import('@/components/trusted-entry').then((module) => ({ default: module.TrustedEntry })),
)

export class RootBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state = { error: null as Error | null }
  static getDerivedStateFromError(error: Error) {
    return { error }
  }
  render() {
    if (this.state.error)
      return (
        <main className="mx-auto max-w-xl space-y-5 px-6 py-24">
          <h1 className="page-title">The console could not be displayed</h1>
          <p className="page-description">
            Reload the application. Data already saved on the server is not affected.
          </p>
          <ErrorNotice error={this.state.error} />
          <Button onClick={() => window.location.reload()}>
            <ArrowClockwiseIcon aria-hidden="true" />
            Reload application
          </Button>
        </main>
      )
    return this.props.children
  }
}
type NavigationItem = {
  to: string
  match?: string
  label: string
  description: string
  icon: Icon
  children?: NavigationItem[]
}
type NavigationGroup = {
  id: string
  label: string
  icon: Icon
  defaultOpen: boolean
  standalone?: boolean
  items: NavigationItem[]
}
function navigationItems(items: NavigationItem[]): NavigationItem[] {
  return items.flatMap((item) => [item, ...navigationItems(item.children ?? [])])
}
function navigationSections(groups: NavigationGroup[]) {
  return groups
    .flatMap((group) => navigationItems(group.items))
    .filter((item) => item.children?.length)
}
const navigationGroups: NavigationGroup[] = [
  {
    id: 'collection',
    label: 'Sources',
    icon: DownloadSimpleIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.sources.indexers.base,
        label: 'Indexers',
        description: 'Manage your indexer connections.',
        icon: PlugsConnectedIcon,
        children: [
          {
            to: routes.sources.indexers.torrents.statistics,
            match: routes.sources.indexers.torrents.base,
            label: 'Torrents',
            description: 'Torrent source connections and automatic imports.',
            icon: DownloadSimpleIcon,
          },
        ],
      },
      {
        to: routes.sources.remoteCatalogs.statistics,
        match: routes.sources.remoteCatalogs.base,
        label: 'Remote catalogs',
        description: 'Import read-only catalogs shared by other instances.',
        icon: CloudArrowDownIcon,
      },
      {
        to: routes.sources.files.statistics,
        match: routes.sources.files.base,
        label: 'Files',
        description: 'Browse imported files and see how they are distributed across sources.',
        icon: ListMagnifyingGlassIcon,
      },
    ],
  },
  {
    id: 'catalog',
    label: 'Metadata',
    icon: StackIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.metadata.catalogs.base,
        label: 'Catalogs',
        description: 'Manage video, game, software, music and book information.',
        icon: DatabaseIcon,
        children: [
          {
            to: routes.metadata.catalogs.videos.statistics,
            match: routes.metadata.catalogs.videos.base,
            label: 'Videos',
            description: 'Film and series information, updates and history.',
            icon: FilmStripIcon,
          },
          {
            to: routes.metadata.catalogs.games.statistics,
            match: routes.metadata.catalogs.games.base,
            label: 'Games',
            description: 'Game information, updates and history.',
            icon: GameControllerIcon,
          },
          {
            to: routes.metadata.catalogs.software.statistics,
            match: routes.metadata.catalogs.software.base,
            label: 'Software',
            description: 'Software application catalog, icons index and history.',
            icon: AppWindowIcon,
          },
          {
            to: routes.metadata.catalogs.music.statistics,
            match: routes.metadata.catalogs.music.base,
            label: 'Music',
            description: 'Music release information, Cover Art Archive index and history.',
            icon: MusicNotesIcon,
          },
          {
            to: routes.metadata.catalogs.books.statistics,
            match: routes.metadata.catalogs.books.base,
            label: 'Books',
            description: 'Book and audiobook information, updates and history.',
            icon: BookOpenIcon,
          },
        ],
      },
      {
        to: routes.metadata.identification.statistics,
        match: routes.metadata.identification.base,
        label: 'Identification',
        description: 'Review and correct torrent identification.',
        icon: ListChecksIcon,
      },
    ],
  },
  {
    id: 'playback',
    label: 'Debrid',
    standalone: true,
    icon: CloudCheckIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.debrid.statistics,
        match: routes.debrid.base,
        label: 'Debrid',
        description: 'Cache availability and playback providers.',
        icon: CloudCheckIcon,
      },
    ],
  },
  {
    id: 'browse',
    label: 'Browse',
    icon: CompassIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.browse.trending,
        label: 'Trending',
        description: 'Most active identified titles across each category for a chosen period.',
        icon: TrendUpIcon,
      },
      {
        to: routes.browse.search,
        label: 'Search',
        description: 'Find identified titles and download through your debrid services.',
        icon: MagnifyingGlassIcon,
      },
    ],
  },
  {
    id: 'integrations',
    label: 'Connections',
    icon: PlugsConnectedIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.connections.torznab,
        label: 'Torznab',
        description: 'Connect external applications to your published catalog.',
        icon: BroadcastIcon,
      },
      {
        to: routes.connections.stremio.statistics,
        match: routes.connections.stremio.base,
        label: 'Stremio',
        description: 'Connect your catalog to Stremio or AIOStreams.',
        icon: PlayCircleIcon,
      },
      {
        to: routes.connections.sharedCatalogs.statistics,
        match: routes.connections.sharedCatalogs.base,
        label: 'Shared catalogs',
        description: 'Share published catalogs with other instances.',
        icon: ShareNetworkIcon,
      },
      {
        to: routes.connections.alerts.active,
        match: routes.connections.alerts.base,
        label: 'Alerts',
        description: 'Configure notification channels and track release alerts.',
        icon: BellIcon,
      },
    ],
  },
  {
    id: 'administration',
    label: 'Administration',
    icon: GearSixIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.administration.security.base,
        label: 'Security',
        description: 'Encrypted secrets, sign-in protection and active sessions.',
        icon: ShieldCheckIcon,
        children: [
          {
            to: routes.administration.security.secrets,
            label: 'Secrets',
            description: 'Manage encrypted credentials.',
            icon: KeyIcon,
          },
          {
            to: routes.administration.security.limits,
            label: 'Limits',
            description: 'Manage administrator, trusted user and public access limits.',
            icon: GaugeIcon,
          },
          {
            to: routes.administration.security.activeSessions,
            label: 'Active sessions',
            description: 'Review and revoke signed-in devices.',
            icon: DevicesIcon,
          },
        ],
      },
      {
        to: routes.administration.network.base,
        label: 'Network',
        description: 'Incoming proxy trust and outgoing connections.',
        icon: GlobeIcon,
        children: [
          {
            to: routes.administration.network.incoming,
            label: 'Incoming',
            description: 'Recognize visitor IP addresses behind trusted proxies.',
            icon: ShieldCheckIcon,
          },
          {
            to: routes.administration.network.outgoing.proxies,
            match: routes.administration.network.outgoing.base,
            label: 'Outgoing',
            description: 'Manage proxies and choose how services connect.',
            icon: GlobeIcon,
          },
        ],
      },
      {
        to: routes.administration.users.base,
        label: 'Users',
        description: 'Trusted user accounts and visitor access.',
        icon: UsersThreeIcon,
        children: [
          {
            to: routes.administration.users.trusted,
            label: 'Trusted',
            description: 'Manage user accounts, feature permissions and credentials.',
            icon: UsersThreeIcon,
          },
          {
            to: routes.administration.users.public,
            label: 'Public',
            description: 'Manage visitor permissions and credentials.',
            icon: GlobeIcon,
          },
        ],
      },
      {
        to: routes.administration.storage.statistics,
        match: routes.administration.storage.base,
        label: 'Storage',
        description: 'PostgreSQL storage, local available space and data retention.',
        icon: DatabaseIcon,
      },
      {
        to: routes.administration.logs,
        label: 'Logs',
        description: 'Service activity and alerts.',
        icon: TerminalWindowIcon,
      },
    ],
  },
]
const publicNavigationGroups: NavigationGroup[] = [
  {
    id: 'browse',
    label: 'Browse',
    icon: CompassIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.public.trending,
        label: 'Trending',
        description:
          'Most active films, series, games, software, music and books for a chosen period.',
        icon: TrendUpIcon,
      },
      {
        to: routes.public.search,
        label: 'Search',
        description:
          'Find films, series, games, software, music and books and download through your own debrid accounts.',
        icon: MagnifyingGlassIcon,
      },
    ],
  },
  {
    id: 'integrations',
    label: 'Connections',
    icon: PlugsConnectedIcon,
    defaultOpen: true,
    items: [
      {
        to: routes.public.torznab,
        label: 'Torznab',
        description: 'Connect a Torznab client with your own optional announce URLs.',
        icon: BroadcastIcon,
      },
      {
        to: routes.public.stremio,
        label: 'Stremio',
        description: 'Connect Stremio or AIOStreams with your own debrid accounts.',
        icon: PlayCircleIcon,
      },
      {
        to: routes.public.sharedCatalogs,
        label: 'Shared catalogs',
        description: 'Keep a read-only catalog copy on another AIOSources instance.',
        icon: ShareNetworkIcon,
      },
      {
        to: routes.public.alerts,
        label: 'Alerts',
        description: 'Manage your release availability alerts.',
        icon: BellIcon,
      },
    ],
  },
]
const navigationStorageKey = 'aiosources.navigation.groups'
const navigationLinkClassName =
  'flex min-h-(--control-height) items-center gap-3 rounded-lg px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-[current=page]:bg-primary/10 aria-[current=page]:font-medium aria-[current=page]:text-primary'

function AppNavigation({
  groups = navigationGroups,
  activeItem,
  activeGroupId,
  openGroups,
  onToggle,
  onNavigate,
}: {
  groups?: NavigationGroup[]
  activeItem: NavigationItem | undefined
  activeGroupId: string | undefined
  openGroups: Record<string, boolean>
  onToggle: (groupId: string) => void
  onNavigate?: () => void
}) {
  const id = useId()
  const navRef = useRef<HTMLElement>(null)
  const activeGroupOpen = activeGroupId ? openGroups[activeGroupId] : true
  useEffect(() => {
    navRef.current?.querySelector('[aria-current="page"]')?.scrollIntoView({ block: 'nearest' })
  }, [activeItem, activeGroupOpen])
  const handleNavigate = (event: MouseEvent<HTMLAnchorElement>) => {
    if (
      !event.defaultPrevented &&
      event.button === 0 &&
      !event.metaKey &&
      !event.ctrlKey &&
      !event.shiftKey &&
      !event.altKey
    ) {
      onNavigate?.()
    }
  }
  function navigationLink(item: NavigationItem) {
    return (
      <Link
        key={item.to}
        to={item.to}
        aria-current={activeItem === item ? 'page' : undefined}
        className={navigationLinkClassName}
        onClick={handleNavigate}
        onMouseEnter={() => prefetchRoute(item.to)}
        onFocus={() => prefetchRoute(item.to)}
        onPointerDown={() => prefetchRoute(item.to)}
      >
        <item.icon size={19} aria-hidden="true" className="shrink-0" />
        {item.label}
      </Link>
    )
  }
  function navigationItem(item: NavigationItem): ReactNode {
    if (!item.children?.length) return <li key={item.to}>{navigationLink(item)}</li>
    const expanded = openGroups[item.to] ?? false
    return (
      <li key={item.to} className="space-y-1">
        <button
          type="button"
          aria-expanded={expanded}
          aria-controls={`${id}-${item.to}`}
          onClick={() => onToggle(item.to)}
          className={`${navigationLinkClassName} w-full text-left`}
        >
          <item.icon size={19} aria-hidden="true" className="shrink-0" />
          <span className="min-w-0 flex-1">{item.label}</span>
          <CaretDownIcon
            aria-hidden="true"
            className={`size-3.5 shrink-0 transition-transform motion-reduce:transition-none ${expanded ? '' : '-rotate-90'}`}
          />
        </button>
        <ul
          id={`${id}-${item.to}`}
          hidden={!expanded}
          className="ml-5 space-y-0.5 border-l border-border pl-2"
        >
          {item.children.map(navigationItem)}
        </ul>
      </li>
    )
  }
  return (
    <nav
      ref={navRef}
      aria-label="Main navigation"
      className="min-h-0 flex-1 space-y-1 overflow-y-auto overscroll-contain px-3 py-3.5 [scrollbar-width:thin]"
    >
      {groups
        .filter((group) => group.items.length > 0)
        .map((group) =>
          group.standalone ? (
            navigationLink(group.items[0])
          ) : (
            <section
              key={group.id}
              className={`space-y-1 ${(openGroups[group.id] ?? group.defaultOpen) ? 'pb-2' : ''}`}
            >
              <h2>
                <button
                  type="button"
                  aria-expanded={openGroups[group.id] ?? group.defaultOpen}
                  aria-controls={`${id}-${group.id}`}
                  onClick={() => onToggle(group.id)}
                  className="flex min-h-(--control-height) w-full items-center gap-3 rounded-lg px-3 py-2 text-left text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-expanded:text-foreground"
                >
                  <group.icon size={19} aria-hidden="true" className="shrink-0" />
                  <span className="min-w-0 flex-1">{group.label}</span>
                  <CaretDownIcon
                    aria-hidden="true"
                    className={`size-3.5 shrink-0 transition-transform motion-reduce:transition-none ${(openGroups[group.id] ?? group.defaultOpen) ? '' : '-rotate-90'}`}
                  />
                </button>
              </h2>
              <ul
                id={`${id}-${group.id}`}
                hidden={!(openGroups[group.id] ?? group.defaultOpen)}
                className="ml-5 space-y-0.5 border-l border-border pl-2"
              >
                {group.items.map(navigationItem)}
              </ul>
            </section>
          ),
        )}
    </nav>
  )
}

export function SessionGate({ children }: { children: ReactNode }) {
  const location = useLocation()
  const session = useQuery({
    queryKey: ['session'],
    queryFn: async ({ signal }) => {
      const value = await api<Session>('/session', { signal })
      setCSRF(value.csrf_token)
      return value
    },
    staleTime: 60_000,
    refetchInterval: AUTO_REFRESH_INTERVAL,
    retry: false,
  })
  useSessionExpired(() => {
    setCSRF()
    queryClient.setQueryData<Session>(['session'], { authenticated: false })
    queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'session' })
    queryClient.getMutationCache().clear()
    void queryClient.cancelQueries()
  })
  if (session.isPending)
    return (
      <div className="mx-auto max-w-lg px-6 py-24">
        <Loading label="Checking session" />
      </div>
    )
  if (!session.data)
    return (
      <div className="mx-auto max-w-lg space-y-6 px-6 py-24">
        <h1 className="page-title">Connect to the server</h1>
        <ErrorNotice error={session.error} retry={() => void session.refetch()} />
      </div>
    )
  const visitorFallback = (
    <div className="mx-auto max-w-lg px-6 py-24">
      <Loading label="Loading page" />
    </div>
  )
  if (location.pathname === '/user-login') return <Navigate to={routes.login} replace />
  if (session.data.role === 'trusted' && location.pathname === routes.login) return <Login />
  if (session.data.role === 'trusted' && location.pathname !== routes.login)
    return (
      <Suspense fallback={visitorFallback}>
        <TrustedEntry
          layout={(content) => (
            <AppShell publicMode trustedMode>
              {content}
            </AppShell>
          )}
        />
      </Suspense>
    )
  if (location.pathname === '/trusted' || location.pathname.startsWith('/trusted/')) {
    if (!session.data.authenticated) return <Login />
    return <Navigate to={routes.administration.users.trusted} replace />
  }
  if (location.pathname === '/public' || location.pathname.startsWith('/public/'))
    return (
      <Suspense fallback={visitorFallback}>
        <PublicEntry
          layout={(content) => (
            <AppShell publicMode authenticated={session.data.authenticated}>
              {content}
            </AppShell>
          )}
        />
      </Suspense>
    )
  if (!session.data.authenticated)
    return location.pathname === routes.login ? (
      <Login />
    ) : (
      <Suspense fallback={visitorFallback}>
        <PublicEntry layout={(content) => <AppShell publicMode>{content}</AppShell>} />
      </Suspense>
    )
  if (location.pathname === routes.login)
    return <Navigate to={routes.sources.indexers.torrents.statistics} replace />
  return (
    <>
      <ErrorNotice error={session.error} retry={() => void session.refetch()} />
      {children}
    </>
  )
}

function useLiveUpdates(enabled = true) {
  useEffect(() => {
    if (!enabled) return
    const source = new EventSource('/api/events', { withCredentials: true })
    let timer: number | undefined
    let nextRefresh = 0
    const queueRefresh = () => {
      if (timer === undefined)
        timer = window.setTimeout(
          () => {
            timer = undefined
            nextRefresh = performance.now() + LIVE_REFRESH_INTERVAL()
            void refreshLiveData()
          },
          Math.max(500, nextRefresh - performance.now()),
        )
    }
    source.onopen = queueRefresh
    source.addEventListener('update', queueRefresh)
    source.onerror = () => {
      void queryClient.invalidateQueries({ queryKey: ['session'] }, { cancelRefetch: false })
    }
    return () => {
      source.close()
      clearTimeout(timer)
    }
  }, [enabled])
}

export function AppShell({
  publicMode = false,
  trustedMode = false,
  authenticated = false,
  children,
}: {
  publicMode?: boolean
  trustedMode?: boolean
  authenticated?: boolean
  children?: ReactNode
}) {
  const publicInfo = useVisitorConfig(trustedMode, publicMode)
  const baseGroups = publicMode
    ? publicNavigationGroups
        .map((group) => ({
          ...group,
          items: group.items.filter((item) =>
            item.to === routes.public.trending
              ? publicInfo.data?.trending_enabled
              : item.to === routes.public.search
                ? publicInfo.data?.search_enabled
                : item.to === routes.public.stremio
                  ? publicInfo.data?.stremio_enabled
                  : item.to === routes.public.torznab
                    ? publicInfo.data?.torznab_enabled
                    : item.to === routes.public.alerts
                      ? trustedMode && publicInfo.data?.alerts_enabled
                      : publicInfo.data?.catalog_enabled,
          ),
        }))
        .filter((group) => group.items.length > 0)
    : navigationGroups
  const groups = trustedMode
    ? baseGroups.map((group) => ({
        ...group,
        items: group.items.map((item) => ({
          ...item,
          to: item.to.replace(/^\/public(?=\/|$)/, '/trusted'),
          ...(item.match ? { match: item.match.replace(/^\/public(?=\/|$)/, '/trusted') } : {}),
          description: item.description
            .replace('your own debrid accounts', 'the instance’s debrid accounts')
            .replace('your own optional announce URLs', 'the instance’s permitted sources'),
        })),
      }))
    : baseGroups
  const items = groups.flatMap((group) => navigationItems(group.items))
  const storageKey = publicMode ? 'aiosources.public.navigation.groups' : navigationStorageKey
  const discard = useDiscardChanges('all')
  useLiveUpdates(!publicMode)
  useEffect(() => {
    if (publicMode || trustedMode) return
    return schedulePrimaryRoutePrefetch()
  }, [publicMode, trustedMode])
  const location = useLocation()
  const visitorBrowseSection = (prefix: VisitorPrefix) => {
    const r = visitorRoutes(prefix)
    if (location.pathname === r.trending || location.pathname === `${prefix}/trending`) {
      return r.trending
    }
    if (
      location.pathname === r.search ||
      location.pathname.startsWith(`${r.search}/`) ||
      location.pathname === `${prefix}/search` ||
      location.pathname.startsWith(`${prefix}/search/`)
    ) {
      return !publicInfo.data?.search_enabled && publicInfo.data?.trending_enabled
        ? r.trending
        : r.search
    }
    if (location.pathname === r.torznab || location.pathname === `${prefix}/torznab`) {
      return r.torznab
    }
    if (location.pathname === r.sharedCatalogs || location.pathname === `${prefix}/sharing`) {
      return r.sharedCatalogs
    }
    if (
      prefix === '/trusted' &&
      (location.pathname === r.alerts || location.pathname === `${prefix}/alerts`)
    ) {
      return r.alerts
    }
    return r.stremio
  }
  let navigationPath = publicMode
    ? visitorBrowseSection('/public')
    : location.pathname.startsWith('/metadata/history')
      ? routes.metadata.catalogs.videos.statistics
      : location.pathname
  if (trustedMode) navigationPath = visitorBrowseSection('/trusted')
  const activeItem = [...items]
    .sort((a, b) => (b.match ?? b.to).length - (a.match ?? a.to).length)
    .find((item) => {
      if (item.children?.length) return false
      const prefix = item.match ?? item.to
      return (
        navigationPath === prefix || (prefix !== '/' && navigationPath.startsWith(`${prefix}/`))
      )
    })
  const activeGroup = groups.find((group) => navigationItems(group.items).includes(activeItem!))
  const activeSections = navigationSections(groups).filter((item) =>
    navigationItems(item.children ?? []).includes(activeItem!),
  )
  const activeSectionKey = activeSections.map((item) => item.to).join('|')
  const activeGroupId = activeGroup?.id
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    let stored: unknown
    try {
      stored = readNavigationPreferences(localStorage, storageKey)
    } catch {
      // Browser preferences must not prevent access to the console.
    }
    return Object.fromEntries([
      ...groups.map((group) => {
        const preference =
          stored !== null && typeof stored === 'object'
            ? (stored as Record<string, unknown>)[group.id]
            : undefined
        return [
          group.id,
          group.id === activeGroup?.id ||
            (typeof preference === 'boolean' ? preference : group.defaultOpen),
        ]
      }),
      ...navigationSections(groups).map((item) => {
        const preference =
          stored !== null && typeof stored === 'object'
            ? (stored as Record<string, unknown>)[item.to]
            : undefined
        return [item.to, activeSections.includes(item) || preference === true]
      }),
    ])
  })
  const [mobileOpen, setMobileOpen] = useState(false)
  const mainRef = useRef<HTMLElement>(null)
  const shellRef = useRef<HTMLDivElement>(null)
  const headerRef = useRef<HTMLElement>(null)
  useEffect(() => {
    const header = headerRef.current
    const shell = shellRef.current
    if (!header || !shell) return
    const measure = () => {
      const top = parseFloat(getComputedStyle(header).top) || 0
      shell.style.setProperty(
        '--app-header-height',
        `${header.getBoundingClientRect().height + top}px`,
      )
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(header)
    return () => observer.disconnect()
  }, [])
  const previousPath = useRef(location.pathname)
  const focusMainOnClose = useRef(false)
  useEffect(() => {
    setMobileOpen(false)
    const activeKeys = [activeGroupId, ...activeSectionKey.split('|')].filter(Boolean) as string[]
    setOpenGroups((current) =>
      activeKeys.every((key) => current[key])
        ? current
        : {
            ...current,
            ...Object.fromEntries(activeKeys.map((key) => [key, true])),
          },
    )
    if (previousPath.current !== location.pathname) {
      if (!isPageTabNavigation(location.state)) {
        focusMainOnClose.current = true
        mainRef.current?.focus({ preventScroll: true })
        window.scrollTo({ top: 0 })
      }
      previousPath.current = location.pathname
    }
  }, [location.pathname, location.state, activeGroupId, activeSectionKey])
  useEffect(() => {
    try {
      localStorage.setItem(storageKey, JSON.stringify(openGroups))
    } catch {
      // Keep the current session usable when browser storage is unavailable.
    }
  }, [openGroups, storageKey])
  useEffect(() => {
    const desktop = window.matchMedia('(min-width: 64rem)')
    const closeOnDesktop = (event: MediaQueryListEvent) => {
      if (event.matches) {
        focusMainOnClose.current = true
        setMobileOpen(false)
      }
    }
    desktop.addEventListener('change', closeOnDesktop)
    return () => desktop.removeEventListener('change', closeOnDesktop)
  }, [])
  const logout = useMutation({
    mutationFn: () => api<void>(trustedMode ? '/trusted/logout' : '/logout', { method: 'POST' }),
    onSuccess: expireSession,
    onError: (error) => {
      if (error instanceof ApiError && error.status === 401) {
        expireSession()
      }
    },
  })
  const navigationProps = {
    groups,
    activeItem,
    activeGroupId,
    openGroups,
    onToggle: (groupId: string) =>
      setOpenGroups((current) => ({
        ...current,
        [groupId]: !(
          current[groupId] ??
          groups.find((group) => group.id === groupId)?.defaultOpen ??
          false
        ),
      })),
  }
  const branding = (
    <>
      <DatabaseIcon
        size={24}
        aria-hidden="true"
        className="shrink-0 text-primary drop-shadow-[0_0_12px_rgba(135,202,188,0.25)]"
      />
      <span className="text-2xl leading-none font-semibold tracking-tight">AIOSources</span>
    </>
  )
  const footer =
    publicMode && !trustedMode ? (
      <div className="shrink-0 space-y-2 p-3">
        <Button variant="outline" className="w-full" asChild>
          <Link to={authenticated ? routes.sources.indexers.torrents.statistics : routes.login}>
            {authenticated ? (
              <ShieldCheckIcon aria-hidden="true" />
            ) : (
              <SignInIcon aria-hidden="true" />
            )}
            {authenticated ? 'Administration' : 'Login'}
          </Link>
        </Button>
      </div>
    ) : (
      <div className="shrink-0 space-y-3 p-3">
        <ErrorNotice error={logout.error} scope="action" />
        <Button
          variant="warning"
          className="w-full justify-center"
          disabled={logout.isPending}
          onClick={() => discard(() => logout.mutate())}
        >
          <SignOutIcon aria-hidden="true" />
          {logout.isPending ? 'Signing out…' : 'Sign out'}
        </Button>
      </div>
    )
  return (
    <Dialog
      open={mobileOpen}
      onOpenChange={(open) => {
        if (open) focusMainOnClose.current = false
        setMobileOpen(open)
      }}
    >
      <div ref={shellRef} data-app-shell className="min-h-dvh">
        <a
          href="#main-content"
          className="fixed left-4 top-3 z-100 -translate-y-24 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:translate-y-0"
        >
          Skip to content
        </a>
        <aside className="fixed inset-y-3 left-3 z-30 hidden w-60 flex-col overflow-hidden rounded-2xl border border-border/80 bg-chrome/88 shadow-chrome backdrop-blur-md lg:flex">
          <div className="flex h-16 shrink-0 items-center gap-3 border-b border-border/70 px-5">
            {branding}
          </div>
          <AppNavigation {...navigationProps} />
          {footer}
        </aside>
        <div className="pt-3 lg:pl-[calc(15rem+var(--shell-gap))]">
          <header
            ref={headerRef}
            className="sticky top-3 z-(--layer-header) mx-3 flex min-h-16 flex-wrap items-center justify-between gap-3 rounded-2xl border border-border/80 bg-chrome/88 px-3 py-3 shadow-chrome backdrop-blur-md md:pl-5 md:pr-4 lg:pr-[calc(1rem+var(--header-actions-width)+0.75rem)]"
          >
            <div className="flex min-w-0 flex-1 items-start gap-2.5 pr-[calc(var(--header-actions-width)+0.75rem)] sm:gap-3 lg:items-center lg:pr-0">
              <DialogTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="rounded-lg lg:hidden"
                  aria-label="Open navigation"
                  aria-controls="mobile-navigation"
                >
                  <ListIcon aria-hidden="true" />
                </Button>
              </DialogTrigger>
              <nav
                aria-label="Breadcrumb"
                className="min-w-0 space-y-1 text-xs text-muted-foreground"
              >
                <ol className="flex flex-wrap items-center gap-x-3 gap-y-1">
                  {activeGroup && !activeGroup.standalone && (
                    <li className="flex items-center gap-3">
                      {activeGroup.label}
                      <span aria-hidden="true" className="text-border">
                        /
                      </span>
                    </li>
                  )}
                  {activeSections.map((section) => (
                    <li key={section.to} className="flex items-center gap-3">
                      {section.label}
                      <span aria-hidden="true" className="text-border">
                        /
                      </span>
                    </li>
                  ))}
                  <li aria-current="page" className="text-foreground">
                    <h1
                      className="text-sm font-medium"
                      aria-describedby={activeItem ? 'page-description' : undefined}
                    >
                      {activeItem?.label ?? 'AIOSources'}
                    </h1>
                  </li>
                </ol>
                {activeItem && (
                  <p id="page-description" className="text-xs leading-4">
                    {activeItem.description}
                  </p>
                )}
              </nav>
            </div>
            <div className="order-last w-full basis-full lg:order-none lg:w-auto lg:basis-auto">
              <SettingsSearch
                trusted={trustedMode}
                visibility={{
                  admin: !publicMode,
                  trending: publicInfo.data?.trending_enabled,
                  search: publicInfo.data?.search_enabled,
                  stremio: publicInfo.data?.stremio_enabled,
                  torznab: publicInfo.data?.torznab_enabled,
                  catalog: publicInfo.data?.catalog_enabled,
                  catalogEntities: publicInfo.data?.catalog_entities,
                  sources: Boolean(publicInfo.data?.sources),
                  announces: Boolean(publicInfo.data?.private_sources?.length),
                  services: publicInfo.data?.use_instance_debrid
                    ? []
                    : publicInfo.data?.providers.map((service) => service.id),
                }}
              />
            </div>
          </header>
          <main id="main-content" ref={mainRef} tabIndex={-1} className="outline-none">
            <PageHeadingLevel.Provider value={2}>
              <PageHeaderVisibility.Provider
                value={{
                  title: location.pathname !== activeItem?.to,
                  description: location.pathname !== activeItem?.to,
                }}
              >
                <Suspense
                  fallback={
                    <div className="page">
                      <Loading label="Loading page" />
                    </div>
                  }
                >
                  <UnsavedChangesScope.Provider value={null}>
                    {children ?? <Outlet />}
                  </UnsavedChangesScope.Provider>
                </Suspense>
              </PageHeaderVisibility.Provider>
            </PageHeadingLevel.Provider>
          </main>
          <SettingsDestination />
        </div>
        <DialogContent
          id="mobile-navigation"
          aria-describedby={undefined}
          showCloseButton={false}
          className="top-3 bottom-3 left-3 flex h-[calc(100dvh-1.5rem)] max-h-[calc(100dvh-1.5rem)] w-80 max-w-[calc(100%-1.5rem)] translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-2xl border border-border/80 bg-chrome/95 p-0 shadow-chrome backdrop-blur-md data-[state=closed]:animate-none data-[state=open]:animate-none sm:max-w-80"
          onCloseAutoFocus={(event) => {
            if (focusMainOnClose.current) {
              event.preventDefault()
              focusMainOnClose.current = false
              mainRef.current?.focus({ preventScroll: true })
            }
          }}
        >
          <DialogTitle className="sr-only">Main navigation</DialogTitle>
          <div className="flex h-16 shrink-0 items-center gap-3 border-b border-border/70 px-5">
            {branding}
            <DialogClose asChild>
              <Button
                variant="ghost"
                size="icon"
                className="ml-auto shrink-0 rounded-lg"
                aria-label="Close navigation"
              >
                <XIcon aria-hidden="true" />
              </Button>
            </DialogClose>
          </div>
          <AppNavigation
            {...navigationProps}
            onNavigate={() => {
              focusMainOnClose.current = true
              setMobileOpen(false)
            }}
          />
          {footer}
        </DialogContent>
      </div>
    </Dialog>
  )
}
