import {
  ArrowRightIcon,
  CloudArrowDownIcon,
  EraserIcon,
  ListChecksIcon,
  MagnifyingGlassIcon,
  WarningCircleIcon,
  XIcon,
} from '@phosphor-icons/react'
import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useId,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from 'react'

import { api, ApiError } from '@/lib/api'
import { messageText, number } from '@/lib/format'
import { useNotification } from '@/lib/notifications'
import {
  applyTorznabCapabilityField,
  applyTorznabCategories,
  applyUseAdvertisedCategories,
  countTorznabCategories,
  filterTorznabCategories,
  findUnknownCategoryIDs,
  isTorznabCapabilitiesStale,
  isTorznabTemplatePlaceholderURL,
  readSelectedCategoryIDs,
  readTorznabCapabilityFieldValue,
  resolveTorznabCapabilityFieldValue,
  torznabConnectionKey,
  type TorznabCapabilitiesResult,
  type TorznabCapabilityField,
  type TorznabCategoryCapability,
} from '@/lib/torznab-capabilities'
import { OptionalSection, useSourceEditor } from '@/components/source-editor-fields'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

type CapabilityTarget = TorznabCapabilityField | 'categories'

interface TorznabCapabilitiesContextValue {
  enabled: boolean
  capabilities: TorznabCapabilitiesResult | null
  busyTarget: CapabilityTarget | null
  error: unknown
  blocked: boolean
  fillField: (field: TorznabCapabilityField) => Promise<void>
  loadCategories: () => Promise<void>
  dismissError: () => void
  jumpToStage: (stage: 'identity' | 'connection' | 'pagination') => void
}

const TorznabCapabilitiesContext = createContext<TorznabCapabilitiesContextValue | null>(null)

export function useTorznabCapabilities() {
  const value = useContext(TorznabCapabilitiesContext)
  if (!value) throw new Error('Torznab capability controls require a TorznabCapabilitiesProvider.')
  return value
}

const missingFieldMessages: Record<TorznabCapabilityField, string> = {
  name: 'This server did not advertise a display name in its capabilities.',
  id: 'Could not derive a source identifier from capabilities.',
  url: 'This server did not return an endpoint URL.',
  page_size: 'This server did not advertise page-size limits in its capabilities.',
  search_mode: 'This server did not advertise supported search modes in its capabilities.',
}

