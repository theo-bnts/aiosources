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
import {
  DatabaseIcon,
  PlugsConnectedIcon,
  MagnifyingGlassIcon,
  ListMagnifyingGlassIcon,
  FilmSlateIcon,
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
import { PublicEntry } from '@/components/public-entry'

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
const navigationGroups = [
  {
    id: 'collection',
    label: 'Sources',
    icon: DownloadSimpleIcon,
    defaultOpen: true,
    items: [
      {
        to: '/providers',
        label: 'Sources',
        description: 'Source connections and automatic imports.',
        icon: PlugsConnectedIcon,
      },
      {
        to: '/remotes',
        label: 'Remote catalogs',
        description: 'Import read-only catalogs shared by other instances.',
        icon: CloudArrowDownIcon,
      },
      {
        to: '/torrents',
        label: 'Torrent search',
        description: 'Search torrents imported from your sources.',
        icon: ListMagnifyingGlassIcon,
      },
    ],
  },
  {
    id: 'catalog',
    label: 'Metadata',
    icon: FilmSlateIcon,
    defaultOpen: true,
    items: [
      {
        to: '/metadata',
        label: 'Metadata',
        description: 'Film and series information and torrent identification.',
        icon: FilmSlateIcon,
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
        to: '/addon',
        label: 'Stremio',
        description: 'Connect your catalog to Stremio or AIOStreams.',
        icon: PlayCircleIcon,
      },
      {
        to: '/torznab',
        label: 'Torznab',
        description: 'Connect external applications to your published catalog.',
        icon: BroadcastIcon,
      },
      {
        to: '/sharing',
        label: 'Shared catalogs',
        description: 'Share published catalogs with other instances.',
        icon: ShareNetworkIcon,
      },
      {
        to: '/public-access',
        label: 'Public access',
        description: 'Visitor access, source visibility and public connections.',
        icon: ShieldCheckIcon,
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
const publicNavigationGroups = [
  {
    id: 'integrations',
    label: 'Connections',
    icon: PlugsConnectedIcon,
    defaultOpen: true,
    items: [
      {
        to: '/public',
        label: 'Stremio',
        description: 'Connect Stremio or AIOStreams with your own debrid accounts.',
        icon: PlayCircleIcon,
      },
      {
        to: '/public/torznab',
        label: 'Torznab',
        description: 'Connect a Torznab client with your own optional announce URLs.',
        icon: BroadcastIcon,
      },
    ],
  },
]
const navigation = navigationGroups.flatMap((group) => group.items)
const navigationStorageKey = 'ingest.navigation.groups'
const navigationLinkClassName =
  'flex min-h-11 items-center gap-3 rounded-md px-3 py-2 text-sm text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-[current=page]:bg-primary/10 aria-[current=page]:font-medium aria-[current=page]:text-primary lg:min-h-9'

function AppNavigation({
  groups = navigationGroups,
  activeItem,
  activeGroupId,
  openGroups,
  onToggle,
  onNavigate,
}: {
  groups?: typeof navigationGroups
  activeItem: (typeof navigation)[number] | undefined
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
  function navigationLink(item: (typeof navigation)[number]) {
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
  return (
    <nav
      ref={navRef}
      aria-label="Main navigation"
      className="min-h-0 flex-1 space-y-1 overflow-y-auto overscroll-contain px-3 py-4 [scrollbar-width:thin]"
    >
      {groups.map((group) =>
        group.items.length === 1 ? (
          navigationLink(group.items[0])
        ) : (
          <section key={group.id} className={`space-y-1 ${openGroups[group.id] ? 'pb-2' : ''}`}>
            <h2>
              <button
                type="button"
                aria-expanded={openGroups[group.id]}
                aria-controls={`${id}-${group.id}`}
                onClick={() => onToggle(group.id)}
                className="flex min-h-11 w-full items-center gap-3 rounded-md px-3 py-2 text-left text-sm font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground aria-expanded:text-foreground lg:min-h-9"
              >
                <group.icon size={19} aria-hidden="true" className="shrink-0" />
                <span className="min-w-0 flex-1">{group.label}</span>
                <CaretDownIcon
                  aria-hidden="true"
                  className={`size-3.5 shrink-0 transition-transform motion-reduce:transition-none ${openGroups[group.id] ? '' : '-rotate-90'}`}
                />
              </button>
            </h2>
            <ul
              id={`${id}-${group.id}`}
              hidden={!openGroups[group.id]}
              className="ml-5 space-y-0.5 border-l border-border pl-2"
            >
              {group.items.map((item) => (
                <li key={item.to}>{navigationLink(item)}</li>
              ))}
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
  const [state, setState] = useState<'connecting' | 'live' | 'reconnecting'>('connecting')
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
            nextRefresh = performance.now() + LIVE_REFRESH_INTERVAL
            void refreshLiveData()
          },
          Math.max(500, nextRefresh - performance.now()),
        )
    }
    source.onopen = () => {
      setState('live')
      queueRefresh()
    }
    source.addEventListener('update', queueRefresh)
    source.onerror = () => {
      setState('reconnecting')
      void queryClient.invalidateQueries({ queryKey: ['session'] }, { cancelRefetch: false })
    }
    return () => {
      source.close()
      clearTimeout(timer)
    }
  }, [enabled])
  return state
}

export function AppShell({
  publicMode = false,
  authenticated = false,
  children,
}: {
  publicMode?: boolean
  authenticated?: boolean
  children?: ReactNode
}) {
  const groups = publicMode ? publicNavigationGroups : navigationGroups
  const items = groups.flatMap((group) => group.items)
  const storageKey = publicMode ? 'aiosources.public.navigation.groups' : navigationStorageKey
  const discard = useDiscardChanges('all')
  const live = useLiveUpdates(!publicMode)
  const location = useLocation()
  const navigationPath = publicMode
    ? location.pathname === '/public/torznab'
      ? '/public/torznab'
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
            : location.pathname
  const activeItem = [...items]
    .sort((a, b) => b.to.length - a.to.length)
    .find(
      (item) =>
        navigationPath === item.to || (item.to !== '/' && navigationPath.startsWith(`${item.to}/`)),
    )
  const activeGroup = groups.find((group) => group.items.some((item) => item === activeItem))
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>(() => {
    let stored: unknown
    try {
      stored = JSON.parse(localStorage.getItem(storageKey) ?? 'null')
    } catch {
      // Browser preferences must not prevent access to the console.
    }
    return Object.fromEntries(
      groups.map((group) => {
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
    )
  })
  const [mobileOpen, setMobileOpen] = useState(false)
  const mainRef = useRef<HTMLElement>(null)
  const previousPath = useRef(location.pathname)
  const focusMainOnClose = useRef(false)
  useEffect(() => {
    setMobileOpen(false)
    if (activeGroup) {
      setOpenGroups((current) =>
        current[activeGroup.id] ? current : { ...current, [activeGroup.id]: true },
      )
    }
    if (previousPath.current !== location.pathname) {
      if (!isPageTabNavigation(location.state)) {
        focusMainOnClose.current = true
        mainRef.current?.focus({ preventScroll: true })
        window.scrollTo({ top: 0 })
      }
      previousPath.current = location.pathname
    }
  }, [location.pathname, location.state, activeGroup])
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
    mutationFn: () => api<void>('/logout', { method: 'POST' }),
    onSuccess: () => {
      setCSRF()
      window.dispatchEvent(new Event('session-expired'))
    },
  })
  const navigationProps = {
    groups,
    activeItem,
    activeGroupId: activeGroup?.id,
    openGroups,
    onToggle: (groupId: string) =>
      setOpenGroups((current) => ({ ...current, [groupId]: !current[groupId] })),
  }
  const branding = (
    <>
      <DatabaseIcon size={32} aria-hidden="true" className="shrink-0 text-primary" />
      <span className="text-2xl leading-none font-semibold tracking-tight">AIOSources</span>
    </>
  )
  const footer = publicMode ? (
    <div className="shrink-0 border-t border-border p-3">
      <Button variant="outline" className="min-h-11 w-full lg:min-h-9" asChild>
        <Link to={authenticated ? '/providers' : '/login'}>
          {authenticated ? 'Administration' : 'Admin login'}
        </Link>
      </Button>
    </div>
  ) : (
    <div className="shrink-0 space-y-3 border-t border-border p-3">
      <ErrorNotice error={logout.error} scope="action" />
      <Button
        variant="warning"
        className="min-h-11 w-full justify-center lg:min-h-9"
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
      <div className="min-h-dvh">
        <a
          href="#main-content"
          className="fixed left-4 top-3 z-50 -translate-y-24 rounded-md bg-primary px-4 py-2 text-primary-foreground focus:translate-y-0"
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
          <header className="sticky top-0 z-20 flex min-h-16 items-center justify-between gap-3 border-b border-border bg-background/95 py-3 pr-20 pl-5 backdrop-blur-sm sm:pr-32 md:pr-36 md:pl-9">
            <div className="flex min-w-0 flex-1 items-center gap-3">
              <DialogTrigger asChild>
                <Button
                  variant="ghost"
                  size="icon"
                  className="size-11 lg:hidden"
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
                  {activeGroup && activeGroup.items.length > 1 && (
                    <li className="flex items-center gap-3">
                      {activeGroup.label}
                      <span aria-hidden="true" className="text-border">
                        /
                      </span>
                    </li>
                  )}
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
            <div className="flex shrink-0 items-center gap-3">
              <span
                className="hidden items-center gap-2 text-xs text-muted-foreground sm:flex"
                role="status"
              >
                <span
                  className={`size-1.5 rounded-full ${live === 'live' ? 'bg-primary' : 'bg-muted-foreground'}`}
                />
                {publicMode
                  ? 'Public access'
                  : live === 'live'
                    ? 'Live updates'
                    : live === 'connecting'
                      ? 'Connecting to updates'
                      : 'Reconnecting · refreshing every 30 s'}
              </span>
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
                className="ml-auto size-11 shrink-0"
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
