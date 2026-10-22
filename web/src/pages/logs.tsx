import { DateTimeInput, useDateTimeZoneLabel } from '@/components/date-time-input'
import { ResultCount } from '@/components/common'
import { useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useViewSearchParams } from '@/lib/detail-navigation'
import { MagnifyingGlassIcon } from '@phosphor-icons/react'
import { api, params } from '@/lib/api'
import { activityLevels, activityServices, type LogEntry } from '@/lib/activity-types'
import type { Items, Page, ProviderSummary } from '@/lib/types'
import { messageText, dateTimeToISO, useDate } from '@/lib/format'
import { useURLPagination } from '@/lib/pagination'
import { resetListView } from '@/lib/list-view'
import { ActivityLinks, LevelBadge } from '@/components/activity-common'
import { ClearFiltersButton, ListEmpty, ErrorNotice, Loading, Pager } from '@/components/common'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
} from '@/components/ui/dialog'
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from '@/components/ui/table'

export function LogsPage() {
  const date = useDate()
  const dateZone = useDateTimeZoneLabel()
  const pagination = useURLPagination()
  const { limit, offset } = pagination
  const [search, setSearch] = useViewSearchParams()
  const [inspecting, setInspecting] = useState<LogEntry | null>(null)
  const returnFocus = useRef<HTMLElement | null>(null)
  const [filterError, setFilterError] = useState<Error | null>(null)
  const [clearVersion, setClearVersion] = useState(0)
  const level = search.get('level') ?? 'info,warn,error'
  const provider = search.get('provider') ?? ''
  const service = search.get('service') ?? ''
  const run = search.get('run') ?? ''
  const q = search.get('q') ?? ''
  const from = search.get('from') ?? ''
  const to = search.get('to') ?? ''
  const query = params({ level, provider, service, run, q, from, to, limit, offset })
  const logs = useQuery({
    queryKey: ['logs', query],
    queryFn: ({ signal }) => api<Page<LogEntry>>(`/logs?${query}`, { signal }),
  })
  const providers = useQuery({
    queryKey: ['providers'],
    queryFn: ({ signal }) => api<Items<ProviderSummary>>('/providers', { signal }),
  })
  return (
    <div className="page">
      <form
        key={`${search.toString()}:${clearVersion}`}
        className="space-y-4 rounded-md border border-border p-4"
        onSubmit={(event) => {
          event.preventDefault()
          const form = new FormData(event.currentTarget)
          const next = resetListView(search, [
            'level',
            'provider',
            'service',
            'run',
            'q',
            'from',
            'to',
          ])
          const selected = form.getAll('level').map(String)
          if (selected.length === 0) {
            setFilterError(new Error('Select at least one log level.'))
            return
          }
          if (selected.join(',') !== 'info,warn,error') next.set('level', selected.join(','))
          const scope = String(form.get('scope') ?? '')
          if (scope.startsWith('service:')) next.set('service', scope.slice(8))
          else if (scope.startsWith('source:')) next.set('provider', scope.slice(7))
          for (const key of ['run', 'q']) {
            const value = String(form.get(key) ?? '').trim()
            if (value) next.set(key, value)
          }
          for (const key of ['from', 'to']) {
            const value = String(form.get(key) ?? '')
            if (value) next.set(key, dateTimeToISO(value, search.get(key))!)
          }
          if (
            next.has('from') &&
            next.has('to') &&
            Date.parse(next.get('from')!) > Date.parse(next.get('to')!)
          ) {
            setFilterError(new Error('The start time must be before the end time.'))
            return
          }
          setFilterError(null)
          setSearch(next)
        }}
      >
        <fieldset className="flex flex-wrap gap-x-5 gap-y-2">
          <legend className="mb-2 text-sm font-medium">Log levels</legend>
          {activityLevels.map((item) => (
            <label key={item} className="flex items-center gap-2 text-sm">
              <input
                type="checkbox"
                name="level"
                value={item}
                defaultChecked={!level || level.split(',').includes(item)}
                className="size-4 accent-primary"
              />
              <LevelBadge level={item} />
            </label>
          ))}
        </fieldset>
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          <div className="field">
            <Label htmlFor="log-provider">Source or service</Label>
            <select
              id="log-provider"
              name="scope"
              className="native-select"
              defaultValue={service ? `service:${service}` : provider ? `source:${provider}` : ''}
            >
              <option value="">All sources and service activity</option>
              {provider && !providers.data?.items?.some((item) => item.id === provider) && (
                <option value={`source:${provider}`}>{provider}</option>
              )}
              <optgroup label="Services">
                {activityServices.map((item) => (
                  <option key={item.id} value={`service:${item.id}`}>
                    {item.name}
                  </option>
                ))}
              </optgroup>
              <optgroup label="Sources">
                {providers.data?.items?.map((item) => (
                  <option key={item.id} value={`source:${item.id}`}>
                    {item.name || item.id}
                  </option>
                ))}
              </optgroup>
            </select>
          </div>
          <div className="field">
            <Label htmlFor="log-run">Run ID</Label>
            <Input id="log-run" name="run" defaultValue={run} placeholder="Any run" />
          </div>
          <div className="field">
            <Label htmlFor="log-query">Search text</Label>
            <Input id="log-query" name="q" defaultValue={q} placeholder="Search event messages" />
          </div>
          <div className="field">
            <Label htmlFor="log-from">From ({dateZone})</Label>
            <DateTimeInput id="log-from" name="from" defaultValue={from} />
          </div>
          <div className="field">
            <Label htmlFor="log-to">To ({dateZone})</Label>
            <DateTimeInput id="log-to" name="to" defaultValue={to} />
          </div>
          <div className="flex flex-wrap items-end gap-2">
            <Button type="submit">
              <MagnifyingGlassIcon />
              Apply filters
            </Button>
            <ClearFiltersButton
              onClear={() => {
                setFilterError(null)
                setClearVersion((value) => value + 1)
                setSearch(
                  resetListView(search, ['level', 'provider', 'service', 'run', 'q', 'from', 'to']),
                )
              }}
            />
          </div>
        </div>
        <p className="help">Date filters use {dateZone}.</p>
        <ErrorNotice error={filterError} />
      </form>
      <ErrorNotice error={providers.error} retry={() => void providers.refetch()} />
      <ErrorNotice error={logs.error} retry={() => void logs.refetch()} />
      {logs.isPending ? (
        <Loading label="Loading logs" />
      ) : (
        logs.data && (
          <>
            <ResultCount total={logs.data.total} label="log entries" />
            {logs.data.items?.length ? (
              <div className="table-frame">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Time / level</TableHead>
                      <TableHead>Event / message</TableHead>
                      <TableHead>Context</TableHead>
                      <TableHead>
                        <span className="sr-only">Metadata</span>
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {logs.data.items.map((entry) => (
                      <TableRow key={entry.id}>
                        <TableCell className="align-top">
                          <time
                            className="mb-2 block text-xs text-muted-foreground"
                            dateTime={entry.created_at}
                            title={entry.created_at}
                          >
                            {date(entry.created_at)}
                          </time>
                          <LevelBadge level={entry.level} />
                        </TableCell>
                        <TableCell className="min-w-64 max-w-xl whitespace-normal align-top">
                          {entry.title && <p className="font-medium">{entry.title}</p>}
                          <p className="mono text-muted-foreground">{entry.kind}</p>
                          <p className="mt-1 break-words leading-6">{messageText(entry.message)}</p>
                        </TableCell>
                        <TableCell className="align-top">
                          <ActivityLinks {...entry} />
                        </TableCell>
                        <TableCell className="align-top">
                          {entry.data && Object.keys(entry.data).length > 0 && (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={(event) => {
                                returnFocus.current = event.currentTarget
                                setInspecting(entry)
                              }}
                            >
                              Metadata<span className="sr-only"> for log {entry.id}</span>
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            ) : (
              <ListEmpty
                title="No logs match these filters"
                description="Source and service activity will appear here."
                filtered={Boolean(
                  search.has('level') || provider || service || run || q || from || to,
                )}
                offset={offset}
                onFirstPage={() => setSearch(resetListView(search))}
              />
            )}
            <Pager
              total={logs.data.total}
              limit={logs.data.limit}
              offset={logs.data.offset}
              busy={logs.isFetching}
              onChange={pagination.setOffset}
            />
          </>
        )
      )}
      <Dialog
        open={Boolean(inspecting)}
        onOpenChange={(open) => {
          if (!open) setInspecting(null)
        }}
      >
        <DialogContent
          onCloseAutoFocus={(event) => {
            event.preventDefault()
            if (returnFocus.current?.isConnected) returnFocus.current.focus()
            else document.getElementById('log-query')?.focus()
          }}
        >
          <DialogHeader>
            <DialogTitle>Event metadata</DialogTitle>
            <DialogDescription>
              {inspecting?.kind} · {date(inspecting?.created_at)}. Only server-approved activity
              metadata is included.
            </DialogDescription>
          </DialogHeader>
          <pre className="max-h-96 overflow-auto rounded-md border border-border bg-muted p-4 font-mono text-xs whitespace-pre-wrap break-words">
            {JSON.stringify(inspecting?.data, null, 2)}
          </pre>
        </DialogContent>
      </Dialog>
    </div>
  )
}
