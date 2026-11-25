import {
  ArrowClockwiseIcon,
  ArrowRightIcon,
  CaretDownIcon,
  CaretUpIcon,
  CheckCircleIcon,
  CheckIcon,
  CloudArrowDownIcon,
  EraserIcon,
  ListChecksIcon,
  MagnifyingGlassIcon,
  PlugsConnectedIcon,
  WarningCircleIcon,
  XIcon,
} from '@phosphor-icons/react'
import { useMutation } from '@tanstack/react-query'
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
  applyTorznabCapabilityChanges,
  applyTorznabCapabilityField,
  applyTorznabCategories,
  applyUseAdvertisedCategories,
  countTorznabCategories,
  filterTorznabCategories,
  findUnknownCategoryIDs,
  formatTorznabSearchMode,
  isTorznabCapabilitiesStale,
  isTorznabTemplatePlaceholderURL,
  planTorznabCapabilityChanges,
  readSelectedCategoryIDs,
  torznabConnectionKey,
  type TorznabCapabilitiesResult,
  type TorznabCapabilityField,
  type TorznabCapabilityFieldChange,
  type TorznabCategoryCapability,
} from '@/lib/torznab-capabilities'
import { OptionalSection, useSourceEditor } from '@/components/source-editor-fields'
import { ConfirmAction } from '@/components/common'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface TorznabCapabilitiesContextValue {
  enabled: boolean
  isExistingSource: boolean
  capabilities: TorznabCapabilitiesResult | null
  changes: TorznabCapabilityFieldChange[]
  pending: boolean
  error: unknown
  stale: boolean
  blocked: boolean
  placeholderURL: boolean
  fetchCapabilities: () => void
  clearCapabilities: () => void
  dismissError: () => void
  applyField: (field: TorznabCapabilityField, value: string | number, label?: string) => void
  applyBulk: (includeOverwrites: boolean) => void
  jumpToStage: (stage: 'identity' | 'connection' | 'pagination') => void
  jumpToCategories: () => void
}

const TorznabCapabilitiesContext = createContext<TorznabCapabilitiesContextValue | null>(null)