export function TorznabCapabilitiesProvider({
  rawJSON,
  sourceID,
  blocked,
  onJumpToStage,
  children,
}: {
  rawJSON: string
  sourceID?: string
  blocked: boolean
  onJumpToStage: (stage: 'identity' | 'connection' | 'pagination') => void
  children: ReactNode
}) {
  const { document, change, readOnly } = useSourceEditor()
  const notify = useNotification()
  const isTorznab = document.get('adapter') === 'torznab'

  const [cache, setCache] = useState<{
    data: TorznabCapabilitiesResult
    key: string
  } | null>(null)
  const [busyTarget, setBusyTarget] = useState<CapabilityTarget | null>(null)
  const [error, setError] = useState<unknown>(null)

  const cacheRef = useRef(cache)
  cacheRef.current = cache
  const abortRef = useRef<AbortController | null>(null)
  const inflightRef = useRef<Promise<TorznabCapabilitiesResult | null> | null>(null)
  const requestCounter = useRef(0)

  const capabilities = useMemo(() => {
    if (!isTorznab || !cache) return null
    return isTorznabCapabilitiesStale(cache.key, document, cache.data) ? null : cache.data
  }, [isTorznab, cache, document])

  useEffect(() => {
    return () => {
      abortRef.current?.abort()
    }
  }, [])

  useEffect(() => {
    if (!isTorznab) {
      requestCounter.current += 1
      abortRef.current?.abort()
      abortRef.current = null
      inflightRef.current = null
      setCache(null)
      setBusyTarget(null)
      setError(null)
    }
  }, [isTorznab])

  const ensureCapabilities = useCallback(
    async (target: CapabilityTarget): Promise<TorznabCapabilitiesResult | null> => {
      if (!isTorznab || readOnly || blocked) return null
      const cached = cacheRef.current
      if (cached && !isTorznabCapabilitiesStale(cached.key, document, cached.data)) {
        setError(null)
        return cached.data
      }
      if (isTorznabTemplatePlaceholderURL(document.get('url'))) {
        onJumpToStage('connection')
        notify({
          message: 'Enter your Torznab server URL in Connection first.',
          warning: true,
        })
        return null
      }
      if (inflightRef.current) {
        return inflightRef.current
      }

      const requestId = requestCounter.current + 1
      requestCounter.current = requestId
      const connectionKey = torznabConnectionKey(document)
      abortRef.current?.abort()
      const controller = new AbortController()
      abortRef.current = controller
      setBusyTarget(target)
      setError(null)

      const promise = (async () => {
        try {
          const result = await api<TorznabCapabilitiesResult>('/providers/torznab-capabilities', {
            method: 'POST',
            signal: controller.signal,
            body: JSON.stringify({
              json: rawJSON,
              source_id: sourceID ?? '',
            }),
          })
          if (requestId !== requestCounter.current) return null
          const nextCache = { data: result, key: connectionKey }
          cacheRef.current = nextCache
          setCache(nextCache)
          setError(null)
          return result
        } catch (caught) {
          if (
            requestId === requestCounter.current &&
            !(caught instanceof Error && caught.name === 'AbortError')
          ) {
            setError(caught)
          }
          return null
        } finally {
          if (requestId === requestCounter.current) {
            abortRef.current = null
            inflightRef.current = null
            setBusyTarget(null)
          }
        }
      })()

      inflightRef.current = promise
      return promise
    },
    [isTorznab, readOnly, blocked, document, rawJSON, sourceID, onJumpToStage, notify],
  )

  const fillField = useCallback(
    async (field: TorznabCapabilityField) => {
      const caps = await ensureCapabilities(field)
      if (!caps) return
      const resolved = resolveTorznabCapabilityFieldValue(caps, field)
      if (!resolved) {
        notify({ message: missingFieldMessages[field], warning: true })
        return
      }
      const current = readTorznabCapabilityFieldValue(document, field)
      if (current === resolved.value) {
        notify(`${resolved.label} already matches capabilities.`)
        return
      }
      change((next) => applyTorznabCapabilityField(next, field, resolved.value))
      notify(`${resolved.label} updated from capabilities.`)
    },
    [ensureCapabilities, document, change, notify],
  )

  const loadCategories = useCallback(async () => {
    const caps = await ensureCapabilities('categories')
    if (!caps) return
    const total = countTorznabCategories(caps.categories)
    if (total === 0) {
      notify({
        message: 'This server did not advertise any categories in its capabilities.',
        warning: true,
      })
    } else {
      notify(
        `Loaded ${number(total)} ${total === 1 ? 'category' : 'categories'} from capabilities.`,
      )
    }
  }, [ensureCapabilities, notify])

  const dismissError = useCallback(() => setError(null), [])

  const value = useMemo<TorznabCapabilitiesContextValue>(
    () => ({
      enabled: isTorznab,
      capabilities,
      busyTarget,
      error,
      blocked,
      fillField,
      loadCategories,
      dismissError,
      jumpToStage: onJumpToStage,
    }),
    [
      isTorznab,
      capabilities,
      busyTarget,
      error,
      blocked,
      fillField,
      loadCategories,
      dismissError,
      onJumpToStage,
    ],
  )

  return (
    <TorznabCapabilitiesContext.Provider value={value}>
      {children}
    </TorznabCapabilitiesContext.Provider>
  )
}

export function TorznabCapabilitiesErrorNotice({ currentStage }: { currentStage: string }) {
  const { enabled, error, dismissError, jumpToStage } = useTorznabCapabilities()
  if (!enabled || !error) return null
  const code = error instanceof ApiError ? error.code : undefined
  const showConnectionLink =
    currentStage !== 'connection' &&
    (code === 'configuration' ||
      code === 'missing_secret' ||
      code === 'authentication' ||
      code === 'not_found')

  return (
    <div
      role="alert"
      className="rounded-md border border-destructive/40 bg-card p-4 text-card-foreground shadow-sm"
    >
      <div className="flex items-start gap-3">
        <WarningCircleIcon size={20} className="mt-0.5 shrink-0 text-destructive" />
        <div className="min-w-0 flex-1 space-y-3">
          <div className="space-y-1">
            <p className="text-sm font-medium">Could not load Torznab capabilities</p>
            <p className="break-words text-sm text-muted-foreground">
              {error instanceof Error
                ? messageText(error.message)
                : 'The capabilities check failed.'}
            </p>
          </div>
          <div className="flex flex-wrap gap-2">
            {showConnectionLink && (
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => jumpToStage('connection')}
              >
                <ArrowRightIcon aria-hidden="true" />
                Open Connection
              </Button>
            )}
            <Button type="button" size="sm" variant="ghost" onClick={dismissError}>
              <XIcon aria-hidden="true" />
              Dismiss
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}

