import {
  ArrowClockwiseIcon,
  FloppyDiskIcon,
  LinkIcon,
  MagnifyingGlassIcon,
  StopIcon,
  TrashIcon,
} from '@phosphor-icons/react'

import { debridProviders, type DebridSecret } from '@/lib/debrid'
import { BookImportFilters } from '@/components/book-import-filters'
import { SoftwareImportFilters } from '@/components/software-import-filters'
import { MusicImportFilters } from '@/components/music-import-filters'
import { MaintenanceStatus } from '@/components/maintenance-status'
import { SettingsSections } from '@/components/settings-sections'
import { RuntimeSettingsPanel } from '@/components/runtime-settings'
import { messageText, number as formatNumber } from '@/lib/format'
import { routes } from '@/lib/routes'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useEffect, useState, type ReactNode } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useLocation } from 'react-router-dom'
import {
  api,
  ApiError,
  commitRevisionQueryData,
  isRevisionConflict,
  queryClient,
  refreshData,
  LIVE_REFRESH_INTERVAL,
} from '@/lib/api'
import type { Items, SecretInfo, ProviderSummary } from '@/lib/types'
import { Loading, ErrorNotice, ConfirmAction } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Disclosure } from '@/components/ui/disclosure'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { InfoTip } from '@/components/ui/info-tip'
import { Switch } from '@/components/ui/switch'
import { SecretPicker } from '@/components/secret-picker'
import { CredentialPageLink } from '@/components/credential-page-link'
import { FormStatus } from '@/components/form-status'
import type { Config, Controls, Tracker } from '@/lib/aiosource-types'
import SharingPage from '@/pages/sharing'
import { metadataSections, type MetadataSection } from '@/lib/metadata-navigation'
import { musicRecoveryMessage, softwareRecoveryMessage } from '@/lib/information-sync'

type MainScope = MetadataSection | 'debrid'
type FormScope =
  | MainScope
  | 'trackers'
  | 'software-catalog'
  | 'book-catalog'
  | 'music-catalog'
  | 'cache-availability'
  | 'videos-updates'
  | 'software-updates'
  | 'books-updates'
  | 'music-updates'
  | 'identification-updates'
  | 'games-updates'
  | 'games-requests'
  | 'videos-requests'
const sectionForms: Record<MainScope, FormScope[]> = {
  videos: ['videos', 'videos-updates', 'videos-requests'],
  games: ['games', 'games-updates', 'games-requests'],
  software: ['software', 'software-catalog', 'software-updates'],
  music: ['music', 'music-catalog', 'music-updates'],
  books: ['books', 'book-catalog', 'books-updates'],
  identification: ['identification', 'identification-updates'],
  debrid: ['debrid', 'cache-availability'],
}
type Changes = Partial<Omit<Config, 'revision' | 'trackers'>> & {
  trackers?: Record<string, Partial<Tracker>>
}
type Draft = { base: Config; changes: Changes }
const resetScopes = {
  'reset-games': 'games',
  'reset-software': 'software',
  'reset-books': 'books',
  'reset-music': 'music',
  'reset-videos': 'videos',
  'reset-metadata': 'metadata',
  'reset-mapping': 'mapping',
  'reset-cache': 'cache',
} as const