export function useTorznabCapabilities() {
  const value = useContext(TorznabCapabilitiesContext)
  if (!value) throw new Error('Torznab capability controls require a TorznabCapabilitiesProvider.')
  return value
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
  const isExistingSource = Boolean(sourceID)
  const [capabilities, setCapabilities] = useState<TorznabCapabilitiesResult | null>(null)
  const [fetchedKey, setFetchedKey] = useState<string | null>(null)
  const [error, setError] = useState<unknown>(null)
  const requestCounter = useRef(0)
  const abortRef = useRef<AbortController | null>(null)
  const pendingScrollToCategories = useRef(false)

  const inspect = useMutation({
    mutationFn: async (variables: {
      json: string
      sourceId?: string
      connectionKey: string
      requestId: number
      signal: AbortSignal
    }) => {
      const result = await api<TorznabCapabilitiesResult>('/providers/torznab-capabilities', {
        method: 'POST',
        signal: variables.signal,
        body: JSON.stringify({
          json: variables.json,
          source_id: variables.sourceId ?? '',
        }),
      })
      return {
        result,
        connectionKey: variables.connectionKey,
        requestId: variables.requestId,
      }
    },
    onSuccess: ({ result, connectionKey, requestId }) => {
      if (requestId !== requestCounter.current) return
      abortRef.current = null
      setCapabilities(result)
      setFetchedKey(connectionKey)
      setError(null)
      const categories = countTorznabCategories(result.categories)
      notify(
        categories > 0
          ? `Loaded Torznab capabilities (${number(categories)} ${categories === 1 ? 'category' : 'categories'}). Review and apply what you need.`
          : 'Loaded Torznab capabilities. Review and apply what you need.',
      )
    },
    onError: (caught, variables) => {
      if (
        variables.requestId !== requestCounter.current ||
        (caught instanceof Error && caught.name === 'AbortError')
      ) {
        return
      }
      abortRef.current = null
      setError(caught)
    },
  })

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
      setCapabilities(null)
      setFetchedKey(null)
      setError(null)
    }
  }, [isTorznab])

  const fetchCapabilities = useCallback(() => {
    if (!isTorznab || readOnly || blocked || inspect.isPending) return
    if (isTorznabTemplatePlaceholderURL(document.get('url'))) {
      onJumpToStage('connection')
      notify({
        message:
          'Enter your Torznab server URL and authentication in Connection, then fetch capabilities.',
        warning: true,
      })
      return
    }
    const requestId = requestCounter.current + 1
    requestCounter.current = requestId
    abortRef.current?.abort()
    const controller = new AbortController()
    abortRef.current = controller
    setError(null)
    inspect.mutate({
      json: rawJSON,
      sourceId: sourceID,
      connectionKey: torznabConnectionKey(document),
      requestId,
      signal: controller.signal,
    })
  }, [isTorznab, readOnly, blocked, inspect, rawJSON, sourceID, document, onJumpToStage, notify])

  const clearCapabilities = useCallback(() => {
    requestCounter.current += 1
    abortRef.current?.abort()
    abortRef.current = null
    setCapabilities(null)
    setFetchedKey(null)
    setError(null)
  }, [])

  const dismissError = useCallback(() => {
    setError(null)
  }, [])

  const changes = useMemo(
    () =>
      capabilities
        ? planTorznabCapabilityChanges(document, capabilities, { isExistingSource })
        : [],
    [document, capabilities, isExistingSource],
  )

  const stale = useMemo(
    () => isTorznabCapabilitiesStale(fetchedKey, document, capabilities),
    [fetchedKey, document, capabilities],
  )

  const placeholderURL = useMemo(
    () => isTorznabTemplatePlaceholderURL(document.get('url')),
    [document],
  )

  const applyField = useCallback(
    (field: TorznabCapabilityField, value: string | number, label?: string) => {
      change((next) => applyTorznabCapabilityField(next, field, value))
      notify(label ? `${label} updated from t=caps.` : 'Setting updated from t=caps.')
    },
    [change, notify],
  )

  const applyBulk = useCallback(
    (includeOverwrites: boolean) => {
      if (!capabilities) return
      let appliedCount = 0
      change((next) => {
        const planned = planTorznabCapabilityChanges(next, capabilities, { isExistingSource })
        const applied = applyTorznabCapabilityChanges(next, planned, { includeOverwrites })
        appliedCount = applied.length
      })
      if (appliedCount > 0) {
        notify(
          `Applied ${appliedCount} ${appliedCount === 1 ? 'setting' : 'settings'} from t=caps. Save when ready.`,
        )
      }
    },
    [capabilities, change, isExistingSource, notify],
  )

  const jumpToCategories = useCallback(() => {
    pendingScrollToCategories.current = true
    onJumpToStage('connection')
    window.requestAnimationFrame(() => {
      window.requestAnimationFrame(() => {
        if (!pendingScrollToCategories.current) return
        pendingScrollToCategories.current = false
        const target = window.document.querySelector<HTMLElement>('[data-torznab-categories]')
        target?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      })
    })
  }, [onJumpToStage])

  const value = useMemo<TorznabCapabilitiesContextValue>(
    () => ({
      enabled: isTorznab,
      isExistingSource,
      capabilities,
      changes,
      pending: inspect.isPending,
      error,
      stale,
      blocked,
      placeholderURL,
      fetchCapabilities,
      clearCapabilities,
      dismissError,
      applyField,
      applyBulk,
      jumpToStage: onJumpToStage,
      jumpToCategories,
    }),
    [
      isTorznab,
      isExistingSource,
      capabilities,
      changes,
      inspect.isPending,
      error,
      stale,
      blocked,
      placeholderURL,
      fetchCapabilities,
      clearCapabilities,
      dismissError,
      applyField,
      applyBulk,
      onJumpToStage,
      jumpToCategories,
    ],
  )

  return (
    <TorznabCapabilitiesContext.Provider value={value}>
      {children}
    </TorznabCapabilitiesContext.Provider>
  )
}

