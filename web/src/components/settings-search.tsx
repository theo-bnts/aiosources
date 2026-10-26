import { useEffect, useId, useMemo, useRef, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import { ArrowLeftIcon, ArrowRightIcon, MagnifyingGlassIcon } from '@phosphor-icons/react'
import { api } from '@/lib/api'
import { randomID } from '@/lib/random-id'
import { useDiscardChanges } from '@/lib/unsaved-changes'
import {
  createSettingsIndex,
  normalizeSettingQuery,
  searchSettings,
  settingURL,
  visibleSettings,
  type Setting,
  type SettingsEntity,
  type SettingsVisibility,
} from '@/lib/settings-catalog'
import type { ProviderSummary } from '@/lib/types'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { ErrorNotice, Loading } from '@/components/common'

type Entity = { id: string; name: string; adapter?: string }
const entityNames: Record<SettingsEntity, [string, string]> = {
  source: ['source', 'Add source'],
  remote: ['remote catalog', 'Add remote catalog'],
  stremio: ['Stremio access', 'Create access'],
  torznab: ['Torznab access', 'Create access'],
  share: ['shared catalog', 'Create share'],
  proxy: ['proxy', 'Add proxy'],
  tracker: ['source', ''],
  'source-route': ['source', ''],
}
async function loadEntities(entity: SettingsEntity, signal: AbortSignal): Promise<Entity[]> {
  if (['source', 'tracker', 'source-route'].includes(entity)) {
    const result = await api<{ items: ProviderSummary[] }>('/providers', { signal })
    return (result.items ?? []).filter((source) => !source.remote_catalog || entity !== 'source')
  }
  if (entity === 'proxy') {
    const result = await api<{ proxies: Entity[] }>('/settings/network', { signal })
    return result.proxies
  }
  const endpoint = {
    remote: '/remotes',
    stremio: '/stremio/connections',
    torznab: '/torznab/endpoints',
    share: '/shares',
  }[entity as 'remote' | 'stremio' | 'torznab' | 'share']
  const result = await api<{ items: Entity[] }>(endpoint, { signal })
  return result.items ?? []
}

export function SettingsSearch({ visibility }: { visibility: SettingsVisibility }) {
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [selected, setSelected] = useState<Setting | null>(null)
  const previousQuery = useRef('')
  const [active, setActive] = useState(0)
  const [shown, setShown] = useState(50)
  const input = useRef<HTMLInputElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  const results = useRef<HTMLDivElement>(null)
  const keyboardSelection = useRef(false)
  const navigating = useRef(false)
  const navigate = useNavigate()
  const location = useLocation()
  const discard = useDiscardChanges('all')
  const id = useId()
  // Only static labels enter the index. Typing never sends a network request.
  const index = useMemo(
    () => createSettingsIndex(visibleSettings(visibility)),
    [
      visibility.admin,
      visibility.search,
      visibility.stremio,
      visibility.torznab,
      visibility.sources,
      visibility.announces,
      visibility.services?.join(','),
    ],
  )
  const matches = useMemo(() => searchSettings(index, query), [index, query])
  const entities = useQuery({
    queryKey: ['settings-search-entities', selected?.entity],
    enabled: open && visibility.admin && Boolean(selected?.entity),
    queryFn: ({ signal }) => loadEntities(selected!.entity!, signal),
    staleTime: 0,
    gcTime: 0,
  })
  const choices = (entities.data ?? []).filter(
    (entity) =>
      (!selected?.adapters || (entity.adapter && selected.adapters.includes(entity.adapter))) &&
      normalizeSettingQuery(entity.name || entity.id).includes(normalizeSettingQuery(query)),
  )
  const rows = selected ? choices : matches
  const visible = rows.slice(0, shown)
  const canCreate =
    Boolean(selected?.entity && entityNames[selected.entity][1]) &&
    !entities.isPending &&
    !entities.isError &&
    !selected?.adapters
  const count = visible.length + (canCreate ? 1 : 0)
  function show() {
    navigating.current = false
    setQuery('')
    setSelected(null)
    setActive(0)
    setShown(50)
    setOpen(true)
  }
  useEffect(() => {
    const keyboard = (event: KeyboardEvent) => {
      if (
        (event.ctrlKey || event.metaKey) &&
        event.key.toLowerCase() === 'k' &&
        !event.altKey &&
        !event.repeat
      ) {
        event.preventDefault()
        event.stopPropagation()
        show()
      }
    }
    window.addEventListener('keydown', keyboard, true)
    return () => window.removeEventListener('keydown', keyboard, true)
  }, [])
  useEffect(() => {
    if (open) {
      navigating.current = true
      setOpen(false)
    }
    // Navigation, session changes and the Back button close stale search results.
  }, [location.key, visibility.admin])
  useEffect(() => {
    if (!open || !keyboardSelection.current) return
    document.getElementById(`${id}-result-${active}`)?.scrollIntoView({ block: 'nearest' })
  }, [active, open, id])
  useEffect(() => {
    if (results.current) results.current.scrollTop = 0
  }, [query, selected])
  function go(setting: Setting, entity?: string, create = false) {
    const target =
      setting.public === 'interface'
        ? `${location.pathname}${location.search}${settingURL(setting).slice(1)}`
        : settingURL(setting, entity, create)
    const resetSourceEditor =
      setting.entity === 'source' &&
      Boolean(document.querySelector('[data-settings-editor="source"][data-settings-draft="true"]'))
    const proceed = () => {
      navigating.current = true
      setOpen(false)
      const samePage = new URL(target, window.location.origin).pathname === location.pathname
      navigate(target, {
        state: {
          ...(samePage ? location.state : undefined),
          settingsNavigation: true,
          settingsNavigationId: randomID(),
          resetSourceEditor,
        },
      })
    }
    // Source drafts can explicitly reset on the same route. Connection editor
    // changes, including browser history, use the shared navigation guard.
    if (
      resetSourceEditor &&
      location.pathname === new URL(target, window.location.origin).pathname
    ) {
      // Return to the underlying editor if the user chooses Keep editing.
      navigating.current = true
      setOpen(false)
      discard(proceed)
    } else proceed()
  }
  function choose(index: number) {
    if (selected) {
      const entity = choices[index]
      if (entity) go(selected, entity.id)
      else if (index === visible.length && canCreate) go(selected, 'new', true)
    } else {
      const setting = matches[index]
      if (!setting) return
      if (setting.entity) {
        previousQuery.current = query
        setSelected(setting)
        setQuery('')
        setActive(0)
        setShown(50)
        input.current?.focus()
      } else go(setting)
    }
  }
  const activeID = count ? `${id}-result-${Math.min(active, count - 1)}` : undefined
  return (
    <>
      <Button
        ref={trigger}
        variant="outline"
        className="h-11 w-full min-w-0 justify-start gap-2 bg-background text-muted-foreground lg:h-9 lg:w-60 xl:w-72"
        onClick={show}
        aria-label="Search settings"
        aria-haspopup="dialog"
      >
        <MagnifyingGlassIcon className="shrink-0" />
        <span className="truncate">Search settings…</span>
        <kbd
          className="ml-auto hidden rounded border border-border px-1.5 text-[10px] lg:inline"
          aria-hidden="true"
        >
          Ctrl / ⌘ K
        </kbd>
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent
          data-settings-search
          className="max-h-[calc(100dvh-2rem)] grid-rows-[auto_auto_minmax(0,1fr)_auto] gap-0 overflow-hidden p-0 sm:max-w-2xl"
          onOpenAutoFocus={(event) => {
            event.preventDefault()
            input.current?.focus()
          }}
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            if (!navigating.current) trigger.current?.focus({ preventScroll: true })
          }}
        >
          <DialogHeader className="px-5 pt-5 pr-12 pb-4">
            <DialogTitle>
              {selected ? `Choose ${entityNames[selected.entity!][0]}` : 'Search settings'}
            </DialogTitle>
            <DialogDescription>
              {selected ? selected.title : 'Find a setting and open its exact location.'}
            </DialogDescription>
          </DialogHeader>
          <div className="flex items-center gap-2 border-y border-border px-4 py-3">
            {selected ? (
              <Button
                size="icon"
                variant="ghost"
                aria-label="Back to settings results"
                onClick={() => {
                  setSelected(null)
                  setQuery(previousQuery.current)
                  setActive(0)
                  input.current?.focus()
                }}
              >
                <ArrowLeftIcon />
              </Button>
            ) : (
              <MagnifyingGlassIcon className="shrink-0 text-muted-foreground" aria-hidden="true" />
            )}
            <Input
              ref={input}
              className="min-w-0 border-0 bg-transparent shadow-none focus-visible:ring-0"
              role="combobox"
              aria-label={selected ? `Find ${entityNames[selected.entity!][0]}` : 'Find a setting'}
              aria-expanded="true"
              aria-autocomplete="list"
              aria-controls={`${id}-results`}
              aria-activedescendant={activeID}
              placeholder={
                selected ? 'Filter by name…' : 'Search settings, e.g. cache, timeout, language…'
              }
              autoComplete="off"
              autoCapitalize="off"
              spellCheck={false}
              maxLength={160}
              value={query}
              onChange={(event) => {
                setQuery(event.target.value)
                setActive(0)
                setShown(50)
              }}
              onKeyDown={(event) => {
                if (event.nativeEvent.isComposing) return
                if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
                  event.preventDefault()
                  keyboardSelection.current = true
                  setActive((current) =>
                    count ? (current + (event.key === 'ArrowDown' ? 1 : count - 1)) % count : 0,
                  )
                } else if (event.key === 'Enter') {
                  event.preventDefault()
                  choose(Math.min(active, count - 1))
                }
              }}
            />
          </div>
          <div
            ref={results}
            className="max-h-[55dvh] min-h-0 overflow-y-auto overscroll-contain p-2"
          >
            {selected && (
              <>
                <ErrorNotice error={entities.error} retry={() => void entities.refetch()} />
                {entities.isPending && <Loading label="Loading available configurations" />}
              </>
            )}
            <div
              role="listbox"
              id={`${id}-results`}
              aria-label={selected ? 'Configurations' : 'Settings results'}
            >
              {visible.map((row, index) => {
                const setting = selected ?? (row as Setting)
                return (
                  <button
                    type="button"
                    role="option"
                    id={`${id}-result-${index}`}
                    key={row.id}
                    aria-selected={active === index}
                    tabIndex={-1}
                    className={`flex w-full min-w-0 items-center gap-3 rounded-md px-3 py-3 text-left text-sm ${active === index ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60'}`}
                    onPointerMove={(event) => {
                      if (event.pointerType === 'mouse') {
                        keyboardSelection.current = false
                        setActive(index)
                      }
                    }}
                    onMouseDown={(event) => event.preventDefault()}
                    onClick={() => choose(index)}
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block break-words font-medium">
                        {selected ? (row as Entity).name || row.id : setting.title}
                      </span>
                      <span className="mt-1 block break-words text-xs text-muted-foreground">
                        {setting.group}
                      </span>
                    </span>
                    <ArrowRightIcon
                      aria-hidden="true"
                      className="size-4 shrink-0 text-muted-foreground"
                    />
                  </button>
                )
              })}
              {canCreate && (
                <button
                  type="button"
                  role="option"
                  id={`${id}-result-${visible.length}`}
                  aria-selected={active === visible.length}
                  tabIndex={-1}
                  className={`w-full rounded-md px-3 py-3 text-left text-sm font-medium ${active === visible.length ? 'bg-accent' : 'hover:bg-accent/60'}`}
                  onMouseDown={(event) => event.preventDefault()}
                  onPointerMove={(event) => {
                    if (event.pointerType === 'mouse') {
                      keyboardSelection.current = false
                      setActive(visible.length)
                    }
                  }}
                  onClick={() => choose(visible.length)}
                >
                  {entityNames[selected!.entity!][1]}…
                </button>
              )}
            </div>
            {!selected && !normalizeSettingQuery(query) && (
              <p className="p-4 text-sm text-muted-foreground">
                Search by setting name or topic. Use ↑ ↓ and Enter to choose a result.
              </p>
            )}
            {!visible.length &&
              (selected
                ? !entities.isPending && !entities.isError
                : Boolean(normalizeSettingQuery(query))) && (
                <p role="status" className="p-4 text-sm text-muted-foreground">
                  {selected
                    ? 'No matching configurations.'
                    : 'No settings match. Try a shorter name or another keyword.'}
                </p>
              )}
            {rows.length > shown && (
              <Button
                className="mt-2 w-full"
                variant="ghost"
                onClick={() => setShown((value) => value + 50)}
              >
                Show more results
              </Button>
            )}
          </div>
          <p
            role="status"
            aria-live="polite"
            className="border-t border-border px-5 py-3 text-xs text-muted-foreground"
          >
            {selected ? `${choices.length} configurations` : `${matches.length} settings`}
            {rows.length > shown ? ` · ${shown} shown` : ''}
          </p>
        </DialogContent>
      </Dialog>
    </>
  )
}