function applyChanges(base: Config, changes: Changes): Config {
  const { trackers, ...settings } = changes
  const value = { ...base, ...settings, trackers: { ...base.trackers } }
  for (const [id, patch] of Object.entries(trackers ?? {})) {
    value.trackers[id] = {
      ...(value.trackers[id] ?? { private: true, announce_secret: '' }),
      ...patch,
    }
  }
  return value
}
function Panel({
  title,
  help,
  children,
}: {
  title: string
  help?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="section-card">
      <div className="flex items-center gap-1.5">
        <h2 className="section-title">{title}</h2>
        {help && <InfoTip label={title}>{help}</InfoTip>}
      </div>
      {children}
    </section>
  )
}
export default function AIOSourcePage({
  section,
}: {
  section: MetadataSection | 'debrid' | 'stremio'
}) {
  const location = useLocation()
  const usesSecrets =
    section === 'games' ||
    section === 'software' ||
    section === 'videos' ||
    section === 'music' ||
    section === 'debrid'
  const usesProviders = section === 'debrid'
  const [maintenanceOpen, setMaintenanceOpen] = useState(location.hash === '#maintenance')
  const [manualActionsOpen, setManualActionsOpen] = useState(location.hash === '#maintenance')
  useEffect(() => {
    if (location.hash === '#maintenance') {
      setMaintenanceOpen(true)
      setManualActionsOpen(true)
    }
  }, [location.hash])
  const query = useQuery({
    queryKey: ['aiosource', 'controls'],
    queryFn: ({ signal }) => api<Controls>('/aiosource/controls', { signal }),
    refetchInterval: LIVE_REFRESH_INTERVAL,
  })
  const secrets = useQuery({
    queryKey: ['secrets'],
    queryFn: ({ signal }) => api<Items<SecretInfo>>('/secrets', { signal }),
    enabled: usesSecrets,
  })
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
    enabled: usesProviders,
  })
  const [drafts, setDrafts] = useState<Partial<Record<FormScope, Draft>>>({})
  const [feedback, setFeedback] = useState<
    Partial<Record<FormScope, { error?: unknown; saved?: boolean }>>
  >({})
  const metadata =
    section === 'games' ||
    section === 'software' ||
    section === 'videos' ||
    section === 'books' ||
    section === 'music' ||
    section === 'identification'
  const scope = section === 'stremio' ? 'debrid' : section
  const formConfig = (form: FormScope) => {
    const draft = drafts[form]
    return draft ? applyChanges(draft.base, draft.changes) : query.data?.config
  }
  const config = formConfig(scope)
  const forms = sectionForms[scope]
  const dirty = forms.some((form) => Boolean(drafts[form]))
  const automaticScope =
    section === 'games'
      ? 'games-updates'
      : section === 'software'
        ? 'software-updates'
        : section === 'books'
          ? 'books-updates'
          : section === 'music'
            ? 'music-updates'
            : section === 'identification'
              ? 'identification-updates'
              : 'videos-updates'
  const trackersDraft = drafts.trackers
  const trackersConfig = trackersDraft
    ? applyChanges(trackersDraft.base, trackersDraft.changes)
    : query.data?.config
  const save = useMutation({
    mutationFn: ({ value }: { scope: FormScope; value: Config }) =>
      api('/aiosource/config', { method: 'PUT', body: JSON.stringify(value) }),
    onMutate: () => queryClient.cancelQueries({ queryKey: ['aiosource'] }),
    onSuccess: async (_, savedForm) => {
      await queryClient.invalidateQueries({ queryKey: ['aiosource'] })
      let saved = queryClient.getQueryData<Controls>(['aiosource', 'controls'])?.config
      if (!saved || saved.revision < savedForm.value.revision + 1) {
        saved = { ...savedForm.value, revision: savedForm.value.revision + 1 }
        queryClient.setQueryData<Controls>(['aiosource', 'controls'], (current) =>
          current ? { ...current, config: saved! } : current,
        )
      }
      setDrafts((current) => {
        const next = { ...current }
        delete next[savedForm.scope]
        // Rebase other local edits after our own successful save. Keep their
        // changed fields while adopting the new revision and untouched values.
        if (saved && saved.revision === savedForm.value.revision + 1) {
          for (const key of Object.keys(next) as FormScope[]) {
            if (next[key]!.base.revision === savedForm.value.revision) {
              next[key] = { ...next[key]!, base: saved }
            }
          }
        }
        return next
      })
      setFeedback((current) => ({ ...current, [savedForm.scope]: { saved: true } }))
    },
    onError: (error, failedForm) => {
      setFeedback((current) => ({ ...current, [failedForm.scope]: { error } }))
      if (error instanceof ApiError && error.status === 409)
        void queryClient.invalidateQueries({ queryKey: ['aiosource'] })
    },
  })
  const review = useMutation({
    mutationFn: (_form: FormScope) => api<Controls>('/aiosource/controls'),
    onSuccess: async (latest, form) => {
      const effective = await commitRevisionQueryData(
        ['aiosource', 'controls'],
        latest,
        (value) => value.config.revision,
      )
      setDrafts((drafts) => {
        if (!drafts[form]) return drafts
        const next = { ...drafts }
        const merged = applyChanges(effective.config, drafts[form].changes)
        if (JSON.stringify(merged) === JSON.stringify(effective.config)) delete next[form]
        else next[form] = { ...drafts[form], base: effective.config }
        return next
      })
      clearFeedback(form)
    },
  })
  const conflict = (form: FormScope) =>
    Boolean(
      drafts[form] &&
      isRevisionConflict(
        drafts[form].base.revision,
        query.data?.config.revision,
        feedback[form]?.error,
      ),
    )
  useUnsavedChanges(
    dirty,
    metadata ? `${metadataSections[section].label.toLowerCase()} settings` : 'debrid settings',
    save.isPending && forms.includes(save.variables?.scope),
  )
  useUnsavedChanges(
    Boolean(
      trackersDraft &&
      JSON.stringify(applyChanges(trackersDraft.base, trackersDraft.changes)) !==
        JSON.stringify(trackersDraft.base),
    ),
    'private source playback',
    save.isPending && save.variables?.scope === 'trackers',
  )
  const action = useMutation({
    mutationFn: (name: string) => api(`/aiosource/${name}`, { method: 'POST' }),
    onSuccess: async (_, name) => {
      const resetScope = resetScopes[name as keyof typeof resetScopes]
      if (
        resetScope ||
        name === 'sync' ||
        name === 'match' ||
        name === 'recompute-correspondences' ||
        name === 'sync-books' ||
        name === 'sync-games' ||
        name === 'sync-software' ||
        name === 'sync-music'
      ) {
        await queryClient.cancelQueries({ queryKey: ['aiosource', 'controls'], exact: true })
        queryClient.setQueryData<Controls>(['aiosource', 'controls'], (current) => {
          if (!current) return current
          if (resetScope)
            return {
              ...current,
              maintenance: {
                scope: resetScope,
                status: 'running',
                phase:
                  resetScope !== 'cache' &&
                  (current.syncing ||
                    current.matching ||
                    current.books?.running ||
                    current.games?.running ||
                    current.software?.running ||
                    current.music?.running)
                    ? 'stopping'
                    : 'resetting',
              },
            }
          if (name === 'sync-games')
            return current.games
              ? { ...current, games: { ...current.games, running: true } }
              : current
          if (name === 'sync-software')
            return current.software
              ? { ...current, software: { ...current.software, running: true } }
              : current
          if (name === 'sync-music')
            return current.music
              ? { ...current, music: { ...current.music, running: true } }
              : current
          if (name === 'sync') return { ...current, syncing: true }
          if (name === 'match' || name === 'recompute-correspondences')
            return { ...current, matching: true }
          return current.books
            ? { ...current, books: { ...current.books, running: true } }
            : current
        })
      }
      // Admission is acknowledged by the write. A slow status/statistics read
      // must not keep Stop pending or keep the reset confirmation open.
      void refreshData()
    },
  })
  const update = (form: FormScope, patch: Changes) => {
    if (!query.data) return
    clearFeedback(form)
    const base = query.data.config
    setDrafts((current) => {
      const previous = current[form]
      const changes = { ...previous?.changes, ...patch }
      if (patch.trackers) {
        changes.trackers = { ...previous?.changes.trackers }
        for (const [id, value] of Object.entries(patch.trackers)) {
          changes.trackers[id] = { ...changes.trackers[id], ...value }
        }
      }
      const draft = { base: previous?.base ?? base, changes }
      const next = { ...current }
      if (JSON.stringify(applyChanges(draft.base, changes)) === JSON.stringify(draft.base)) {
        delete next[form]
      } else {
        next[form] = draft
      }
      return next
    })
  }
  const clearFeedback = (form: FormScope) => {
    if (review.variables === form) review.reset()
    setFeedback((current) => {
      const next = { ...current }
      delete next[form]
      return next
    })
  }
  const secret = (
    label: string,
    key:
      | 'tmdb_secret'
      | 'igdb_client_id_secret'
      | 'igdb_secret'
      | 'software_nvd_secret'
      | 'musicbrainz_secret'
      | DebridSecret,
    help?: ReactNode,
  ) => (
    <SecretPicker
      inputId={key}
      label={label}
      help={help}
      value={config?.[key] ?? ''}
      savedValue={query.data?.config[key] ?? ''}
      onChange={(reference) => update(scope, { [key]: reference })}
      prefix={
        key === 'igdb_client_id_secret'
          ? 'igdb_client_id'
          : key === 'igdb_secret'
            ? 'igdb_client_secret'
            : key === 'tmdb_secret'
              ? 'tmdb_api_token'
              : key === 'software_nvd_secret'
                ? 'software_nvd_api_key'
                : key === 'musicbrainz_secret'
                  ? 'musicbrainz_replication_token'
                  : `${key.replace('_secret', '')}_api_key`
      }
      addLabel={`Add ${
        key === 'igdb_client_id_secret'
          ? 'Twitch client ID'
          : key === 'igdb_secret'
            ? 'Twitch client secret'
            : key === 'tmdb_secret'
              ? 'TMDB token'
              : key === 'software_nvd_secret'
                ? 'NVD API key'
                : key === 'musicbrainz_secret'
                  ? 'MetaBrainz token'
                  : `${debridProviders.find((provider) => provider.secret === key)?.name} key`
      }`}
      valueLabel={label}
      disabled={save.isPending}
    />
  )
  const number = (
    label: string,
    key:
      | 'sync_minutes'
      | 'match_seconds'
      | 'tmdb_requests_per_second'
      | 'cache_minutes'
      | 'response_seconds'
      | 'igdb_minutes'
      | 'igdb_requests_per_second'
      | 'igdb_reconcile_days'
      | 'software_minutes'
      | 'openlibrary_minutes'
      | 'musicbrainz_minutes',
    min: number,
    max: number,
    form: FormScope = scope,
    help?: ReactNode,
  ) => {
    const currentValue = formConfig(form)?.[key]
    return (
      <div className="field">
        <Label htmlFor={key} help={help}>
          {label}
        </Label>
        <Input
          id={key}
          type="number"
          step={1}
          disabled={save.isPending}
          required
          min={min}
          max={max}
          aria-invalid={
            currentValue === undefined ||
            !Number.isInteger(currentValue) ||
            currentValue < min ||
            currentValue > max
          }
          value={currentValue === undefined || Number.isNaN(currentValue) ? '' : currentValue}
          onChange={(e) => update(form, { [key]: e.target.valueAsNumber })}
        />
      </div>
    )
  }
  const toggle = (
    label: string,
    key:
      | 'igdb_enabled'
      | 'software_enabled'
      | 'auto_sync'
      | 'auto_match'
      | 'auto_cache'
      | 'openlibrary_enabled'
      | 'musicbrainz_enabled',
    form: FormScope = scope,
    help?: ReactNode,
  ) => (
    <div className="flex items-center gap-1.5">
      <label className="flex items-center gap-3 text-sm">
        <Switch
          aria-label={label}
          checked={formConfig(form)?.[key] ?? false}
          disabled={save.isPending}
          onCheckedChange={(checked) => update(form, { [key]: checked })}
        />
        {label}
      </label>
      {help && <InfoTip label={label}>{help}</InfoTip>}
    </div>
  )
  if (query.isPending)
    return (
      <div className="page">
        <Loading />
      </div>
    )
  if (!config || !query.data)
    return (
      <div className="page">
        <ErrorNotice error={query.error} />
      </div>
    )
  const status = query.data
  const catalogRecovery =
    section === 'music'
      ? musicRecoveryMessage(status.music)
      : section === 'software'
        ? softwareRecoveryMessage(status.software)
        : undefined
  const resetting = status.maintenance?.status === 'running'
  const maintenanceBusy = resetting || action.isPending
  const invalidNumbers = (form: FormScope) => {
    const local = drafts[form]
    if (!local) return false
    return Object.values(applyChanges(local.base, local.changes)).some(
      (value) => typeof value === 'number' && Number.isNaN(value),
    )
  }
  const submit = (form: FormScope) => {
    const local = drafts[form]
    if (local && !save.isPending && !review.isPending && !conflict(form) && !invalidNumbers(form)) {
      save.mutate({ scope: form, value: applyChanges(local.base, local.changes) })
    }
  }
  const footer = (form: FormScope) => (
    <>
      <ErrorNotice error={feedback[form]?.error} />
      {review.variables === form && <ErrorNotice error={review.error} />}
      {conflict(form) && (
        <p role="status" className="text-sm text-muted-foreground">
          These settings changed elsewhere. Load the latest settings to review them; your edits will
          be kept.
        </p>
      )}
      <div className="flex flex-wrap items-center gap-4">
        <Button
          type="submit"
          disabled={
            !drafts[form] ||
            save.isPending ||
            review.isPending ||
            conflict(form) ||
            invalidNumbers(form) ||
            (form === 'software-catalog' && (Boolean(status.software?.running) || resetting)) ||
            (form === 'book-catalog' && (Boolean(status.books?.running) || resetting)) ||
            (form === 'music-catalog' && (Boolean(status.music?.running) || resetting))
          }
        >
          <FloppyDiskIcon aria-hidden="true" />
          {save.isPending && save.variables?.scope === form ? 'Saving…' : 'Save'}
        </Button>
        {conflict(form) && (
          <Button
            type="button"
            variant="outline"
            disabled={save.isPending || review.isPending}
            onClick={() => review.mutate(form)}
          >
            <ArrowClockwiseIcon aria-hidden="true" />
            Load latest settings
          </Button>
        )}
        <FormStatus
          dirty={Boolean(drafts[form])}
          saving={save.isPending && save.variables?.scope === form}
          saved={feedback[form]?.saved}
          message={
            invalidNumbers(form) ? 'Enter whole numbers within the allowed ranges.' : undefined
          }
        />
      </div>
    </>
  )
  const settingsForm = (
    <>
      <form
        aria-label={
          metadata ? `${metadataSections[section].label} configuration` : 'Debrid configuration'
        }
        onSubmit={(e) => {
          e.preventDefault()
          submit(scope)
        }}
      >
        <fieldset disabled={save.isPending} className="min-w-0 space-y-5">
          <Panel
            title={
              metadata
                ? section === 'identification'
                  ? 'Automatic identification'
                  : 'Information settings'
                : 'Debrid settings'
            }
            help={
              section === 'games'
                ? 'IGDB supplies game titles, platforms and cover references. Use a confidential Twitch application; no OAuth redirect or browser sign-in is required. Images stay remote.'
                : section === 'software'
                  ? 'Software combines local bulk catalogs from NIST NVD CPE 2.0 (US Gov Public Domain), Microsoft WinGet (MIT) and Homebrew Cask (BSD-2-Clause), plus optional remote icon metadata from selfh.st/icons (CC BY 4.0). No external requests are made per torrent or per search. Adding an optional free NVD API key accelerates incremental NVD updates from 5 to 50 requests per 30 seconds.'
                  : section === 'books'
                    ? 'Open Library supplies book and audiobook information without an API key. The first import can take hours; later updates follow recent changes. Cover images stay remote.'
                    : section === 'music'
                      ? 'MusicBrainz supplies album, artist, label, Discogs and Wikidata metadata, plus optional Cover Art Archive front-cover flags. Twice-weekly CC0 exports work without any token; adding an optional free MetaBrainz Live Data Feed token enables hourly global updates (CC BY-NC-SA 3.0) without querying external APIs per torrent. Cover images stay remote.'
                      : undefined
            }
          >
            {section === 'videos' && (
              <div className="grid gap-5 md:grid-cols-2">
                <div className="min-w-0 space-y-1">
                  {secret('TMDB read access token', 'tmdb_secret')}
                  <CredentialPageLink href="https://www.themoviedb.org/settings/api/request">
                    Get TMDB read access token
                  </CredentialPageLink>
                </div>
              </div>
            )}
            {section === 'games' && (
              <div className="space-y-2">
                <div className="grid gap-5 md:grid-cols-2">
                  {secret('Twitch client ID', 'igdb_client_id_secret')}
                  {secret('Twitch client secret', 'igdb_secret')}
                </div>
                <CredentialPageLink href="https://dev.twitch.tv/console/apps/create">
                  Get Twitch credentials
                </CredentialPageLink>
              </div>
            )}
            {section === 'software' && (
              <div className="grid gap-5 md:grid-cols-2">
                <div className="min-w-0 space-y-1">
                  {secret(
                    'NVD API key (optional)',
                    'software_nvd_secret',
                    'Optional. Leave blank to sync NVD CPE at the public rate (5 requests per 30 seconds). With a free NVD API key, incremental updates run at up to 50 requests per 30 seconds. Long pauses (> 110 days) automatically recover from a fresh full snapshot.',
                  )}
                  <CredentialPageLink href="https://nvd.nist.gov/developers/request-an-api-key">
                    Get NVD API key
                  </CredentialPageLink>
                </div>
              </div>
            )}
            {section === 'music' && (
              <div className="grid gap-5 md:grid-cols-2">
                <div className="min-w-0 space-y-1">
                  {secret(
                    'MetaBrainz Live Data Feed token (optional)',
                    'musicbrainz_secret',
                    'Optional. Leave blank to stay on twice-weekly public CC0 full exports. With a free non-commercial MetaBrainz Live Data Feed token, AIOSources applies hourly global replication packets (CC BY-NC-SA 3.0) and automatically recovers via a full export after long pauses.',
                  )}
                  <CredentialPageLink href="https://metabrainz.org/profile">
                    Get MetaBrainz Live Data Feed token
                  </CredentialPageLink>
                </div>
              </div>
            )}
            {section === 'debrid' && (
              <div className="grid gap-5 md:grid-cols-2">
                {debridProviders.map((provider) => (
                  <div key={provider.id} className="min-w-0 space-y-1">
                    {secret(
                      `${provider.name} API key`,
                      provider.secret,
                      'hint' in provider ? provider.hint : undefined,
                    )}
                    <CredentialPageLink href={provider.credentialUrl}>
                      Get {provider.name} API key
                    </CredentialPageLink>
                  </div>
                ))}
              </div>
            )}
            {section === 'videos' &&
              toggle('Automatically update film & series information', 'auto_sync')}
            {section === 'games' && toggle('Automatically update game information', 'igdb_enabled')}
            {section === 'software' &&
              toggle('Automatically update software information', 'software_enabled')}
            {section === 'books' &&
              toggle('Automatically update book information', 'openlibrary_enabled')}
            {section === 'music' &&
              toggle('Automatically update music information', 'musicbrainz_enabled')}
            {section === 'identification' &&
              toggle('Automatically identify torrents', 'auto_match')}
            {footer(scope)}
          </Panel>
        </fieldset>
      </form>
      <SettingsSections>
        {section === 'debrid' && (
          <Disclosure title="Cache availability">
            <form
              aria-label="Cache availability"
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                submit('cache-availability')
              }}
            >
              {number('Cache validity (minutes)', 'cache_minutes', 5, 43200, 'cache-availability')}
              {toggle(
                'Automatically recheck availability',
                'auto_cache',
                'cache-availability',
                'Retry incomplete checks made with your configured accounts while their cache window remains valid. Public visitors are never rechecked in the background. Temporary-item cleanup remains active.',
              )}
              {footer('cache-availability')}
            </form>
          </Disclosure>
        )}
        {section === 'software' && (
          <form
            aria-label="Software catalog filters"
            onSubmit={(event) => {
              event.preventDefault()
              submit('software-catalog')
            }}
          >
            <SoftwareImportFilters
              value={formConfig('software-catalog')!}
              onChange={(patch) => update('software-catalog', patch)}
              busy={Boolean(status.software?.running) || resetting || save.isPending}
              footer={footer('software-catalog')}
            />
          </form>
        )}
        {section === 'books' && (
          <form
            aria-label="Books catalog filters"
            onSubmit={(event) => {
              event.preventDefault()
              submit('book-catalog')
            }}
          >
            <BookImportFilters
              value={formConfig('book-catalog')!}
              onChange={(patch) => update('book-catalog', patch)}
              busy={Boolean(status.books?.running) || resetting || save.isPending}
              footer={footer('book-catalog')}
            />
          </form>
        )}
        {section === 'music' && (
          <form
            aria-label="Music catalog filters"
            onSubmit={(event) => {
              event.preventDefault()
              submit('music-catalog')
            }}
          >
            <MusicImportFilters
              value={formConfig('music-catalog')!}
              onChange={(patch) => update('music-catalog', patch)}
              busy={Boolean(status.music?.running) || resetting || save.isPending}
              footer={footer('music-catalog')}
            />
          </form>
        )}
        {metadata && (
          <Disclosure
            title="Automatic updates"
            help={
              section === 'identification'
                ? 'Pending work and timing survive restarts. This interval applies to videos, games, software, music and books; manual actions run immediately.'
                : undefined
            }
          >
            <form
              aria-label={`${metadataSections[section].label} automatic updates`}
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                submit(automaticScope)
              }}
            >
              {section === 'videos' &&
                number(
                  'Information update interval (minutes)',
                  'sync_minutes',
                  5,
                  1440,
                  automaticScope,
                )}
              {section === 'games' &&
                number(
                  'Game information update interval (minutes)',
                  'igdb_minutes',
                  5,
                  1440,
                  automaticScope,
                )}
              {section === 'games' &&
                number(
                  'Full catalog reconciliation (days)',
                  'igdb_reconcile_days',
                  1,
                  30,
                  automaticScope,
                  'Refresh all game details periodically to catch deleted records and changes to related information. Progress resumes after an interruption.',
                )}
              {section === 'software' &&
                number(
                  'Software information update interval (minutes)',
                  'software_minutes',
                  15,
                  1440,
                  automaticScope,
                  'Checks NVD CPE incremental changes, WinGet, Homebrew Cask and selfh.st/icons on this interval (default 360 minutes). Conditional requests skip unchanged bulk feeds.',
                )}
              {section === 'books' &&
                number(
                  'Book information update interval (minutes)',
                  'openlibrary_minutes',
                  5,
                  1440,
                  automaticScope,
                )}
              {section === 'music' &&
                number(
                  'Music information update interval (minutes)',
                  'musicbrainz_minutes',
                  15,
                  1440,
                  automaticScope,
                  'With a MetaBrainz Live Data Feed token, hourly packets are checked on this interval (default 60 minutes). Without a token, AIOSources checks twice-weekly full exports at most every 6 hours.',
                )}
              {section === 'identification' &&
                number(
                  'Identification interval (seconds)',
                  'match_seconds',
                  5,
                  86400,
                  automaticScope,
                )}
              {footer(automaticScope)}
            </form>
          </Disclosure>
        )}
        {section === 'games' && (
          <Disclosure title="Request limits">
            <form
              aria-label="Games request limits"
              className="space-y-5"
              onSubmit={(e) => {
                e.preventDefault()
                submit('games-requests')
              }}
            >
              {number(
                'IGDB requests per second',
                'igdb_requests_per_second',
                1,
                4,
                'games-requests',
                'IGDB allows up to four requests per second per application. Use a lower limit if this Twitch application is shared with other services.',
              )}
              {footer('games-requests')}
            </form>
          </Disclosure>
        )}
        {section === 'videos' && (
          <Disclosure title="Request limits">
            <form
              aria-label="Videos request limits"
              className="space-y-5"
              onSubmit={(event) => {
                event.preventDefault()
                submit('videos-requests')
              }}
            >
              {number(
                'TMDB requests per second',
                'tmdb_requests_per_second',
                1,
                40,
                'videos-requests',
                'Lower this rate if several applications use the same TMDB token or if TMDB regularly limits requests.',
              )}
              {footer('videos-requests')}
            </form>
          </Disclosure>
        )}
      </SettingsSections>
    </>
  )
  const maintenance = (
    <Disclosure
      id="maintenance"
      title="Maintenance"
      open={maintenanceOpen}
      onOpenChange={setMaintenanceOpen}
      help={
        metadata
          ? 'Rebuild information or identification results when needed. Each action explains what it removes before confirmation.'
          : 'Available, absent and unknown results stay distinct. Cache observations and retries survive restart and are isolated by provider account.'
      }
    >
      <MaintenanceStatus
        value={status.maintenance}
        scope={
          section === 'identification'
            ? 'mapping'
            : section === 'debrid' || section === 'stremio'
              ? 'cache'
              : section
        }
      />
      {metadata && (
        <div className="flex flex-wrap gap-3">
          {section === 'videos' && (
            <ConfirmAction
              title="Reset video information?"
              description="Information updates and identification will be stopped first. Film and series information, related automatic and manual identifications, availability observations and video operation history will be removed. Torrent records, book information, settings and files in your debrid accounts are retained. Automatic video updates can import the catalog again."
              label="Reset video information"
              variant="destructive"
              action={() => action.mutateAsync('reset-videos')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset video information
                </Button>
              }
            />
          )}
          {section === 'games' && (
            <ConfirmAction
              title="Reset game information?"
              description="Information updates and identification will be stopped first. IGDB information, related automatic and manual identifications, availability observations and game operation history will be removed. Source files, video, software, music and book information, settings and files in your debrid accounts are retained. Automatic game updates can import the catalog again."
              label="Reset game information"
              variant="destructive"
              action={() => action.mutateAsync('reset-games')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset game information
                </Button>
              }
            />
          )}
          {section === 'software' && (
            <ConfirmAction
              title="Reset software information?"
              description="Information updates and identification will be stopped first. NVD CPE, WinGet, Homebrew Cask and selfh.st icon metadata, related automatic and manual identifications, availability observations and software operation history will be removed. Source records, video, game, music and book information, settings and files in your debrid accounts are retained. Automatic software updates can import the catalog again."
              label="Reset software information"
              variant="destructive"
              action={() => action.mutateAsync('reset-software')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset software information
                </Button>
              }
            />
          )}
          {section === 'books' && (
            <ConfirmAction
              title="Reset book information?"
              description="Information updates and identification will be stopped first. Open Library information, related automatic and manual identifications, availability observations and book operation history will be removed. Torrent records, film & series information, settings and files in your debrid accounts are retained. Automatic book updates can import the catalog again."
              label="Reset book information"
              variant="destructive"
              action={() => action.mutateAsync('reset-books')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset book information
                </Button>
              }
            />
          )}
          {section === 'music' && (
            <ConfirmAction
              title="Reset music information?"
              description="Information updates and identification will be stopped first. MusicBrainz and Cover Art Archive metadata, related automatic and manual identifications, availability observations and music operation history will be removed. Source records, video, game and book information, settings and files in your debrid accounts are retained. Automatic music updates can import the catalog again."
              label="Reset music information"
              variant="destructive"
              action={() => action.mutateAsync('reset-music')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset music information
                </Button>
              }
            />
          )}
          {section === 'identification' && (
            <ConfirmAction
              title="Reset title matches?"
              description="Information updates and identification will be stopped first. Automatic correspondences will be recomputed from the local information catalog. Manual choices are retained. No metadata download is needed."
              label="Reset matches"
              variant="destructive"
              action={() => action.mutateAsync('reset-mapping')}
              trigger={
                <Button variant="destructive" disabled={maintenanceBusy}>
                  <TrashIcon aria-hidden="true" />
                  Reset matches
                </Button>
              }
            />
          )}
        </div>
      )}
      {section === 'debrid' && (
        <div className="flex flex-wrap items-center gap-4">
          <ConfirmAction
            title="Reset cache observations?"
            description="Availability will be checked again on the next search. Temporary magnet ownership records are preserved."
            label="Reset cache"
            variant="destructive"
            action={() => action.mutateAsync('reset-cache')}
            trigger={
              <Button variant="destructive" disabled={action.isPending || resetting}>
                <TrashIcon aria-hidden="true" />
                Reset cache
              </Button>
            }
          />
        </div>
      )}
    </Disclosure>
  )
  return (
    <div className="page">
      <ErrorNotice
        error={
          query.error ??
          (usesSecrets ? secrets.error : null) ??
          (usesProviders ? providers.error : null)
        }
      />
      <ErrorNotice error={action.error} scope="action" />
      {metadata && (
        <>
          {settingsForm}
          <SettingsSections>
            {section === 'videos' && <RuntimeSettingsPanel section="metadata" />}
            {(section === 'videos' ||
              section === 'games' ||
              section === 'software' ||
              section === 'music' ||
              section === 'books') && (
              <RuntimeSettingsPanel
                section="metadata_retries"
                scopeHelp="These settings apply to video, game, software, music and book information updates."
              />
            )}
            {section === 'identification' && (
              <>
                <RuntimeSettingsPanel section="identification" />
                <RuntimeSettingsPanel section="matching" />
              </>
            )}
            <Disclosure
              title="Manual actions"
              help="Run a task now without changing its automatic settings. Stopping the current task preserves committed progress; disable its automatic option to stop future runs."
            >
              {section === 'videos' && status.recovery_required && (
                <div
                  role="status"
                  className="notice notice-warning flex flex-wrap items-center gap-1.5"
                >
                  <p>
                    Video information needs recovery.{' '}
                    {status.config.auto_sync
                      ? 'Automatic recovery continues.'
                      : 'Enable automatic video updates or run Update film & series information.'}
                  </p>
                  <InfoTip label="Video recovery">
                    The TMDB change history could not cover the interruption. Film & series
                    information will be refreshed progressively. Existing data, books and manual
                    choices are retained.{' '}
                    {status.config.auto_sync
                      ? 'Automatic recovery continues from saved progress.'
                      : 'Enable automatic video updates, or run Update film & series information to continue.'}
                  </InfoTip>
                </div>
              )}
              {section === 'books' && Boolean(status.books?.skipped) && (
                <p role="status" className="notice notice-warning">
                  {formatNumber(status.books!.skipped)} unreadable or oversized book records were
                  skipped.{' '}
                  <Link className="data-link" to={metadataSections.books.history}>
                    View history
                  </Link>
                </p>
              )}
              {section === 'books' && status.books?.running && (
                <p role="status" className="help">
                  Updating book information. Progress is saved automatically and continues after an
                  interruption.
                </p>
              )}
              {section === 'books' && status.books?.needs_export && (
                <div
                  role="status"
                  className="notice notice-warning flex flex-wrap items-center gap-1.5"
                >
                  <p>
                    A newer Open Library export is needed to recover a missing update period.
                    Existing data is retained.{' '}
                    {status.config.openlibrary_enabled
                      ? 'Recent updates continue while recovery waits.'
                      : 'Enable automatic book updates or run Update book information.'}
                  </p>
                  <InfoTip label="Book recovery">
                    Change continuity was lost. A newer Open Library export is needed to cover the
                    missing period. Existing data is retained.{' '}
                    {status.config.openlibrary_enabled
                      ? 'Recent updates continue while automatic recovery waits for that export.'
                      : 'Enable automatic book updates, or run Update book information to retry manually.'}
                  </InfoTip>
                </div>
              )}
              {catalogRecovery && (
                <p role="status" className="notice notice-warning">
                  {catalogRecovery}
                </p>
              )}
              {section === 'software' && status.software?.running && (
                <p role="status" className="help">
                  Updating software information. Existing entries remain searchable during import.
                </p>
              )}
              {section === 'music' && status.music?.schema_unsupported && (
                <div
                  role="status"
                  className="notice notice-warning flex flex-wrap items-center gap-1.5"
                >
                  <p>
                    MusicBrainz published a newer database schema that this AIOSources version does
                    not support yet. Existing music information is preserved until you upgrade
                    AIOSources.
                  </p>
                </div>
              )}
              {section === 'music' && status.music?.running && (
                <p role="status" className="help">
                  Updating music information. Existing entries remain searchable during import.
                </p>
              )}
              {Boolean(
                section === 'videos'
                  ? status.status.sync_error
                  : section === 'games'
                    ? status.games?.error
                    : section === 'software'
                      ? status.software?.error
                      : section === 'books'
                        ? status.books?.error
                        : section === 'music'
                          ? status.music?.error
                          : status.matching_error,
              ) && (
                <p role="status" className="notice notice-warning">
                  {messageText(
                    section === 'videos'
                      ? status.status.sync_error
                      : section === 'games'
                        ? status.games?.error || ''
                        : section === 'software'
                          ? status.software?.error || ''
                          : section === 'books'
                            ? status.books?.error || ''
                            : section === 'music'
                              ? status.music?.error || ''
                              : status.matching_error || '',
                  )}{' '}
                  <Link className="data-link" to={metadataSections[section].history}>
                    View history
                  </Link>
                </p>
              )}
              {dirty && <p className="help">Save your changes before starting a run.</p>}
              <div className="flex flex-wrap gap-3">
                {section === 'videos' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.syncing ||
                        resetting ||
                        action.isPending ||
                        dirty ||
                        !status.config.tmdb_secret,
                      )}
                      onClick={() => action.mutate('sync')}
                    >
                      <ArrowClockwiseIcon aria-hidden="true" />
                      Update film & series information
                    </Button>
                    {status.syncing && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-sync')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop film & series information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'games' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.games?.running ||
                        resetting ||
                        action.isPending ||
                        dirty ||
                        !status.config.igdb_client_id_secret ||
                        !status.config.igdb_secret,
                      )}
                      onClick={() => action.mutate('sync-games')}
                    >
                      <ArrowClockwiseIcon aria-hidden="true" />
                      Update game information
                    </Button>
                    {status.games?.running && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-games')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop game information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'software' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.software?.running || resetting || action.isPending || dirty,
                      )}
                      onClick={() => action.mutate('sync-software')}
                    >
                      <ArrowClockwiseIcon aria-hidden="true" />
                      Update software information
                    </Button>
                    {status.software?.running && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-software')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop software information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'books' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.books?.running || resetting || action.isPending || dirty,
                      )}
                      onClick={() => action.mutate('sync-books')}
                    >
                      <ArrowClockwiseIcon aria-hidden="true" />
                      Update book information
                    </Button>
                    {status.books?.running && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-books')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop book information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'music' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.music?.running || resetting || action.isPending || dirty,
                      )}
                      onClick={() => action.mutate('sync-music')}
                    >
                      <ArrowClockwiseIcon aria-hidden="true" />
                      Update music information
                    </Button>
                    {status.music?.running && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-music')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop music information update
                      </Button>
                    )}
                  </>
                )}
                {section === 'identification' && (
                  <>
                    <Button
                      disabled={Boolean(
                        status.matching ||
                        resetting ||
                        action.isPending ||
                        dirty ||
                        !status.status.has_information,
                      )}
                      onClick={() => action.mutate('match')}
                    >
                      <MagnifyingGlassIcon aria-hidden="true" />
                      Identify torrents
                    </Button>
                    <Button
                      variant="outline"
                      disabled={Boolean(status.matching || resetting || action.isPending || dirty)}
                      onClick={() => action.mutate('recompute-correspondences')}
                    >
                      <LinkIcon aria-hidden="true" />
                      Recompute correspondences
                    </Button>
                    {status.matching && (
                      <Button
                        variant="warning"
                        disabled={action.isPending}
                        onClick={() => action.mutate('stop-matching')}
                      >
                        <StopIcon aria-hidden="true" />
                        Stop identification
                      </Button>
                    )}
                  </>
                )}
              </div>
            </Disclosure>
            {maintenance}
          </SettingsSections>
        </>
      )}
      {section === 'stremio' && (
        <SharingPage
          format="stremio"
          services={[
            ...debridProviders.filter((provider) => config[provider.secret]),
            { id: 'direct', name: 'Direct torrents' },
          ]}
        />
      )}
      {section === 'debrid' && (
        <>
          {settingsForm}
          <SettingsSections>
            <Disclosure
              title="Private source playback"
              help={
                <>
                  For uncached private torrents, add the complete announce URL, including its
                  passkey, in{' '}
                  <Link className="data-link" to={routes.administration.security.secrets}>
                    Secrets
                  </Link>
                  , or directly below, then save the playback settings. The URL is used exactly as
                  supplied by the tracker, whatever the position of the passkey.
                </>
              }
            >
              <form
                aria-label="Private source playback"
                onSubmit={(e) => {
                  e.preventDefault()
                  submit('trackers')
                }}
              >
                <fieldset disabled={save.isPending} className="space-y-4">
                  {providers.data?.items?.length === 0 && (
                    <p className="text-sm text-muted-foreground">
                      Add your own sources first. No tracker is bundled.
                    </p>
                  )}
                  {providers.data?.items?.map((p) => {
                    const tracker = trackersConfig?.trackers[p.id] ?? {
                      private: true,
                      announce_secret: '',
                    }
                    const change = (patch: Partial<Tracker>) =>
                      update('trackers', { trackers: { [p.id]: patch } })
                    return (
                      <div key={p.id} id={`tracker-${p.id}`} className="space-y-3 border-t pt-4">
                        <h3 className="font-medium">{p.name}</h3>
                        <label className="flex items-center gap-3 text-sm">
                          <Switch
                            aria-label="Private source"
                            checked={tracker.private}
                            onCheckedChange={(checked) => change({ private: checked })}
                          />
                          Private source
                        </label>
                        <SecretPicker
                          inputId={`announce-${p.id}`}
                          label="Announce URL secret"
                          value={tracker.announce_secret}
                          savedValue={status.config.trackers[p.id]?.announce_secret ?? ''}
                          onChange={(reference) => change({ announce_secret: reference })}
                          prefix={`${p.id}_announce_url`}
                          addLabel="Add announce URL"
                          valueLabel="Complete announce URL"
                          disabled={save.isPending || !tracker.private}
                        />
                      </div>
                    )
                  })}
                  {footer('trackers')}
                </fieldset>
              </form>
            </Disclosure>
            <RuntimeSettingsPanel section="debrid" />
            <RuntimeSettingsPanel section="debrid_retries" />
            <Disclosure
              title="Manual actions"
              open={manualActionsOpen}
              onOpenChange={setManualActionsOpen}
            >
              <Button
                disabled={action.isPending || !status.config.alldebrid_secret}
                onClick={() => action.mutate('retry-cleanup')}
              >
                <ArrowClockwiseIcon aria-hidden="true" />
                Retry temporary cleanup
              </Button>
            </Disclosure>
            {maintenance}
          </SettingsSections>
        </>
      )}
    </div>
  )
}