export function TorznabCapabilitiesToolbarAction() {
  const { readOnly } = useSourceEditor()
  const { enabled, capabilities, pending, blocked, fetchCapabilities } = useTorznabCapabilities()
  if (!enabled || capabilities) return null
  return (
    <Button
      type="button"
      size="sm"
      variant="outline"
      disabled={readOnly || blocked || pending}
      onClick={fetchCapabilities}
    >
      <CloudArrowDownIcon aria-hidden="true" />
      {pending ? 'Fetching capabilities…' : 'Fetch capabilities'}
    </Button>
  )
}

export function TorznabCapabilitiesBanner({ currentStage }: { currentStage: string }) {
  const { document, readOnly } = useSourceEditor()
  const {
    enabled,
    capabilities,
    changes,
    pending,
    error,
    stale,
    blocked,
    fetchCapabilities,
    clearCapabilities,
    dismissError,
    applyField,
    applyBulk,
    jumpToStage,
    jumpToCategories,
  } = useTorznabCapabilities()
  const [expanded, setExpanded] = useState(true)
  const pendingChanges = changes.filter((item) => !item.matches)
  const safeChanges = pendingChanges.filter((item) => !item.overwrites)
  const overwriteChanges = pendingChanges.filter((item) => item.overwrites)

  useEffect(() => {
    if (!capabilities) return
    setExpanded(pendingChanges.length > 0)
  }, [capabilities])

  if (!enabled) return null

  const errorCode = error instanceof ApiError ? error.code : undefined
  const showConnectionLink =
    currentStage !== 'connection' &&
    (errorCode === 'configuration' ||
      errorCode === 'missing_secret' ||
      errorCode === 'authentication' ||
      errorCode === 'not_found')

  const categoryCount = capabilities ? countTorznabCategories(capabilities.categories) : 0
  const selectedCategories = readSelectedCategoryIDs(document)
  const categoryScope =
    typeof document.getIn(['options', 'category_scope']) === 'string'
      ? String(document.getIn(['options', 'category_scope']))
      : 'combined'
  const currentSearchMode =
    typeof document.getIn(['options', 'search_mode']) === 'string' &&
    document.getIn(['options', 'search_mode'])
      ? String(document.getIn(['options', 'search_mode']))
      : 'search'
  const rawPageSize = document.get('page_size')
  const currentPageSize =
    typeof rawPageSize === 'number' && Number.isInteger(rawPageSize) ? rawPageSize : undefined

  let discoveredHost = ''
  if (capabilities?.endpoint) {
    try {
      discoveredHost = new URL(capabilities.endpoint).host
    } catch {
      discoveredHost = capabilities.endpoint
    }
  }

  return (
    <div className="space-y-3">
      {error ? (
        <div
          role="alert"
          className="rounded-md border border-destructive/40 bg-card p-4 text-card-foreground shadow-sm"
        >
          <div className="flex items-start gap-3">
            <WarningCircleIcon size={20} className="mt-0.5 shrink-0 text-destructive" />
            <div className="min-w-0 flex-1 space-y-3">
              <div className="space-y-1">
                <p className="text-sm font-medium">Could not fetch Torznab capabilities (t=caps)</p>
                <p className="break-words text-sm text-muted-foreground">
                  {error instanceof Error
                    ? messageText(error.message)
                    : 'The capabilities check failed.'}
                </p>
              </div>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  disabled={readOnly || blocked || pending}
                  onClick={fetchCapabilities}
                >
                  <ArrowClockwiseIcon aria-hidden="true" />
                  Try again
                </Button>
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
      ) : null}

      {capabilities ? (
        <section
          aria-label="Discovered Torznab capabilities"
          className="overflow-hidden rounded-md border border-border bg-card"
        >
          <div className="flex flex-wrap items-center justify-between gap-3 p-4">
            <div className="min-w-0 flex-1 space-y-1.5">
              <div className="flex flex-wrap items-center gap-2">
                <PlugsConnectedIcon aria-hidden="true" className="size-4 shrink-0 text-primary" />
                <h3 className="break-words text-sm font-semibold">
                  {capabilities.title || discoveredHost || 'Torznab capabilities'}
                </h3>
                <Badge variant="success">t=caps loaded</Badge>
                {pendingChanges.length === 0 && <Badge variant="outline">All settings match</Badge>}
                {stale && <Badge variant="warning">Connection changed</Badge>}
              </div>
              <div className="flex flex-wrap items-center gap-1.5">
                <Badge variant="secondary">
                  {number(categoryCount)} {categoryCount === 1 ? 'category' : 'categories'}
                </Badge>
                {capabilities.limits.max || capabilities.limits.default ? (
                  <Badge variant="outline">
                    Limit{' '}
                    {capabilities.limits.max
                      ? `max ${number(capabilities.limits.max)}`
                      : `default ${number(capabilities.limits.default ?? 0)}`}
                  </Badge>
                ) : null}
                {capabilities.rate_limit &&
                capabilities.rate_limit.remaining !== undefined &&
                capabilities.rate_limit.limit !== undefined ? (
                  <Badge variant="info">
                    Quota {number(capabilities.rate_limit.remaining)} /{' '}
                    {number(capabilities.rate_limit.limit)}
                  </Badge>
                ) : null}
              </div>
            </div>
            <div className="flex flex-wrap items-center gap-2">
              {!expanded && categoryCount > 0 && currentStage !== 'connection' && (
                <Button type="button" size="sm" variant="outline" onClick={jumpToCategories}>
                  <ListChecksIcon aria-hidden="true" />
                  Categories
                </Button>
              )}
              <Button
                type="button"
                size="sm"
                variant="ghost"
                aria-expanded={expanded}
                onClick={() => setExpanded((current) => !current)}
              >
                {expanded ? (
                  <>
                    <CaretUpIcon aria-hidden="true" />
                    Hide details
                  </>
                ) : (
                  <>
                    <CaretDownIcon aria-hidden="true" />
                    Show details
                  </>
                )}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="outline"
                disabled={readOnly || blocked || pending}
                onClick={fetchCapabilities}
              >
                <ArrowClockwiseIcon
                  aria-hidden="true"
                  className={pending ? 'animate-spin' : undefined}
                />
                {pending ? 'Refreshing…' : 'Refresh'}
              </Button>
              <Button
                type="button"
                size="icon-sm"
                variant="ghost"
                aria-label="Dismiss discovered capabilities"
                onClick={clearCapabilities}
              >
                <XIcon aria-hidden="true" />
              </Button>
            </div>
          </div>

          {expanded && (
            <>
              <div className="flex flex-wrap items-center justify-between gap-3 border-t border-border bg-muted/20 px-4 py-3">
                <p className="min-w-0 flex-1 text-sm text-muted-foreground">
                  {pendingChanges.length === 0 ? (
                    <span className="inline-flex items-center gap-1.5 text-foreground">
                      <CheckCircleIcon
                        aria-hidden="true"
                        className="size-4 shrink-0 text-emerald-600 dark:text-emerald-400"
                      />
                      All discovered settings match this draft.
                    </span>
                  ) : overwriteChanges.length === 0 ? (
                    <>
                      {safeChanges.length}{' '}
                      {safeChanges.length === 1 ? 'setting is' : 'settings are'} ready to fill from{' '}
                      <code className="mono">t=caps</code>.
                    </>
                  ) : safeChanges.length > 0 ? (
                    <>
                      {safeChanges.length} empty/default{' '}
                      {safeChanges.length === 1 ? 'field' : 'fields'} can be filled safely;{' '}
                      {overwriteChanges.length} customized{' '}
                      {overwriteChanges.length === 1 ? 'field differs' : 'fields differ'}.
                    </>
                  ) : (
                    <>
                      {overwriteChanges.length} discovered{' '}
                      {overwriteChanges.length === 1 ? 'setting differs' : 'settings differ'} from
                      your current configuration. Apply individual fields below or overwrite all.
                    </>
                  )}
                </p>
                <div className="flex flex-wrap items-center gap-2">
                  {safeChanges.length > 0 && overwriteChanges.length === 0 && (
                    <Button
                      type="button"
                      size="sm"
                      disabled={readOnly}
                      onClick={() => {
                        applyBulk(false)
                        setExpanded(false)
                      }}
                    >
                      <CheckIcon aria-hidden="true" />
                      Apply discovered settings ({safeChanges.length})
                    </Button>
                  )}
                  {safeChanges.length > 0 && overwriteChanges.length > 0 && (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      disabled={readOnly}
                      onClick={() => applyBulk(false)}
                    >
                      <CheckIcon aria-hidden="true" />
                      Fill missing defaults ({safeChanges.length})
                    </Button>
                  )}
                  {overwriteChanges.length > 0 && (
                    <ConfirmAction
                      title="Overwrite existing source settings?"
                      description={
                        <div className="space-y-3">
                          <p>
                            Applying all discovered settings will replace the following customized
                            values in this draft:
                          </p>
                          <ul className="list-disc space-y-1.5 pl-5 text-foreground">
                            {overwriteChanges.map((item) => (
                              <li key={item.field} className="break-words">
                                <span className="font-medium">{item.label}:</span>{' '}
                                <span className="text-muted-foreground">{item.currentDisplay}</span>{' '}
                                → <span className="font-medium">{item.nextDisplay}</span>
                              </li>
                            ))}
                          </ul>
                          {safeChanges.length > 0 && (
                            <p>
                              It will also fill {safeChanges.length} empty/default{' '}
                              {safeChanges.length === 1 ? 'field' : 'fields'}.
                            </p>
                          )}
                          <p>
                            Only this draft is updated; your saved source does not change until you
                            click Save. Categories are edited separately.
                          </p>
                        </div>
                      }
                      label="Overwrite settings"
                      variant="warning"
                      trigger={
                        <Button type="button" size="sm" variant="warning" disabled={readOnly}>
                          {safeChanges.length > 0
                            ? `Apply all (overwrites ${overwriteChanges.length})`
                            : `Overwrite all (${overwriteChanges.length})`}
                        </Button>
                      }
                      action={async () => {
                        applyBulk(true)
                        setExpanded(false)
                      }}
                    />
                  )}
                  {categoryCount > 0 && (
                    <Button type="button" size="sm" variant="outline" onClick={jumpToCategories}>
                      <ListChecksIcon aria-hidden="true" />
                      {selectedCategories.length > 0
                        ? `Categories (${selectedCategories.length} selected)`
                        : 'Browse categories'}
                    </Button>
                  )}
                </div>
              </div>

              <div className="divide-y divide-border border-t border-border">
                {changes.map((item) => (
                  <div
                    key={item.field}
                    className="flex flex-wrap items-center justify-between gap-3 px-4 py-3"
                  >
                    <div className="min-w-0 flex-1 space-y-1">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-medium">{item.label}</span>
                        {item.matches ? (
                          <Badge variant="success">Matches</Badge>
                        ) : item.overwrites ? (
                          <Badge variant="warning">Differs</Badge>
                        ) : (
                          <Badge variant="info">Ready</Badge>
                        )}
                      </div>
                      <p className="break-words text-xs text-muted-foreground">
                        {item.matches ? (
                          <span
                            className={item.field === 'url' || item.field === 'id' ? 'mono' : ''}
                          >
                            {item.nextDisplay}
                          </span>
                        ) : (
                          <>
                            Current:{' '}
                            <span
                              className={
                                item.field === 'url' || item.field === 'id' ? 'mono break-all' : ''
                              }
                            >
                              {item.currentDisplay}
                            </span>{' '}
                            → Discovered:{' '}
                            <span
                              className={`font-medium text-foreground ${item.field === 'url' || item.field === 'id' ? 'mono break-all' : ''}`}
                            >
                              {item.nextDisplay}
                            </span>
                          </>
                        )}
                      </p>
                    </div>
                    <div className="flex shrink-0 flex-wrap items-center gap-2">
                      {!item.matches && (
                        <Button
                          type="button"
                          size="sm"
                          variant={item.overwrites ? 'outline' : 'secondary'}
                          disabled={readOnly}
                          aria-label={`${item.overwrites ? 'Overwrite' : 'Apply'} ${item.label}`}
                          onClick={() => applyField(item.field, item.nextValue, item.label)}
                        >
                          <CheckIcon aria-hidden="true" />
                          {item.overwrites ? 'Overwrite' : 'Apply'}
                        </Button>
                      )}
                      {item.field === 'page_size' &&
                        (() => {
                          const effectiveDefault = capabilities.limits.default
                            ? Math.min(capabilities.limits.default, 10000)
                            : 0
                          const effectiveMax = capabilities.limits.max
                            ? Math.min(capabilities.limits.max, 10000)
                            : 0
                          if (
                            !effectiveDefault ||
                            !effectiveMax ||
                            effectiveDefault === effectiveMax ||
                            currentPageSize === effectiveDefault
                          ) {
                            return null
                          }
                          return (
                            <Button
                              type="button"
                              size="sm"
                              variant="ghost"
                              disabled={readOnly}
                              onClick={() => applyField('page_size', effectiveDefault, 'Page size')}
                            >
                              Use default ({effectiveDefault})
                            </Button>
                          )
                        })()}
                    </div>
                  </div>
                ))}

                {capabilities.searches.length > 0 && (
                  <div className="space-y-2 px-4 py-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <span className="text-sm font-medium">Supported search modes</span>
                      <span className="text-xs text-muted-foreground">
                        Click an available mode to use it
                      </span>
                    </div>
                    <div className="flex flex-wrap items-center gap-2">
                      {capabilities.searches.map((mode) => {
                        const active = currentSearchMode === mode.mode
                        if (!mode.available) {
                          return (
                            <Badge key={mode.mode} variant="outline" className="py-1 opacity-70">
                              {formatTorznabSearchMode(mode.mode)} · unavailable
                            </Badge>
                          )
                        }
                        return (
                          <Button
                            key={mode.mode}
                            type="button"
                            size="sm"
                            variant={active ? 'secondary' : 'outline'}
                            aria-pressed={active}
                            disabled={readOnly}
                            onClick={() => applyField('search_mode', mode.mode, 'Search mode')}
                          >
                            {active && <CheckIcon aria-hidden="true" />}
                            {formatTorznabSearchMode(mode.mode)}
                          </Button>
                        )
                      })}
                    </div>
                  </div>
                )}

                <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3">
                  <div className="min-w-0 flex-1 space-y-0.5">
                    <span className="text-sm font-medium">Categories</span>
                    <p className="text-xs text-muted-foreground">
                      {categoryCount === 0
                        ? 'This server did not advertise categories in t=caps.'
                        : categoryScope === 'advertised'
                          ? `${number(categoryCount)} advertised categories · importing all advertised categories.`
                          : selectedCategories.length > 0
                            ? `${number(categoryCount)} advertised categories · ${selectedCategories.length} selected (${categoryScope === 'each' ? 'separate requests' : 'combined'}).`
                            : `${number(categoryCount)} advertised categories · no category filter applied (all categories).`}
                    </p>
                  </div>
                  {categoryCount > 0 && (
                    <Button type="button" size="sm" variant="outline" onClick={jumpToCategories}>
                      <ArrowRightIcon aria-hidden="true" />
                      Manage categories
                    </Button>
                  )}
                </div>
              </div>
            </>
          )}
        </section>
      ) : null}
    </div>
  )
}

