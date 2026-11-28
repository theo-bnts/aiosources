import { BellIcon, FloppyDiskIcon, GearSixIcon, TrashIcon } from '@phosphor-icons/react'
import { useEffect, useId, useMemo, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'

import { api, queryClient } from '@/lib/api'
import { catalogCardSummary } from '@/lib/catalog-cards'
import type { CatalogTitle } from '@/lib/catalog-search-types'
import {
  browserLanguage,
  resolvedLanguage,
  type LanguagePreferences,
} from '@/lib/language-preferences'
import {
  formatAlertEpisode,
  formatAlertSizeInput,
  hasActiveAlertFilters,
  notificationChannelTypeLabel,
  parseAlertSizeInput,
  type NotificationChannel,
  type NotificationChannelSummary,
  type ReleaseAlert,
  type ReleaseAlertFilters,
} from '@/lib/notification-types'
import { useNotification } from '@/lib/notifications'
import { routes } from '@/lib/routes'
import type { Items, ProviderSummary } from '@/lib/types'
import { CatalogCover } from '@/components/catalog-cover'
import { ConfirmAction, ErrorNotice, Loading } from '@/components/common'
import { LanguageFilter } from '@/components/language-filter'
import { SourceMultiSelect } from '@/components/source-multi-select'
import { Button } from '@/components/ui/button'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'

export type ReleaseAlertTarget =
  | {
      mode: 'create'
      work: CatalogTitle
      season?: number
      episode?: number
      unavailableNotice?: boolean
    }
  | {
      mode: 'edit'
      alert: ReleaseAlert
    }

function initialLanguagePrefs(filters?: ReleaseAlertFilters): LanguagePreferences {
  if (!filters?.language_mode || filters.language_mode === 'all') {
    return { language: '', mode: 'all' }
  }
  return {
    language: filters.language ?? '',
    mode: filters.language_mode,
  }
}

export function ReleaseAlertDialog({
  open,
  onOpenChange,
  target,
  trusted = false,
  trustedSources,
  existingAlerts,
  onSaved,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
  target: ReleaseAlertTarget | null
  trusted?: boolean
  trustedSources?: { id: string; name: string }[]
  existingAlerts?: ReleaseAlert[]
  onSaved?: () => void
}) {
  const idPrefix = useId()
  const navigate = useNavigate()
  const notify = useNotification()
  const channelsKey = trusted ? ['trusted-notification-channels'] : ['notification-channels']
  const alertsKey = trusted ? ['trusted-alerts'] : ['alerts']

  const channelsQuery = useQuery({
    queryKey: channelsKey,
    enabled: open,
    queryFn: ({ signal }) =>
      api<Items<NotificationChannel | NotificationChannelSummary>>(
        trusted ? '/trusted/notifications/channels' : '/notifications/channels',
        { signal },
      ),
  })

  const sourcesQuery = useQuery({
    queryKey: ['providers'],
    enabled: open && !trusted,
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })

  const availableChannels = useMemo(() => {
    const raw = channelsQuery.data?.items ?? []
    return raw.filter((ch) => ('enabled' in ch ? ch.enabled : true))
  }, [channelsQuery.data])

  const defaultChannelID = useMemo(() => {
    const def = availableChannels.find((ch) => ch.is_default)
    return def?.id ?? availableChannels[0]?.id ?? ''
  }, [availableChannels])

  const sourceOptions = useMemo(
    () =>
      trusted
        ? (trustedSources ?? []).map((s) => ({ value: s.id, label: s.name }))
        : (sourcesQuery.data?.items ?? []).map((s) => ({ value: s.id, label: s.name || s.id })),
    [trusted, trustedSources, sourcesQuery.data],
  )

  const [seasonText, setSeasonText] = useState('1')
  const [episodeText, setEpisodeText] = useState('1')
  const [channelID, setChannelID] = useState('')
  const [selectedSources, setSelectedSources] = useState<string[]>([])
  const [languagePrefs, setLanguagePrefs] = useState<LanguagePreferences>({
    language: '',
    mode: 'all',
  })
  const [minSizeText, setMinSizeText] = useState('')
  const [maxSizeText, setMaxSizeText] = useState('')
  const [cachedOnly, setCachedOnly] = useState(false)
  const [validationError, setValidationError] = useState<string | null>(null)

  const mediaType = target
    ? target.mode === 'edit'
      ? target.alert.media_type
      : target.work.type
    : 'movie'
  const isTV = mediaType === 'tv'

  const parsedSeason = /^\d+$/.test(seasonText.trim()) ? Number(seasonText.trim()) : NaN
  const parsedEpisode = /^\d+$/.test(episodeText.trim()) ? Number(episodeText.trim()) : NaN

  // Detect if the user already has an active alert for this exact work/episode
  // when opened in create mode so they can update or remove it directly.
  const matchedExisting = useMemo(() => {
    if (!target || target.mode !== 'create' || !existingAlerts?.length) return undefined
    return existingAlerts.find((item) => {
      if (item.work_id !== target.work.id) return false
      if (target.work.type === 'tv') {
        return item.season === parsedSeason && item.episode === parsedEpisode
      }
      return true
    })
  }, [target, existingAlerts, parsedSeason, parsedEpisode])

  const editingAlert = target?.mode === 'edit' ? target.alert : matchedExisting

  useEffect(() => {
    if (!open || !target) return
    setValidationError(null)
    if (target.mode === 'edit') {
      const a = target.alert
      setSeasonText(a.season !== undefined ? String(a.season) : '1')
      setEpisodeText(a.episode !== undefined ? String(a.episode) : '1')
      setChannelID(a.channel_id)
      setSelectedSources(a.filters?.providers ?? [])
      setLanguagePrefs(initialLanguagePrefs(a.filters))
      setMinSizeText(formatAlertSizeInput(a.filters?.min_size))
      setMaxSizeText(formatAlertSizeInput(a.filters?.max_size))
      setCachedOnly(Boolean(a.filters?.cached_only))
    } else {
      const initialSeason = target.season ?? 1
      const initialEpisode = target.episode ?? 1
      setSeasonText(String(initialSeason))
      setEpisodeText(String(initialEpisode))
      const existing = existingAlerts?.find((item) =>
        target.work.type === 'tv'
          ? item.work_id === target.work.id &&
            item.season === initialSeason &&
            item.episode === initialEpisode
          : item.work_id === target.work.id,
      )
      if (existing) {
        setChannelID(existing.channel_id)
        setSelectedSources(existing.filters?.providers ?? [])
        setLanguagePrefs(initialLanguagePrefs(existing.filters))
        setMinSizeText(formatAlertSizeInput(existing.filters?.min_size))
        setMaxSizeText(formatAlertSizeInput(existing.filters?.max_size))
        setCachedOnly(Boolean(existing.filters?.cached_only))
      } else {
        setChannelID(defaultChannelID)
        setSelectedSources([])
        setLanguagePrefs({ language: '', mode: 'all' })
        setMinSizeText('')
        setMaxSizeText('')
        setCachedOnly(false)
      }
    }
  }, [open, target, defaultChannelID, existingAlerts])

  useEffect(() => {
    if (open && !channelID && defaultChannelID) {
      setChannelID(defaultChannelID)
    }
  }, [open, channelID, defaultChannelID])

  const minParsed = parseAlertSizeInput(minSizeText)
  const maxParsed = parseAlertSizeInput(maxSizeText)
  const sizeOrderError =
    !minParsed.error &&
    !maxParsed.error &&
    minParsed.bytes !== undefined &&
    maxParsed.bytes !== undefined &&
    minParsed.bytes > maxParsed.bytes
      ? 'Minimum size must not exceed maximum size.'
      : null

  const draftFilters: ReleaseAlertFilters = {
    ...(selectedSources.length ? { providers: selectedSources } : {}),
    ...(languagePrefs.mode !== 'all'
      ? { language: resolvedLanguage(languagePrefs), language_mode: languagePrefs.mode }
      : {}),
    ...(minParsed.bytes ? { min_size: minParsed.bytes } : {}),
    ...(maxParsed.bytes ? { max_size: maxParsed.bytes } : {}),
    ...(cachedOnly ? { cached_only: true } : {}),
  }
  const hasFilters = hasActiveAlertFilters(draftFilters)

  const saveMutation = useMutation({
    mutationFn: async () => {
      if (!target) throw new Error('No target selected.')
      const activeChannel = channelID || defaultChannelID
      if (!activeChannel) throw new Error('Select a notification channel.')
      if (isTV) {
        if (!Number.isInteger(parsedSeason) || parsedSeason < 0 || parsedSeason > 999) {
          throw new Error('Enter a valid season number (0–999).')
        }
        if (!Number.isInteger(parsedEpisode) || parsedEpisode < 1 || parsedEpisode > 9999) {
          throw new Error('Enter a valid episode number (1–9999).')
        }
      }
      if (minParsed.error) throw new Error(minParsed.error)
      if (maxParsed.error) throw new Error(maxParsed.error)
      if (sizeOrderError) throw new Error(sizeOrderError)

      const base = trusted ? '/trusted/alerts' : '/alerts'
      const lang = encodeURIComponent(browserLanguage())
      if (editingAlert) {
        return api<ReleaseAlert>(
          `${base}/${encodeURIComponent(editingAlert.id)}?language=${lang}`,
          {
            method: 'PUT',
            body: JSON.stringify({
              channel_id: activeChannel,
              filters: draftFilters,
              revision: editingAlert.revision,
            }),
          },
        )
      }
      const workID = target.mode === 'create' ? target.work.id : target.alert.work_id
      return api<ReleaseAlert>(`${base}?language=${lang}`, {
        method: 'POST',
        body: JSON.stringify({
          work_id: workID,
          ...(isTV ? { season: parsedSeason, episode: parsedEpisode } : {}),
          channel_id: activeChannel,
          filters: draftFilters,
        }),
      })
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: alertsKey })
      await queryClient.invalidateQueries({ queryKey: channelsKey })
      notify(editingAlert ? 'Release alert updated.' : 'Release alert created.')
      onSaved?.()
      onOpenChange(false)
    },
  })

  const deleteMutation = useMutation({
    mutationFn: async (alert: ReleaseAlert) => {
      const base = trusted ? '/trusted/alerts' : '/alerts'
      await api<void>(`${base}/${encodeURIComponent(alert.id)}`, {
        method: 'DELETE',
        body: JSON.stringify({ revision: alert.revision }),
      })
    },
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: alertsKey })
      await queryClient.invalidateQueries({ queryKey: channelsKey })
      notify('Release alert removed.')
      onSaved?.()
      onOpenChange(false)
    },
  })

  if (!target) return null

  const title = target.mode === 'edit' ? target.alert.title : target.work.title
  const cover = target.mode === 'edit' ? target.alert.cover : target.work.cover
  const summary =
    target.mode === 'edit'
      ? catalogCardSummary({ type: target.alert.media_type, years: target.alert.years })
      : catalogCardSummary(target.work)
  const unavailableNotice = target.mode === 'create' && Boolean(target.unavailableNotice)
  const episodeBadge =
    target.mode === 'edit' ? formatAlertEpisode(target.alert.season, target.alert.episode) : ''
  const busy = saveMutation.isPending || deleteMutation.isPending
  const noChannels = channelsQuery.isSuccess && availableChannels.length === 0

  return (
    <Dialog
      open={open}
      onOpenChange={(next) => {
        if (!busy) {
          if (!next) {
            saveMutation.reset()
            deleteMutation.reset()
            setValidationError(null)
          }
          onOpenChange(next)
        }
      }}
    >
      <DialogContent showCloseButton={!busy}>
        <DialogHeader>
          <DialogTitle>
            {editingAlert
              ? 'Release alert'
              : unavailableNotice
                ? 'No source releases available'
                : 'Set release alert'}
          </DialogTitle>
          <DialogDescription>
            {editingAlert
              ? 'An alert is already active for this target. Update its channel or filters, or remove it.'
              : unavailableNotice
                ? `No source releases are currently available for “${title}”. Set an alert to be notified when a matching release becomes available.`
                : `Receive a notification when a matching release becomes available for “${title}”. The alert is removed automatically after delivery.`}
          </DialogDescription>
        </DialogHeader>

        <div className="flex items-center gap-3 rounded-md border border-border bg-muted/30 p-3">
          <CatalogCover
            src={cover}
            book={mediaType === 'book'}
            game={mediaType === 'game'}
            software={mediaType === 'software'}
            music={mediaType === 'music'}
          />
          <div className="min-w-0">
            <p className="truncate text-sm font-medium">{title}</p>
            <p className="help truncate">
              {summary}
              {episodeBadge ? ` · ${episodeBadge}` : ''}
              {!trusted &&
                editingAlert?.owner_type === 'trusted' &&
                editingAlert.owner_email &&
                ` · ${editingAlert.owner_email}`}
            </p>
          </div>
        </div>

        {editingAlert?.status === 'retrying' && editingAlert.last_error && (
          <p role="status" className="notice notice-warning text-xs">
            Last delivery attempt failed: {editingAlert.last_error}. Delivery will retry
            automatically, or you can select another channel.
          </p>
        )}

        <ErrorNotice error={channelsQuery.error} retry={() => void channelsQuery.refetch()} />

        {channelsQuery.isPending ? (
          <Loading label="Loading notification channels" />
        ) : noChannels ? (
          <div className="space-y-4">
            <div className="notice space-y-2">
              <p className="text-sm font-medium">No notification channel available</p>
              <p className="help">
                {trusted
                  ? 'Ask the administrator to enable a notification channel before creating release alerts.'
                  : 'Configure and enable at least one notification channel (Discord, ntfy, Gotify, Telegram, Slack or webhook) before creating release alerts.'}
              </p>
            </div>
            <DialogFooter>
              <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
                Close
              </Button>
              {!trusted && (
                <Button
                  type="button"
                  onClick={() => {
                    onOpenChange(false)
                    navigate(routes.connections.alerts.settings)
                  }}
                >
                  <GearSixIcon aria-hidden="true" />
                  Configure channels
                </Button>
              )}
            </DialogFooter>
          </div>
        ) : (
          <form
            className="space-y-4"
            onSubmit={(event) => {
              event.preventDefault()
              setValidationError(null)
              if (minParsed.error) {
                setValidationError(minParsed.error)
                return
              }
              if (maxParsed.error) {
                setValidationError(maxParsed.error)
                return
              }
              if (sizeOrderError) {
                setValidationError(sizeOrderError)
                return
              }
              saveMutation.mutate()
            }}
          >
            <fieldset className="min-w-0 space-y-4" disabled={busy}>
              {isTV && target.mode === 'create' && (
                <div className="grid grid-cols-2 gap-3">
                  <div className="field">
                    <Label
                      htmlFor={`${idPrefix}-season`}
                      help="Series alerts watch a specific episode. Season 1 Episode 1 is selected by default when a series has no releases yet."
                    >
                      Season
                    </Label>
                    <Input
                      id={`${idPrefix}-season`}
                      type="number"
                      min={0}
                      max={999}
                      step={1}
                      required
                      value={seasonText}
                      onChange={(event) => setSeasonText(event.target.value)}
                    />
                  </div>
                  <div className="field">
                    <Label htmlFor={`${idPrefix}-episode`}>Episode</Label>
                    <Input
                      id={`${idPrefix}-episode`}
                      type="number"
                      min={1}
                      max={9999}
                      step={1}
                      required
                      value={episodeText}
                      onChange={(event) => setEpisodeText(event.target.value)}
                    />
                  </div>
                </div>
              )}

              <div className="field">
                <Label htmlFor={`${idPrefix}-channel`}>Notification channel</Label>
                <select
                  id={`${idPrefix}-channel`}
                  className="native-select"
                  value={channelID}
                  required
                  onChange={(event) => setChannelID(event.target.value)}
                >
                  {!availableChannels.some((ch) => ch.id === channelID) && channelID && (
                    <option value={channelID}>Unavailable channel</option>
                  )}
                  {availableChannels.map((ch) => (
                    <option key={ch.id} value={ch.id}>
                      {ch.name} ({notificationChannelTypeLabel(ch.type)})
                      {ch.is_default ? ' · Default' : ''}
                    </option>
                  ))}
                </select>
              </div>

              <Disclosure
                title={
                  <span className="flex flex-wrap items-center justify-between gap-2">
                    <span>Release filters</span>
                    {hasFilters && (
                      <span className="text-xs font-normal text-muted-foreground">Configured</span>
                    )}
                  </span>
                }
                help="Optional criteria. Leave unset to be notified on the first available source release."
              >
                {sourceOptions.length > 0 && (
                  <div className="field">
                    <Label htmlFor={`${idPrefix}-sources`}>Sources</Label>
                    <SourceMultiSelect
                      id={`${idPrefix}-sources`}
                      value={selectedSources}
                      options={sourceOptions}
                      onChange={setSelectedSources}
                    />
                  </div>
                )}

                <LanguageFilter
                  layout="stacked"
                  value={languagePrefs}
                  onChange={setLanguagePrefs}
                />

                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="field">
                    <Label
                      htmlFor={`${idPrefix}-min-size`}
                      help="Optional minimum size, e.g. 1 GB, 700 MB, or bytes."
                    >
                      Minimum size
                    </Label>
                    <Input
                      id={`${idPrefix}-min-size`}
                      value={minSizeText}
                      placeholder="e.g. 1 GB"
                      aria-invalid={Boolean(minParsed.error || sizeOrderError)}
                      onChange={(event) => {
                        setMinSizeText(event.target.value)
                        setValidationError(null)
                      }}
                    />
                    {minParsed.error && (
                      <p role="alert" className="text-xs text-destructive">
                        {minParsed.error}
                      </p>
                    )}
                  </div>
                  <div className="field">
                    <Label
                      htmlFor={`${idPrefix}-max-size`}
                      help="Optional maximum size, e.g. 20 GB."
                    >
                      Maximum size
                    </Label>
                    <Input
                      id={`${idPrefix}-max-size`}
                      value={maxSizeText}
                      placeholder="No maximum"
                      aria-invalid={Boolean(maxParsed.error || sizeOrderError)}
                      onChange={(event) => {
                        setMaxSizeText(event.target.value)
                        setValidationError(null)
                      }}
                    />
                    {maxParsed.error && (
                      <p role="alert" className="text-xs text-destructive">
                        {maxParsed.error}
                      </p>
                    )}
                  </div>
                </div>
                {sizeOrderError && (
                  <p role="alert" className="text-xs text-destructive">
                    {sizeOrderError}
                  </p>
                )}

                <div className="flex items-center justify-between gap-3">
                  <Label
                    htmlFor={`${idPrefix}-cached`}
                    help="Wait until a matching release is cached on a configured debrid service before sending the notification."
                  >
                    Cached results only
                  </Label>
                  <Switch
                    id={`${idPrefix}-cached`}
                    checked={cachedOnly}
                    onCheckedChange={setCachedOnly}
                  />
                </div>
              </Disclosure>
            </fieldset>

            {validationError && <ErrorNotice error={new Error(validationError)} />}
            <ErrorNotice error={saveMutation.error} />
            <ErrorNotice error={deleteMutation.error} />

            <DialogFooter>
              {editingAlert && (
                <ConfirmAction
                  title="Remove release alert?"
                  description="You will no longer be notified when this release becomes available."
                  label="Remove alert"
                  variant="destructive"
                  action={() => deleteMutation.mutateAsync(editingAlert)}
                  trigger={
                    <Button type="button" variant="destructive" disabled={busy}>
                      <TrashIcon aria-hidden="true" />
                      Remove
                    </Button>
                  }
                />
              )}
              <Button type="submit" disabled={busy}>
                {editingAlert ? (
                  <>
                    <FloppyDiskIcon aria-hidden="true" />
                    {saveMutation.isPending ? 'Saving…' : 'Save alert'}
                  </>
                ) : (
                  <>
                    <BellIcon aria-hidden="true" />
                    {saveMutation.isPending ? 'Creating…' : 'Create alert'}
                  </>
                )}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}
