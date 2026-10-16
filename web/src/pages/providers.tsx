import { ResultCount } from '@/components/common'
import { useNotification } from '@/lib/notifications'
import { useUnsavedChanges } from '@/lib/unsaved-changes'
import { useMemo, useRef, useState } from 'react'
import { useMutation, useQuery } from '@tanstack/react-query'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import { parseProviderJSON, rebaseProviderJSON } from '@/lib/provider-json'
import {
  PlusIcon,
  PencilSimpleIcon,
  TrashIcon,
  CheckCircleIcon,
  FloppyDiskIcon,
  ArrowClockwiseIcon,
  PlayIcon,
  DownloadSimpleIcon,
  UploadSimpleIcon,
  BracketsCurlyIcon,
} from '@phosphor-icons/react'
import { api, ApiError, queryClient, refreshData, COLLECTION_REFRESH_INTERVAL } from '@/lib/api'
import type {
  AdapterInfo,
  Items,
  ProviderDocument,
  ProviderSummary,
  ScheduleSummary,
  Validation,
} from '@/lib/types'
import { messageText, useDate } from '@/lib/format'
import { frequencyLabel } from '@/lib/schedule-input'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'
import { CodeEditor } from '@/components/code-editor'
import { SourceEditor } from '@/components/source-editor'
import { CollectDialog } from '@/components/collect-dialog'
import { SourceFilters, sourceMatches, useSourceFilters } from '@/components/source-filters'
import { FormStatus } from '@/components/form-status'
import { AdvancedImportSettings } from '@/components/advanced-import-settings'
import { Disclosure } from '@/components/ui/disclosure'
import { useListViewState } from '@/lib/detail-navigation'
import {
  ConfirmAction,
  Empty,
  ErrorNotice,
  Loading,
  PageHeader,
  RunStatusBadge,
} from '@/components/common'