export function TorznabIdentityIntro() {
  const { enabled, isExistingSource, capabilities, jumpToStage } = useTorznabCapabilities()
  if (!enabled || isExistingSource || capabilities) return null
  return (
    <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/25 p-4">
      <div className="min-w-0 flex-1 space-y-1">
        <p className="text-sm font-medium">Auto-configure from Torznab capabilities (t=caps)</p>
        <p className="text-xs leading-5 text-muted-foreground">
          Enter your Torznab URL and credentials in Connection to detect the display name, source
          identifier, endpoint URL, page size, search modes, and categories automatically.
        </p>
      </div>
      <Button type="button" size="sm" variant="outline" onClick={() => jumpToStage('connection')}>
        <ArrowRightIcon aria-hidden="true" />
        Open Connection
      </Button>
    </div>
  )
}

export function TorznabFieldHint({
  field,
  buttonLabel,
}: {
  field: 'name' | 'id' | 'url'
  buttonLabel: string
}) {
  const { readOnly } = useSourceEditor()
  const { enabled, capabilities, changes, applyField } = useTorznabCapabilities()
  if (!enabled || !capabilities) return null
  const change = changes.find((item) => item.field === field)
  if (!change || change.matches) return null
  return (
    <div className="mt-2 flex flex-wrap items-center justify-between gap-2 rounded-md border border-border bg-muted/25 px-3 py-2">
      <p className="min-w-0 flex-1 break-words text-xs text-muted-foreground">
        From <code className="mono">t=caps</code>:{' '}
        <span
          className={`font-medium text-foreground ${field === 'url' || field === 'id' ? 'mono break-all' : ''}`}
        >
          {change.nextDisplay}
        </span>
      </p>
      <Button
        type="button"
        size="sm"
        variant="outline"
        disabled={readOnly}
        onClick={() => applyField(change.field, change.nextValue, change.label)}
      >
        <CheckIcon aria-hidden="true" />
        {buttonLabel}
      </Button>
    </div>
  )
}