export function TorznabFieldCapabilityButton({
  field,
  label,
}: {
  field: TorznabCapabilityField
  label: string
}) {
  const { readOnly } = useSourceEditor()
  const { enabled, busyTarget, blocked, fillField } = useTorznabCapabilities()
  if (!enabled) return null
  const loading = busyTarget === field
  return (
    <Button
      type="button"
      variant="outline"
      className="self-start shrink-0"
      aria-label={`Get ${label.toLowerCase()} from capabilities`}
      disabled={readOnly || blocked || busyTarget !== null}
      onClick={() => void fillField(field)}
    >
      <CloudArrowDownIcon aria-hidden="true" />
      {loading ? 'Loading…' : 'From capabilities'}
    </Button>
  )
}

export function TorznabCategoryPicker({ fallback }: { fallback: ReactNode }) {
  const { document, change, readOnly } = useSourceEditor()
  const { enabled, capabilities, busyTarget, blocked, loadCategories } = useTorznabCapabilities()
  const notify = useNotification()
  const filterId = useId()
  const [query, setQuery] = useState('')

  const selectedIDs = useMemo(() => readSelectedCategoryIDs(document), [document])
  const selectedSet = useMemo(() => new Set(selectedIDs), [selectedIDs])
  const categoryScope =
    typeof document.getIn(['options', 'category_scope']) === 'string' &&
    document.getIn(['options', 'category_scope'])
      ? String(document.getIn(['options', 'category_scope']))
      : 'combined'

  const categories = capabilities?.categories ?? []
  const totalCount = useMemo(() => countTorznabCategories(categories), [categories])
  const filtered = useMemo(() => filterTorznabCategories(categories, query), [categories, query])
  const unknownIDs = useMemo(
    () => findUnknownCategoryIDs(selectedIDs, categories),
    [selectedIDs, categories],
  )

  if (!enabled) return <>{fallback}</>

  const updateSelection = (nextIDs: number[]) => {
    let switched = false
    change((next) => {
      const outcome = applyTorznabCategories(next, nextIDs)
      switched = Boolean(outcome.switchedScope)
    })
    if (switched) {
      notify(
        'Category requests switched to Combine selected categories so this selection stays valid.',
      )
    }
  }

  const toggleID = (id: number) => {
    if (selectedSet.has(id)) {
      updateSelection(selectedIDs.filter((existing) => existing !== id))
    } else {
      updateSelection([...selectedIDs, id])
    }
  }

  if (!capabilities) {
    return (
      <div className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <p className="help">Load categories from capabilities to select them by name.</p>
          <Button
            type="button"
            variant="outline"
            disabled={readOnly || blocked || busyTarget !== null}
            onClick={() => void loadCategories()}
          >
            <CloudArrowDownIcon aria-hidden="true" />
            {busyTarget === 'categories' ? 'Loading…' : 'Load from capabilities'}
          </Button>
        </div>
        {fallback}
      </div>
    )
  }

  if (categories.length === 0) {
    return (
      <div className="space-y-3">
        <p className="help">
          This server did not advertise any categories. You can still configure numeric category IDs
          below.
        </p>
        {fallback}
      </div>
    )
  }

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Label htmlFor={filterId}>Categories</Label>
          <Badge variant={selectedIDs.length > 0 ? 'info' : 'secondary'}>
            {categoryScope === 'advertised'
              ? 'All advertised'
              : selectedIDs.length > 0
                ? `${selectedIDs.length} selected`
                : 'All categories'}
          </Badge>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {categoryScope !== 'advertised' && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={readOnly}
              onClick={() => {
                change((next) => applyUseAdvertisedCategories(next))
                notify('Category requests set to use all categories advertised by the source.')
              }}
            >
              <ListChecksIcon aria-hidden="true" />
              Use all advertised
            </Button>
          )}
          {selectedIDs.length > 0 && (
            <Button
              type="button"
              size="sm"
              variant="ghost"
              disabled={readOnly}
              onClick={() => updateSelection([])}
            >
              <EraserIcon aria-hidden="true" />
              Clear selection
            </Button>
          )}
        </div>
      </div>

      {categoryScope === 'advertised' && (
        <p className="help">
          Currently importing every category advertised by this source. Checking specific categories
          below switches Category requests to Combine selected categories.
        </p>
      )}

      {unknownIDs.length > 0 && (
        <div className="flex flex-wrap items-center gap-2 rounded-md border border-warning/30 bg-warning/10 px-3 py-2 text-xs">
          <span className="text-warning-foreground">Unlisted category IDs kept in this draft:</span>
          {unknownIDs.map((id) => (
            <Badge key={id} variant="outline" className="mono gap-1.5">
              {id}
              {!readOnly && (
                <button
                  type="button"
                  className="rounded-xs hover:text-destructive"
                  aria-label={`Remove category ${id}`}
                  onClick={() => toggleID(id)}
                >
                  <XIcon aria-hidden="true" className="size-3" />
                </button>
              )}
            </Badge>
          ))}
        </div>
      )}

      <div className="relative">
        <MagnifyingGlassIcon
          aria-hidden="true"
          className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground"
        />
        <Input
          id={filterId}
          type="search"
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder={`Filter ${number(totalCount)} categories by name or ID…`}
          className="pl-9"
        />
      </div>

      <div
        role="group"
        aria-label="Torznab advertised categories"
        className="max-h-72 overflow-y-auto rounded-md border border-border bg-background/50 divide-y divide-border"
      >
        {filtered.length === 0 ? (
          <div className="flex flex-wrap items-center justify-between gap-2 p-4 text-sm text-muted-foreground">
            <span>No categories match “{query}”.</span>
            <Button type="button" size="sm" variant="ghost" onClick={() => setQuery('')}>
              Clear filter
            </Button>
          </div>
        ) : (
          filtered.map((group) => (
            <div key={group.id} className="p-2">
              <label className="flex min-h-9 cursor-pointer items-center justify-between gap-3 rounded-sm px-2 py-1.5 hover:bg-muted/50">
                <span className="flex min-w-0 items-center gap-2.5">
                  <input
                    type="checkbox"
                    className="size-4 shrink-0 accent-primary"
                    checked={selectedSet.has(group.id)}
                    disabled={readOnly}
                    onChange={() => toggleID(group.id)}
                  />
                  <span className="break-words text-sm font-medium">{group.name}</span>
                </span>
                <span className="mono shrink-0 text-xs text-muted-foreground">{group.id}</span>
              </label>
              {group.subcategories && group.subcategories.length > 0 && (
                <div className="mt-1 grid gap-1 pl-6 sm:grid-cols-2">
                  {flattenDescendantCategories(group.subcategories).map((sub) => (
                    <label
                      key={sub.id}
                      className="flex min-h-9 cursor-pointer items-center justify-between gap-2 rounded-sm px-2 py-1 hover:bg-muted/50"
                    >
                      <span className="flex min-w-0 items-center gap-2.5">
                        <input
                          type="checkbox"
                          className="size-4 shrink-0 accent-primary"
                          checked={selectedSet.has(sub.id)}
                          disabled={readOnly}
                          onChange={() => toggleID(sub.id)}
                        />
                        <span className="break-words text-sm">{sub.label}</span>
                      </span>
                      <span className="mono shrink-0 text-xs text-muted-foreground">{sub.id}</span>
                    </label>
                  ))}
                </div>
              )}
            </div>
          ))
        )}
      </div>

      <OptionalSection
        title="Manual category IDs"
        description="Edit numeric Torznab category IDs directly, including custom IDs not listed by capabilities."
        configuredPaths={[['search', 'categories']]}
      >
        {fallback}
      </OptionalSection>
    </div>
  )
}

function flattenDescendantCategories(
  items: readonly TorznabCategoryCapability[],
  prefix = '',
): Array<{ id: number; label: string }> {
  const result: Array<{ id: number; label: string }> = []
  for (const item of items) {
    const label = prefix ? `${prefix} › ${item.name}` : item.name
    result.push({ id: item.id, label })
    if (item.subcategories?.length) {
      result.push(...flattenDescendantCategories(item.subcategories, label))
    }
  }
  return result
}