export function ProvidersPage() {
  const listState = useListViewState()
  const filters = useSourceFilters()
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  const items = (providers.data?.items ?? []).filter((source) => !source.remote_catalog)
  const schedules = useQuery({
    queryKey: ['schedules'],
    queryFn: ({ signal }) => api<Items<ScheduleSummary>>('/schedules', { signal }),
    refetchInterval: COLLECTION_REFRESH_INTERVAL,
  })
  const bySource = new Map((schedules.data?.items ?? []).map((item) => [item.provider_id, item]))
  const visible = items.filter(
    (source) =>
      sourceMatches(filters.query, source.name, source.id, source.adapter) &&
      (!filters.state ||
        (filters.state === 'enabled' && source.enabled) ||
        (filters.state === 'disabled' && !source.enabled) ||
        (filters.state === 'invalid' && !source.valid)),
  )
  const filtered = Boolean(filters.query || filters.state)
  const toggle = useMutation({
    mutationFn: async (source: ProviderSummary) => {
      const document = await api<ProviderDocument>(`/providers/${encodeURIComponent(source.id)}`)
      if (document.revision !== source.revision)
        throw new ApiError(
          409,
          'This source was changed elsewhere. Refresh before changing its state.',
        )
      const json = parseProviderJSON(document.json)
      if (json.errors.length) throw new Error('Fix the JSON before changing its enabled state.')
      json.set('enabled', !source.enabled)
      return api<ProviderDocument>(`/providers/${encodeURIComponent(source.id)}`, {
        method: 'PUT',
        body: JSON.stringify({ json: json.toString(), revision: source.revision }),
      })
    },
    onSuccess: refreshData,
  })
  return (
    <div className="page">
      <PageHeader
        title="Sources"
        actions={
          <>
            <Button asChild>
              <Link to="/providers/new" state={listState}>
                <PlusIcon />
                Add source
              </Link>
            </Button>
          </>
        }
      />
      <ErrorNotice
        error={toggle.error}
        retryLabel="Reload sources"
        retry={() => {
          void providers.refetch().then((result) => {
            if (!result.isError) toggle.reset()
          })
        }}
      />
      {(items.length > 0 || filtered) && (
        <SourceFilters
          filters={filters}
          states={[
            { value: 'enabled', label: 'Enabled' },
            { value: 'disabled', label: 'Disabled' },
            { value: 'invalid', label: 'Needs fixing' },
          ]}
          count={visible.length}
          total={items.length}
        />
      )}
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      <ErrorNotice error={schedules.error} retry={() => void schedules.refetch()} />
      {providers.isPending ? (
        <Loading />
      ) : visible.length ? (
        <div className="table-frame">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source</TableHead>
                <TableHead>Adapter</TableHead>
                <TableHead>Configuration</TableHead>
                <TableHead>Last run</TableHead>
                <TableHead>Schedule</TableHead>
                <TableHead>Enabled</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visible.map((source) => (
                <TableRow key={source.id}>
                  <TableCell className="whitespace-normal">
                    <Link
                      className="data-link font-medium"
                      to={`/providers/${encodeURIComponent(source.id)}`}
                      state={listState}
                    >
                      {source.name || source.id}
                    </Link>
                  </TableCell>
                  <TableCell>
                    <Badge variant="outline" className="font-mono">
                      {source.adapter || 'Not defined'}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {source.valid ? (
                      <Badge variant="success">Valid</Badge>
                    ) : (
                      <Link
                        className="text-xs text-destructive underline underline-offset-4"
                        to={`/providers/${encodeURIComponent(source.id)}`}
                        state={listState}
                      >
                        Needs fixing
                      </Link>
                    )}
                  </TableCell>
                  <TableCell>
                    {source.last_run ? (
                      <Link
                        to={`/runs/${encodeURIComponent(source.last_run.id)}`}
                        state={listState}
                      >
                        <RunStatusBadge run={source.last_run} />
                      </Link>
                    ) : (
                      <span className="text-xs text-muted-foreground">No runs</span>
                    )}
                  </TableCell>
                  <SourceScheduleCell
                    source={source}
                    schedule={bySource.get(source.id)}
                    listState={listState}
                    unavailable={schedules.isError}
                  />
                  <TableCell>
                    <div className="flex items-center gap-2">
                      <Switch
                        aria-label={`${source.enabled ? 'Disable' : 'Enable'} ${source.name || source.id}`}
                        checked={source.enabled}
                        disabled={!source.valid || toggle.isPending}
                        onCheckedChange={() => toggle.mutate(source)}
                      />
                    </div>
                  </TableCell>
                  <TableCell>
                    <div className="flex justify-end gap-1">
                      <CollectDialog
                        providerId={source.id}
                        trigger={
                          <Button
                            size="icon-sm"
                            variant="ghost"
                            aria-label={`Run manual import for ${source.name || source.id}`}
                            disabled={!source.valid || !source.enabled}
                          >
                            <PlayIcon />
                          </Button>
                        }
                      />
                      <Button size="icon-sm" variant="ghost" asChild>
                        <Link
                          to={`/providers/${encodeURIComponent(source.id)}`}
                          state={listState}
                          aria-label={`Edit ${source.name || source.id}`}
                        >
                          <PencilSimpleIcon />
                        </Link>
                      </Button>
                      <ConfirmAction
                        title={`Delete source “${source.name || source.id}”?`}
                        description="Only the configuration will be deleted. Raw archives and run history are preserved."
                        label="Delete source"
                        variant="destructive"
                        trigger={
                          <Button
                            size="icon-sm"
                            variant="destructive"
                            aria-label={`Delete ${source.name || source.id}`}
                          >
                            <TrashIcon />
                          </Button>
                        }
                        action={async () => {
                          await api<void>(`/providers/${encodeURIComponent(source.id)}`, {
                            method: 'DELETE',
                            body: JSON.stringify({ revision: source.revision }),
                          })
                          await refreshData()
                        }}
                      />
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        !providers.isError && (
          <Empty
            title={filtered ? 'No sources match these filters' : 'No sources configured'}
            description={
              filtered
                ? 'Clear the filters to see your other sources.'
                : 'Add a source connection, check its settings, then preview the returned data before importing.'
            }
          />
        )
      )}
      <Disclosure title="Manual actions">
        <CollectDialog />
      </Disclosure>
      <AdvancedImportSettings />
    </div>
  )
}

function SourceScheduleCell({
  source,
  schedule,
  listState,
  unavailable,
}: {
  source: ProviderSummary
  schedule?: ScheduleSummary
  listState: ReturnType<typeof useListViewState>
  unavailable: boolean
}) {
  const date = useDate()
  const states: Record<ScheduleSummary['state'], string> = {
    manual: 'Manual only',
    disabled: 'Disabled',
    running: 'Import in progress',
    paused: 'Budget reached · continues on the next tick',
    blocked: 'Existing run needs review in History',
    invalid: 'Source needs review',
    waiting: schedule?.next_run_at
      ? `Next: ${date(schedule.next_run_at)}`
      : 'Waiting for next import',
  }
  return (
    <TableCell>
      <Link
        className="data-link"
        to={`/providers/${encodeURIComponent(source.id)}#schedules`}
        state={listState}
      >
        {schedule
          ? frequencyLabel(schedule.schedule.every, schedule.schedule.cron)
          : unavailable
            ? 'Schedule unavailable'
            : 'Loading schedule…'}
      </Link>
      {schedule && (
        <>
          <p
            className={`mt-1 text-xs ${schedule.state === 'blocked' || schedule.state === 'invalid' || schedule.state === 'paused' ? 'text-amber-800 dark:text-amber-300' : schedule.state === 'running' ? 'text-sky-800 dark:text-sky-300' : 'text-muted-foreground'}`}
          >
            {states[schedule.state]}
          </p>
          {schedule.last_error && (
            <p className="mt-1 max-w-xs whitespace-normal text-xs text-destructive">
              {messageText(schedule.last_error)}
            </p>
          )}
        </>
      )}
    </TableCell>
  )
}

async function readSourceImport(file: File) {
  if (!file.name.toLowerCase().endsWith('.json'))
    throw new Error('Choose a .json source definition.')
  if (file.size > 128 * 1024) throw new Error('The source definition exceeds 128 KiB.')
  let source: string
  try {
    source = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(
      await file.arrayBuffer(),
    )
  } catch {
    throw new Error('The source definition must be valid UTF-8.')
  }
  const parsed = parseProviderJSON(source)
  if (parsed.errors.length) throw new Error(`Import rejected. ${parsed.errors[0]}`)
  return source
}

export function ProviderEditor() {
  const { id } = useParams()
  const [template, setTemplate] = useState<AdapterInfo | null>(null)
  const [jsonDefinition, setJSONDefinition] = useState(false)
  const document = useQuery({
    queryKey: ['provider', id],
    queryFn: ({ signal }) =>
      api<ProviderDocument>(`/providers/${encodeURIComponent(id!)}`, { signal }),
    enabled: Boolean(id),
  })
  const adapters = useQuery({
    queryKey: ['adapters'],
    queryFn: ({ signal }) => api<Items<AdapterInfo>>('/adapters', { signal }),
    enabled: !id,
  })
  if (id)
    return (
      <div className="page">
        {document.isPending ? (
          <Loading />
        ) : !document.data ? (
          <>
            <PageHeader
              title="Source configuration"
              back={{ to: '/providers/settings', label: 'All sources' }}
            />
            <ErrorNotice error={document.error} retry={() => void document.refetch()} />
          </>
        ) : (
          <>
            <ErrorNotice error={document.error} retry={() => void document.refetch()} />
            <ProviderForm key={id} document={document.data} sourceID={id} />
          </>
        )}
      </div>
    )
  if (jsonDefinition)
    return (
      <div className="page">
        <ProviderForm key="json" initialJSON="" />
      </div>
    )
  if (template)
    return (
      <div className="page">
        <ProviderForm key={`${template.type}:${template.profile ?? ''}`} template={template} />
      </div>
    )
  return (
    <div className="page">
      <PageHeader
        title="Add source"
        description="Choose a source template or configure a JSON definition."
        back={{ to: '/providers/settings', label: 'All sources' }}
        actions={
          <Button variant="outline" onClick={() => setJSONDefinition(true)}>
            <BracketsCurlyIcon />
            Use JSON
          </Button>
        }
      />
      <ErrorNotice error={adapters.error} retry={() => void adapters.refetch()} />
      {adapters.data && <ResultCount total={adapters.data.items?.length ?? 0} label="templates" />}
      {adapters.isPending ? (
        <Loading />
      ) : adapters.data?.items?.length ? (
        <div className="table-frame">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Source template</TableHead>
                <TableHead>Usage</TableHead>
                <TableHead className="text-right">Template</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {adapters.data.items.map((adapter) => (
                <TableRow key={`${adapter.type}:${adapter.profile ?? ''}`}>
                  <TableCell>
                    <p className="font-medium">{adapter.name}</p>
                    <p className="mono mt-1 text-muted-foreground">{adapter.type}</p>
                  </TableCell>
                  <TableCell className="max-w-xl whitespace-normal text-muted-foreground">
                    {adapter.description}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" onClick={() => setTemplate(adapter)}>
                      Use this template
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      ) : (
        !adapters.isError && (
          <Empty
            title="No source templates available"
            description="The server exposes no source templates. Check its configuration."
          />
        )
      )}
    </div>
  )
}

function ProviderForm({
  document,
  sourceID,
  template,
  initialJSON,
}: {
  document?: ProviderDocument
  sourceID?: string
  template?: AdapterInfo
  initialJSON?: string
}) {
  const navigate = useNavigate()
  const { state: navigationState, hash } = useLocation()
  const [json, setJSON] = useState(document?.json ?? initialJSON ?? template?.template ?? '')
  const [savedJSON, setSavedJSON] = useState(document?.json ?? '')
  const [revision, setRevision] = useState(document?.revision ?? '')
  const [validation, setValidation] = useState<Validation | null>(null)
  const [validatedJSON, setValidatedJSON] = useState<string | null>(null)
  const [remote, setRemote] = useState<ProviderDocument | null>(null)
  const [mergeError, setMergeError] = useState('')
  const notify = useNotification()
  const [fieldDraft, setFieldDraft] = useState(false)
  const [importError, setImportError] = useState<Error | null>(null)
  const [pendingImport, setPendingImport] = useState<{ name: string; json: string } | null>(null)
  const [editorKey, setEditorKey] = useState(0)
  const fileInput = useRef<HTMLInputElement>(null)
  const parsed = useMemo(() => parseProviderJSON(json), [json])
  const [editorMode, setEditorMode] = useState<'visual' | 'json'>(() =>
    parsed.errors.length ? 'json' : 'visual',
  )
  const byteLength = useMemo(() => new TextEncoder().encode(json).length, [json])
  const locallyInvalid = parsed.errors.length > 0 || byteLength > 128 * 1024
  const dirty = json !== savedJSON || fieldDraft
  const externallyChanged = Boolean(document && document.revision !== revision)
  const validate = useMutation({
    mutationFn: (source: string) =>
      api<Validation>('/providers/validate', {
        method: 'POST',
        body: JSON.stringify({ json: source }),
      }),
    onSuccess: (result, source) => {
      setValidation(result)
      setValidatedJSON(source)
    },
  })
  const save = useMutation({
    mutationFn: async (source: string) => {
      const result = await api<Validation>('/providers/validate', {
        method: 'POST',
        body: JSON.stringify({ json: source }),
      })
      setValidation(result)
      setValidatedJSON(source)
      if (!result.valid || !result.id)
        throw new ApiError(
          422,
          'The definition must be valid before it can be saved.',
          result.issues ?? [],
        )
      if (sourceID && result.id !== sourceID)
        throw new ApiError(
          422,
          `An existing source's ID cannot change. Keep "${sourceID}", or add a separate source with the new ID.`,
        )
      try {
        return await api<ProviderDocument>(
          `/providers/${encodeURIComponent(sourceID ?? result.id)}`,
          {
            method: 'PUT',
            body: JSON.stringify({ json: source, revision }),
          },
        )
      } catch (error) {
        if (!sourceID && error instanceof ApiError && error.status === 409)
          throw new ApiError(
            409,
            'A source already uses this ID. Choose a different ID, or open the existing source to edit it.',
          )
        throw error
      }
    },
    onSuccess: async (result) => {
      queryClient.setQueryData(['provider', result.provider.id], result)
      setJSON(result.json)
      setSavedJSON(result.json)
      setRevision(result.revision)
      setRemote(null)
      setPendingImport(null)
      notify('Configuration saved. No run was started.')
      await refreshData()
      markSaved()
      if (!sourceID || result.provider.id !== sourceID)
        navigate(`/providers/${encodeURIComponent(result.provider.id)}`, {
          replace: true,
          state: navigationState,
          // Mount the saved editor before the old form can accept another edit.
          flushSync: true,
        })
    },
  })
  const markSaved = useUnsavedChanges(dirty, 'source configuration', save.isPending)
  const loadRemote = useMutation({
    mutationFn: () =>
      api<ProviderDocument>(`/providers/${encodeURIComponent(sourceID ?? validation?.id ?? '')}`),
    onSuccess: (current) => {
      setRemote(current)
      setMergeError('')
      if (sourceID) queryClient.setQueryData(['provider', sourceID], current)
    },
  })
  const busy = validate.isPending || save.isPending
  const blocked = busy || locallyInvalid || fieldDraft
  const issues = validation && validatedJSON === json ? validation.issues : document?.issues
  return (
    <>
      <PageHeader
        title={sourceID ? document?.provider.name || sourceID : 'Configure source'}
        back={{ to: '/providers/settings', label: 'All sources' }}
        actions={
          <>
            <Button
              variant="outline"
              disabled={blocked}
              onClick={() => validate.mutate(json)}
              title="Check without contacting the source"
            >
              <CheckCircleIcon />
              {validate.isPending ? 'Checking…' : 'Check configuration'}
            </Button>
            <Button disabled={blocked || !dirty} onClick={() => save.mutate(json)}>
              <FloppyDiskIcon />
              {save.isPending ? 'Saving…' : 'Save'}
            </Button>
          </>
        }
      />
      <ErrorNotice error={validate.error} />
      <ErrorNotice error={save.error} />
      <ErrorNotice error={loadRemote.error} />
      <ErrorNotice error={importError} />
      {sourceID &&
        ((save.error instanceof ApiError && save.error.status === 409) || externallyChanged) && (
          <section className="notice space-y-3">
            <h2 className="font-medium">Revision conflict</h2>
            <p>
              The server definition changed. Your edits are preserved. Load the current version,
              compare it, and merge your changes before trying again.
            </p>
            <Button
              variant="outline"
              size="sm"
              disabled={busy || loadRemote.isPending}
              onClick={() => loadRemote.mutate()}
            >
              <ArrowClockwiseIcon />
              {loadRemote.isPending ? 'Loading…' : 'Compare server version'}
            </Button>
          </section>
        )}
      {sourceID && remote && (
        <section className="space-y-3">
          {mergeError && (
            <p className="notice notice-error" role="alert">
              {mergeError}
            </p>
          )}
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="section-title">Current server version</h2>
            <span className="mono text-muted-foreground">{remote.revision.slice(0, 12)}</span>
          </div>
          <CodeEditor
            value={remote.json}
            label="Current server JSON version"
            height="280px"
            readOnly
          />
          <div className="toolbar">
            <Button
              variant="outline"
              disabled={busy || locallyInvalid || fieldDraft}
              onClick={() => {
                let merged
                try {
                  merged = rebaseProviderJSON(savedJSON, json, remote.json)
                } catch {
                  setMergeError(
                    'A source version contains invalid JSON. Compare the versions and repair your definition before choosing which edits to keep.',
                  )
                  return
                }
                if (merged.conflicts.length) {
                  setMergeError(
                    'Both versions changed the same setting. Compare the JSON and resolve that difference before merging again.',
                  )
                  return
                }
                setJSON(merged.json)
                setRevision(remote.revision)
                setSavedJSON(remote.json)
                setRemote(null)
                save.reset()
                setValidation(null)
                notify(
                  'Server changes and your field edits are merged. Review the result before saving.',
                )
              }}
            >
              Merge my field edits
            </Button>
            <Button
              variant="outline"
              disabled={busy || locallyInvalid || fieldDraft}
              onClick={() => {
                setRevision(remote.revision)
                setSavedJSON(remote.json)
                setRemote(null)
                save.reset()
                notify(
                  'Revision updated. Your reviewed definition is preserved; check the merge before saving.',
                )
              }}
            >
              Keep my edits on this revision
            </Button>
            <ConfirmAction
              title="Reload the server definition?"
              description="Your local edits will be discarded and replaced by the version shown above."
              label="Reload"
              variant="warning"
              trigger={
                <Button variant="warning" disabled={busy}>
                  Discard my edits
                </Button>
              }
              action={async () => {
                setJSON(remote.json)
                setSavedJSON(remote.json)
                setRevision(remote.revision)
                setValidation(null)
                setRemote(null)
                setFieldDraft(false)
                setEditorKey((key) => key + 1)
                save.reset()
              }}
            />
          </div>
          <p className="help">
            Merge keeps independent server changes. Keeping your reviewed definition uses all values
            currently in your editor; compare and merge concurrent changes first.
          </p>
        </section>
      )}
      {(dirty || save.isPending) && <FormStatus dirty={dirty} saving={save.isPending} />}
      {(editorMode === 'json' || parsed.errors.length > 0) && (
        <input
          ref={fileInput}
          type="file"
          accept=".json,application/json"
          className="sr-only"
          aria-label="Import a source JSON definition"
          onChange={async (event) => {
            const file = event.target.files?.[0]
            event.target.value = ''
            if (!file) return
            try {
              setPendingImport({ name: file.name, json: await readSourceImport(file) })
              setImportError(null)
            } catch (error) {
              setImportError(
                error instanceof Error ? error : new Error('Could not read the source definition.'),
              )
            }
          }}
        />
      )}
      {pendingImport && (
        <section className="notice space-y-3">
          <h2 className="font-medium">Import {pendingImport.name}?</h2>
          <p>
            This replaces the editor content, not the saved source.{' '}
            {dirty ? 'Your current unsaved changes will be discarded. ' : ''}Review and save
            separately; no collection will start.
          </p>
          <div className="toolbar">
            <Button
              variant="warning"
              disabled={busy || fieldDraft}
              onClick={() => {
                setJSON(pendingImport.json)
                setPendingImport(null)
                setValidation(null)
                notify(
                  'JSON imported into the editor. Check the configuration and save when ready.',
                )
                setEditorKey((key) => key + 1)
                validate.reset()
                save.reset()
              }}
            >
              Replace editor content
            </Button>
            <Button variant="ghost" onClick={() => setPendingImport(null)}>
              Cancel import
            </Button>
          </div>
        </section>
      )}
      {byteLength > 128 * 1024 && (
        <p className="notice notice-error" role="alert">
          The definition exceeds the 128 KiB limit. Reduce its size before saving.
        </p>
      )}
      <SourceEditor
        key={editorKey}
        value={json}
        onChange={(value) => {
          setJSON(value)
        }}
        readOnly={busy}
        sourceID={sourceID}
        initialStage={hash === '#schedules' ? 'schedules' : undefined}
        onBlockedChange={setFieldDraft}
        mode={editorMode}
        onModeChange={setEditorMode}
        jsonActions={
          <>
            <Button
              variant="ghost"
              size="sm"
              disabled={busy || fieldDraft}
              onClick={() => fileInput.current?.click()}
            >
              <UploadSimpleIcon />
              Import JSON
            </Button>
            <Button
              variant="ghost"
              size="sm"
              disabled={locallyInvalid || fieldDraft}
              onClick={() => {
                const blob = new Blob([json], { type: 'application/json;charset=utf-8' })
                const url = URL.createObjectURL(blob)
                const link = window.document.createElement('a')
                const id = parsed.get('id')
                link.href = url
                link.download = `${typeof id === 'string' && /^[a-zA-Z0-9_-]+$/.test(id) ? id : 'source-definition'}.json`
                link.click()
                window.setTimeout(() => URL.revokeObjectURL(url), 1000)
              }}
            >
              <DownloadSimpleIcon />
              Export JSON
            </Button>
          </>
        }
      />
      {validation && validatedJSON === json && (
        <section className="notice" aria-live="polite">
          <p className={validation.valid ? 'text-primary' : 'text-destructive'}>
            {validation.valid
              ? 'Valid definition. No provider network requests were made.'
              : 'The definition contains errors.'}
          </p>
        </section>
      )}
      {Boolean(issues?.length) && (
        <ul
          className="list-disc space-y-2 rounded-md border border-destructive/25 bg-destructive/5 p-5 pl-9 text-sm"
          aria-label="Source validation issues"
        >
          {issues?.map((issue, index) => (
            <li key={index}>{issue}</li>
          ))}
        </ul>
      )}
      <footer className="flex flex-wrap items-center justify-between gap-4 border-t border-border pt-5">
        <p className="help max-w-2xl">
          Saving always uses server validation. Secret values stay in the vault; source JSON
          contains references only. Configure automatic imports in the Schedules section above. New
          runs use the saved definition.
        </p>
        {sourceID && (
          <div className="flex flex-wrap items-center gap-2">
            <CollectDialog
              providerId={sourceID}
              trigger={
                <Button
                  disabled={
                    dirty ||
                    blocked ||
                    parsed.get('enabled') !== true ||
                    Boolean(document?.issues?.length)
                  }
                >
                  <PlayIcon />
                  Run manual import
                </Button>
              }
            />
            <ConfirmAction
              title="Delete this source?"
              description="The definition will be deleted. Archives and history will remain available."
              label="Delete"
              variant="destructive"
              trigger={
                <Button variant="destructive" disabled={busy}>
                  <TrashIcon />
                  Delete source
                </Button>
              }
              action={async () => {
                await api<void>(`/providers/${encodeURIComponent(sourceID)}`, {
                  method: 'DELETE',
                  body: JSON.stringify({ revision }),
                })
                await refreshData()
                navigate('/providers/settings')
              }}
            />
            {dirty && (
              <p className="help w-full text-right">Save your changes before starting a run.</p>
            )}
          </div>
        )}
      </footer>
    </>
  )
}