export function TorznabConnectionDiscoveryBox() {
  const { readOnly } = useSourceEditor()
  const { enabled, capabilities, pending, blocked, placeholderURL, fetchCapabilities } =
    useTorznabCapabilities()
  if (!enabled || capabilities) return null
  return (
    <section
      data-settings-section="Torznab capabilities"
      className="space-y-3 rounded-md border border-border bg-muted/20 p-4"
    >
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 flex-1 space-y-1">
          <h3 className="text-sm font-medium">Discover capabilities (t=caps)</h3>
          <p className="text-xs leading-5 text-muted-foreground">
            Read the server’s advertised title, endpoint, page-size limits, search modes, and
            categories using the URL and authentication above. Nothing in this draft is modified
            until you apply it.
          </p>
        </div>
        <Button
          type="button"
          variant="secondary"
          disabled={readOnly || blocked || pending || placeholderURL}
          onClick={fetchCapabilities}
        >
          <CloudArrowDownIcon aria-hidden="true" />
          {pending ? 'Fetching capabilities…' : 'Fetch capabilities (t=caps)'}
        </Button>
      </div>
      {placeholderURL && (
        <p className="text-xs text-muted-foreground">
          Enter your Torznab server URL in Endpoint above to enable capability discovery.
        </p>
      )}
    </section>
  )
}

