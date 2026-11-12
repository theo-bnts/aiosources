import { readNavigationPreferences } from '@/lib/navigation-preferences'
import { SettingsSearch } from '@/components/settings-search'
import { SettingsDestination } from '@/components/settings-destination'
import {
  Component,
  Suspense,
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
import type { Icon } from '@phosphor-icons/react'
import {
  DatabaseIcon,
  PlugsConnectedIcon,
  MagnifyingGlassIcon,
  ListMagnifyingGlassIcon,
  StackIcon,
  CloudCheckIcon,
  PlayCircleIcon,
  SignOutIcon,
  ListIcon,
  XIcon,
  ArrowClockwiseIcon,
  TerminalWindowIcon,
  ShareNetworkIcon,
  ShieldCheckIcon,
  CaretDownIcon,
  DownloadSimpleIcon,
  CloudArrowDownIcon,
  GearSixIcon,
  ListChecksIcon,
  BroadcastIcon,
  GlobeIcon,
  UsersThreeIcon,
  FilmStripIcon,
  BookOpenIcon,
  GameControllerIcon,
  KeyIcon,
  DevicesIcon,
} from '@phosphor-icons/react'
import {
  api,
  queryClient,
  refreshLiveData,
  setCSRF,
  AUTO_REFRESH_INTERVAL,
  LIVE_REFRESH_INTERVAL,
} from '@/lib/api'
import type { Session } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Separator } from '@/components/ui/separator'
import {
  Dialog,
  DialogClose,
  DialogContent,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import { ErrorNotice, Loading, PageHeadingLevel, PageHeaderVisibility } from '@/components/common'
import { Login } from '@/components/login'
import type { PublicInfo } from '@/components/public-connection-fields'
import { PublicEntry } from '@/components/public-entry'
import { TrustedEntry } from '@/components/trusted-entry'

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
            <ArrowClockwiseIcon />
            Reload application
          </Button>
        </main>
      )
    return this.props.children
  }
}
type NavigationItem = {
  to: string
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
        to: '/indexers',
        label: 'Indexers',
        description: 'Manage your indexer connections.',
        icon: PlugsConnectedIcon,
        children: [
          {
            to: '/providers',
            label: 'Torrents',
            description: 'Torrent source connections and automatic imports.',
            icon: DownloadSimpleIcon,
          },
        ],
      },
      {
        to: '/remotes',
        label: 'Remote catalogs',
        description: 'Import read-only catalogs shared by other instances.',
        icon: CloudArrowDownIcon,
      },
      {
        to: '/torrents',
        label: 'Files',
        description: 'Search torrents imported from your sources.',
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
        to: '/metadata/videos',
        label: 'Videos',
        description: 'Film and series information, updates and history.',
        icon: FilmStripIcon,
      },
      {
        to: '/metadata/games',
        label: 'Games',
        description: 'Game information, updates and history.',
        icon: GameControllerIcon,
      },
      {
        to: '/metadata/books',
        label: 'Books',
        description: 'Book and audiobook information, updates and history.',
        icon: BookOpenIcon,
      },
      {
        to: '/metadata/identification',
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
        to: '/debrid',
        label: 'Debrid',
        description: 'Cache availability and playback providers.',
        icon: CloudCheckIcon,
      },
    ],
  },
  {
    id: 'search',
    label: 'Search',
    standalone: true,
    icon: MagnifyingGlassIcon,
    defaultOpen: true,
    items: [
      {
        to: '/search',
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
        to: '/torznab',
        label: 'Torznab',
        description: 'Connect external applications to your published catalog.',
        icon: BroadcastIcon,
      },
      {
        to: '/addon',
        label: 'Stremio',
        description: 'Connect your catalog to Stremio or AIOStreams.',
        icon: PlayCircleIcon,
      },
      {
        to: '/sharing',
        label: 'Shared catalogs',
        description: 'Share published catalogs with other instances.',
        icon: ShareNetworkIcon,
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
        to: '/security',
        label: 'Security',
        description: 'Encrypted secrets, sign-in protection and active sessions.',
        icon: ShieldCheckIcon,
        children: [
          {
            to: '/security/secrets',
            label: 'Secrets',
            description: 'Manage encrypted credentials.',
            icon: KeyIcon,
          },
          {
            to: '/security/settings',
            label: 'Settings',
            description: 'Configure sign-in limits and sessions.',
            icon: ShieldCheckIcon,
          },
          {
            to: '/security/sessions',
            label: 'Active sessions',
            description: 'Review and revoke signed-in devices.',
            icon: DevicesIcon,
          },
        ],
      },
      {
        to: '/network',
        label: 'Network',
        description: 'Incoming proxy trust and outgoing connections.',
        icon: GlobeIcon,
        children: [
          {
            to: '/network/incoming',
            label: 'Incoming',
            description: 'Recognize visitor IP addresses behind trusted proxies.',
            icon: ShieldCheckIcon,
          },
          {
            to: '/network/outgoing',
            label: 'Outgoing',
            description: 'Manage proxies and choose how services connect.',
            icon: GlobeIcon,
          },
        ],
      },
      {
        to: '/users',
        label: 'Users',
        description: 'Trusted user accounts and visitor access.',
        icon: UsersThreeIcon,
        children: [
          {
            to: '/trusted-users',
            label: 'Trusted',
            description: 'Manage user accounts, feature permissions and credentials.',
            icon: UsersThreeIcon,
          },
          {
            to: '/public-access',
            label: 'Public',
            description: 'Visitor access, source visibility and public connections.',
            icon: GlobeIcon,
          },
        ],
      },
      {
        to: '/health',
        label: 'Storage',
        description: 'PostgreSQL storage, local available space and data retention.',
        icon: DatabaseIcon,
      },
      {
        to: '/logs',
        label: 'Logs',
        description: 'Service activity and alerts.',
        icon: TerminalWindowIcon,
      },
    ],
  },
]
const publicSearchNavigation: NavigationGroup = {
  id: 'public-search',
  label: 'Search',
  standalone: true,
  icon: MagnifyingGlassIcon,
  defaultOpen: true,
  items: [
    {
      to: '/public/search',
      label: 'Search',
      description:
        'Find films, series, games and books and download through your own debrid accounts.',
      icon: MagnifyingGlassIcon,
    },
  ],
}
const publicNavigationGroups: NavigationGroup[] = [
  {
    id: 'integrations',
    label: 'Connections',
    icon: PlugsConnectedIcon,
    defaultOpen: true,
    items: [
      {
        to: '/public/torznab',
        label: 'Torznab',
        description: 'Connect a Torznab client with your own optional announce URLs.',
        icon: BroadcastIcon,
      },
      {
        to: '/public',
        label: 'Stremio',
        description: 'Connect Stremio or AIOStreams with your own debrid accounts.',
        icon: PlayCircleIcon,
      },
      {
        to: '/public/sharing',
        label: 'Shared catalogs',
        description: 'Keep a read-only catalog copy on another AIOSources instance.',
        icon: ShareNetworkIcon,
      },
    ],
  },
]
const navigationStorageKey = 'aiosources.navigation.groups'
const navigationLinkClassName =
  'flex min-h-(--control-height) items-center gap-3 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-[current=page]:bg-primary/10 aria-[current=page]:font-medium aria-[current=page]:text-primary'

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
      className="min-h-0 flex-1 space-y-1 overflow-y-auto overscroll-contain px-3 py-4 [scrollbar-width:thin]"
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
                  className="flex min-h-(--control-height) w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-expanded:text-foreground"
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
  useEffect(() => {
    async function expired() {
      await queryClient.cancelQueries()
      queryClient.setQueryData<Session>(['session'], { authenticated: false })
      queryClient.removeQueries({ predicate: (query) => query.queryKey[0] !== 'session' })
      queryClient.getMutationCache().clear()
    }
    window.addEventListener('session-expired', expired)
    return () => window.removeEventListener('session-expired', expired)
  }, [])
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
  if (location.pathname === '/user-login') return <Navigate to="/login" replace />
  if (session.data.role === 'trusted' && location.pathname === '/login') return <Login />
  if (session.data.role === 'trusted' && location.pathname !== '/login')
    return (
      <TrustedEntry
        layout={(content) => (
          <AppShell publicMode trustedMode>
            {content}
          </AppShell>
        )}
      />
    )
  if (
    location.pathname.startsWith('/trusted') &&
    !location.pathname.startsWith('/trusted-users') &&
    !session.data.authenticated
  )
    return <Login />
  if (location.pathname === '/public' || location.pathname.startsWith('/public/'))
    return (
      <PublicEntry
        layout={(content) => (
          <AppShell publicMode authenticated={session.data.authenticated}>
            {content}
          </AppShell>
        )}
      />
    )
  if (!session.data.authenticated)
    return location.pathname === '/login' ? (
      <Login />
    ) : (
      <PublicEntry layout={(content) => <AppShell publicMode>{content}</AppShell>} />
    )
  if (location.pathname === '/login') return <Navigate to="/" replace />
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
  const publicInfo = useQuery({
    queryKey: trustedMode ? ['trusted-config'] : ['public-config'],
    queryFn: ({ signal }) =>
      api<PublicInfo>(trustedMode ? '/trusted/config' : '/public/config', {
        signal,
        cache: 'no-store',
      }),
    enabled: publicMode,
    refetchInterval: LIVE_REFRESH_INTERVAL,
    retry: false,
  })
  const baseGroups = publicMode
    ? [
        ...(publicInfo.data?.search_enabled ? [publicSearchNavigation] : []),
        ...publicNavigationGroups
          .map((group) => ({
            ...group,
            items: group.items.filter((item) =>
              item.to === '/public'
                ? publicInfo.data?.stremio_enabled
                : item.to === '/public/torznab'
                  ? publicInfo.data?.torznab_enabled
                  : publicInfo.data?.catalog_enabled,
            ),
          }))
          .filter((group) => group.items.length > 0),
      ]
    : navigationGroups
  const groups = trustedMode
    ? baseGroups.map((group) => ({
        ...group,
        items: group.items.map((item) => ({
          ...item,
          to: item.to.replace(/^\/public(?=\/|$)/, '/trusted'),
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
  const location = useLocation()
  let navigationPath = publicMode
    ? location.pathname.startsWith('/public/search')
      ? '/public/search'
      : location.pathname === '/public/torznab'
        ? '/public/torznab'
        : location.pathname === '/public/sharing'
          ? '/public/sharing'
          : '/public'
    : ['/runs', '/raw', '/pages'].some(
          (path) => location.pathname === path || location.pathname.startsWith(path + '/'),
        ) || ['/', '/settings', '/schedules', '/import-settings'].includes(location.pathname)
      ? '/providers'
      : ['/notifications', '/webhooks'].includes(location.pathname)
        ? '/logs'
        : location.pathname === '/sharing/remotes'
          ? '/remotes'
          : location.pathname === '/secrets'
            ? '/security'
            : location.pathname === '/metadata' ||
                location.pathname === '/metadata/settings' ||
                location.pathname.startsWith('/metadata/history')
              ? '/metadata/videos'
              : location.pathname
  if (trustedMode)
    navigationPath = location.pathname.startsWith('/trusted/search')
      ? '/trusted/search'
      : location.pathname === '/trusted/torznab'
        ? '/trusted/torznab'
        : location.pathname === '/trusted/sharing'
          ? '/trusted/sharing'
          : '/trusted'
  const activeItem = [...items]
    .sort((a, b) => b.to.length - a.to.length)
    .find(
      (item) =>
        !item.children?.length &&
        (navigationPath === item.to ||
          (item.to !== '/' && navigationPath.startsWith(`${item.to}/`))),
    )
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
    const measure = () =>
      shell.style.setProperty('--app-header-height', `${header.getBoundingClientRect().height}px`)
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
    onSuccess: () => {
      setCSRF()
      window.dispatchEvent(new Event('session-expired'))
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
      <DatabaseIcon size={24} aria-hidden="true" className="shrink-0 text-primary" />
      <span className="text-2xl leading-none font-semibold tracking-tight">AIOSources</span>
    </>
  )
  const footer =
    publicMode && !trustedMode ? (
      <div className="shrink-0 space-y-2 p-3">
        <Button variant="outline" className="w-full" asChild>
          <Link to={authenticated ? '/providers' : '/login'}>
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
          <SignOutIcon />
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
        <aside className="fixed inset-y-0 left-0 z-30 hidden w-60 flex-col overflow-hidden border-r border-border bg-[#141719] lg:flex">
          <div className="flex h-20 shrink-0 items-center gap-3 px-6">{branding}</div>
          <Separator />
          <AppNavigation {...navigationProps} />
          {footer}
        </aside>
        <div className="lg:pl-60">
          <header
            ref={headerRef}
            className="sticky top-0 z-(--layer-header) flex min-h-16 flex-wrap items-center justify-between gap-3 border-b border-border bg-background/95 px-5 py-3 backdrop-blur-sm md:px-9 lg:pr-[calc(2.25rem+var(--header-actions-width)+0.75rem)]"
          >
            <div className="flex min-w-0 flex-1 items-center gap-3 pr-[calc(var(--header-actions-width)+0.75rem)] lg:pr-0">
              <DialogTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="lg:hidden"
                  aria-label="Open navigation"
                  aria-controls="mobile-navigation"
                >
                  <ListIcon />
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
          className="top-0 left-0 flex h-dvh max-h-dvh w-80 max-w-[calc(100%-2rem)] translate-x-0 translate-y-0 flex-col gap-0 overflow-hidden rounded-none border-y-0 border-l-0 bg-[#141719] p-0 data-[state=closed]:animate-none data-[state=open]:animate-none sm:max-w-80"
          onCloseAutoFocus={(event) => {
            if (focusMainOnClose.current) {
              event.preventDefault()
              focusMainOnClose.current = false
              mainRef.current?.focus({ preventScroll: true })
            }
          }}
        >
          <DialogTitle className="sr-only">Main navigation</DialogTitle>
          <div className="flex h-20 shrink-0 items-center gap-3 border-b border-border px-5">
            {branding}
            <DialogClose asChild>
              <Button
                variant="ghost"
                size="icon"
                className="ml-auto shrink-0"
                aria-label="Close navigation"
              >
                <XIcon />
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