export function TorznabSearchModeHints() {
  const { document, readOnly } = useSourceEditor()
  const { enabled, capabilities, applyField } = useTorznabCapabilities()
  if (!enabled || !capabilities || capabilities.searches.length === 0) return null
  const currentMode =
    typeof document.getIn(['options', 'search_mode']) === 'string' &&
    document.getIn(['options', 'search_mode'])
      ? String(document.getIn(['options', 'search_mode']))
      : 'search'
  const currentCap = capabilities.searches.find((item) => item.mode === currentMode)
  const recommended = capabilities.recommended_search_mode

  return (
    <div className="space-y-2 rounded-md border border-border bg-muted/20 p-3">
      {currentCap && !currentCap.available && (
        <div className="flex flex-wrap items-center justify-between gap-2 rounded-md border border-warning/40 bg-warning/10 px-3 py-2 text-xs">
          <p className="min-w-0 flex-1 text-warning-foreground">
            This server advertises{' '}
            <span className="font-medium">{formatTorznabSearchMode(currentMode)}</span> as
            unavailable.
          </p>
          {recommended && recommended !== currentMode && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={readOnly}
              onClick={() => applyField('search_mode', recommended, 'Search mode')}
            >
              <CheckIcon aria-hidden="true" />
              Use {formatTorznabSearchMode(recommended)}
            </Button>
          )}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-xs text-muted-foreground">Advertised modes:</span>
        {capabilities.searches.map((item) => (
          <Badge
            key={item.mode}
            variant={
              !item.available ? 'outline' : item.mode === currentMode ? 'success' : 'secondary'
            }
          >
            {formatTorznabSearchMode(item.mode)}
            {item.available ? '' : ' (unavailable)'}
          </Badge>
        ))}
      </div>
      {currentCap?.available && currentCap.supported_params.length > 0 && (
        <p className="break-words text-xs text-muted-foreground">
          Supported parameters for {formatTorznabSearchMode(currentCap.mode)}:{' '}
          <span className="mono">{currentCap.supported_params.join(', ')}</span>
        </p>
      )}
    </div>
  )
}

export function TorznabCategoryPicker({ fallback }: { fallback: ReactNode }) {
  const { document, change, readOnly } = useSourceEditor()
  const { enabled, capabilities, pending, blocked, placeholderURL, fetchCapabilities } =
    useTorznabCapabilities()
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
      <div data-torznab-categories className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border bg-muted/20 p-3">
          <p className="min-w-0 flex-1 text-xs leading-5 text-muted-foreground">
            Fetch <code className="mono">t=caps</code> to browse and filter this source’s categories
            by name instead of entering numeric IDs manually.
          </p>
          <Button
            type="button"
            size="sm"
            variant="outline"
            disabled={readOnly || blocked || pending || placeholderURL}
            onClick={fetchCapabilities}
          >
            <CloudArrowDownIcon aria-hidden="true" />
            {pending ? 'Loading categories…' : 'Fetch categories (t=caps)'}
          </Button>
        </div>
        {fallback}
      </div>
    )
  }

  if (categories.length === 0) {
    return (
      <div data-torznab-categories className="space-y-3">
        <p className="text-xs text-muted-foreground">
          This server did not advertise any categories in <code className="mono">t=caps</code>. You
          can still configure numeric category IDs below.
        </p>
        {fallback}
      </div>
    )
  }

  return (
    <div data-torznab-categories className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <Label htmlFor={filterId}>Advertised categories</Label>
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
        <p className="rounded-md border border-border bg-muted/25 px-3 py-2 text-xs text-muted-foreground">
          Currently configured to import every category advertised by this source. Checking specific
          categories below switches Category requests to Combine selected categories.
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
        description="Edit numeric Torznab category IDs directly, including custom IDs not advertised in t=caps."
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

export function TorznabPageSizeHint() {
  const { document, readOnly } = useSourceEditor()
  const { enabled, capabilities, applyField } = useTorznabCapabilities()
  if (!enabled || !capabilities) return null
  const { default: defaultLimit, max: maxLimit } = capabilities.limits
  if (!defaultLimit && !maxLimit) return null
  const raw = document.get('page_size')
  const current = typeof raw === 'number' && Number.isInteger(raw) ? raw : undefined
  const exceedsMax = Boolean(maxLimit && current && current > maxLimit)
  const effectiveMax = maxLimit ? Math.min(maxLimit, 10000) : 0
  const effectiveDefault = defaultLimit ? Math.min(defaultLimit, 10000) : 0

  return (
    <div
      className={`space-y-2 rounded-md border px-3 py-2.5 text-xs ${
        exceedsMax ? 'border-warning/40 bg-warning/10' : 'border-border bg-muted/25'
      }`}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="min-w-0 flex-1 space-y-0.5">
          <p className="text-muted-foreground">
            Advertised by <code className="mono">t=caps</code>:{' '}
            {maxLimit ? (
              <>
                max <span className="mono font-medium text-foreground">{number(maxLimit)}</span>
              </>
            ) : null}
            {maxLimit && defaultLimit ? ' · ' : null}
            {defaultLimit ? (
              <>
                default{' '}
                <span className="mono font-medium text-foreground">{number(defaultLimit)}</span>
              </>
            ) : null}
          </p>
          {exceedsMax && (
            <p className="text-warning-foreground">
              Current page size ({current}) exceeds this server’s advertised maximum ({maxLimit}).
            </p>
          )}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {effectiveMax > 0 && current !== effectiveMax && (
            <Button
              type="button"
              size="sm"
              variant="outline"
              disabled={readOnly}
              onClick={() => applyField('page_size', effectiveMax, 'Page size')}
            >
              <CheckIcon aria-hidden="true" />
              Use max ({effectiveMax})
            </Button>
          )}
          {effectiveDefault > 0 &&
            effectiveDefault !== effectiveMax &&
            current !== effectiveDefault && (
              <Button
                type="button"
                size="sm"
                variant="ghost"
                disabled={readOnly}
                onClick={() => applyField('page_size', effectiveDefault, 'Page size')}
              >
                Use default ({effectiveDefault})
              </Button>
            )}
        </div>
      </div>
    </div>
  )
}
